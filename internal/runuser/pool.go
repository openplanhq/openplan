package runuser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/vishu42/openplan/internal/runseal"
)

var (
	// ErrNoLease reports a session that holds no pool user: its lease was
	// taken back, or the executor restarted since its PrepareWorkspace.
	ErrNoLease = errors.New("no pool user leased to this session")
	// ErrNoFreeUser reports a pool with every user leased or quarantined.
	ErrNoFreeUser = errors.New("no free pool user")
)

// Reclaimer ends a branch: it kills every process of user, clears its scratch
// files, and deletes dir, the session's run directory.
type Reclaimer func(ctx context.Context, user User, dir string) error

type lease struct {
	user    User
	dir     string
	created time.Time
}

// Pool leases each session a user of its own. It is the key ring's sibling,
// with the same life cycle: made in PrepareWorkspace, dropped in
// CleanupWorkspace, and taken back once older than a session can live.
type Pool struct {
	mu      sync.Mutex
	users   []User
	free    []User
	leases  map[string]lease
	ttl     time.Duration
	now     func() time.Time
	reclaim Reclaimer
}

// NewPool returns a pool of users, all free.
func NewPool(users []User, reclaim Reclaimer) *Pool {
	return &Pool{
		users:   slices.Clone(users),
		free:    slices.Clone(users),
		leases:  map[string]lease{},
		ttl:     runseal.DefaultKeyTTL,
		now:     time.Now,
		reclaim: reclaim,
	}
}

// Lease gives session id a free user, and records dir for its reclaim. It
// first takes back expired leases, and any earlier lease of id itself.
func (p *Pool) Lease(ctx context.Context, id, dir string) (User, error) {
	p.mu.Lock()
	now := p.now()
	var stale []lease
	for leased, l := range p.leases {
		if leased == id || now.Sub(l.created) > p.ttl {
			stale = append(stale, l)
			delete(p.leases, leased)
		}
	}
	p.mu.Unlock()

	// Outside the lock, because a reclaim starts a helper. Meanwhile these
	// users are neither free nor leased, so nothing can hand them out.
	for _, l := range stale {
		if err := p.finish(ctx, l); err != nil {
			log.Printf("runuser: %v", err)
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return User{}, ErrNoFreeUser
	}
	user := p.free[0]
	p.free = p.free[1:]
	p.leases[id] = lease{user: user, dir: dir, created: p.now()}
	return user, nil
}

// Lookup returns the user leased to session id.
func (p *Pool) Lookup(id string) (User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.leases[id]
	if !ok {
		return User{}, fmt.Errorf("%w %q", ErrNoLease, id)
	}
	return l.user, nil
}

// Release reclaims session id's user and frees it. A session without a lease
// has nothing to release.
func (p *Pool) Release(ctx context.Context, id string) error {
	p.mu.Lock()
	l, ok := p.leases[id]
	delete(p.leases, id)
	p.mu.Unlock()
	if !ok {
		return nil
	}
	return p.finish(ctx, l)
}

// finish reclaims l and frees its user. A user whose reclaim failed may still
// have processes running, so it never returns to the pool: the spare users
// absorb it until the executor restarts and sweeps.
func (p *Pool) finish(ctx context.Context, l lease) error {
	if err := p.reclaim(ctx, l.user, l.dir); err != nil {
		return fmt.Errorf("reclaim %s: %w; it stays out of the pool", l.user.Name, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Freed users join the back, so a user rests as long as possible
	// between sessions.
	p.free = append(p.free, l.user)
	return nil
}

// Users returns every user the pool was made with, for the startup sweep.
func (p *Pool) Users() []User {
	return slices.Clone(p.users)
}

// ByUID returns the pool user with uid.
func (p *Pool) ByUID(uid uint32) (User, bool) {
	for _, user := range p.users {
		if user.UID == uid {
			return user, true
		}
	}
	return User{}, false
}
