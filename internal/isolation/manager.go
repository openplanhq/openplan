package isolation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/vishu42/openplan/internal/runseal"
)

const (
	// workspaceDirName is the session's half of a run directory. The rest of
	// the run directory is root's: the executor keeps its logs beside it.
	workspaceDirName = "workspace"
)

// Manager leases each session a pool user of its own, lays out its run
// directory, and ends it again: when the session closes, when its lease
// expires, and in the sweep after a restart. Leases live as long as the
// sealing keys do (runseal.DefaultKeyTTL), and like them die with the process.
type Manager struct {
	runRoot string
	helper  helper
	// users is every user the manager was made with, for the sweep.
	users []user
	ttl   time.Duration
	now   func() time.Time
	// reclaim ends a session: reclaimSession, replaced in tests.
	reclaim func(ctx context.Context, s *Session) error

	mu     sync.Mutex
	free   []user
	leases map[string]lease
}

type lease struct {
	session *Session
	created time.Time
}

func newManager(cfg Config, users []user, h helper) *Manager {
	m := &Manager{
		runRoot: cfg.RunRoot,
		helper:  h,
		users:   slices.Clone(users),
		ttl:     runseal.DefaultKeyTTL,
		now:     time.Now,
		free:    slices.Clone(users),
		leases:  map[string]lease{},
	}
	m.reclaim = m.reclaimSession
	return m
}

// Open leases run id a free pool user and lays out runDir for it. It first
// takes back expired leases, and any earlier lease of id itself: a plan
// session that lost its cleanup, then the apply session of the same run on
// the same executor, whose old branch must end before the new one starts.
func (m *Manager) Open(ctx context.Context, id, runDir string) (*Session, error) {
	// The sweep after a restart walks <RunRoot>/<tenant>/<run> and nothing
	// else: a run directory anywhere else would never be reclaimed.
	if m.runRoot == "" || filepath.Dir(filepath.Dir(filepath.Clean(runDir))) != filepath.Clean(m.runRoot) {
		return nil, fmt.Errorf("run directory %s is not <run root>/<tenant>/<run> under %q", runDir, m.runRoot)
	}
	s, err := m.lease(ctx, id, runDir)
	if err != nil {
		return nil, err
	}
	if err := makeRunDirectory(runDir, s.user); err != nil {
		// Hand the user back now rather than hold it until the lease expires.
		return nil, errors.Join(err, m.Close(ctx, id))
	}
	return s, nil
}

func (m *Manager) lease(ctx context.Context, id, runDir string) (*Session, error) {
	m.mu.Lock()
	now := m.now()
	var stale []*Session
	for leased, l := range m.leases {
		if leased == id || now.Sub(l.created) > m.ttl {
			stale = append(stale, l.session)
			delete(m.leases, leased)
		}
	}
	m.mu.Unlock()

	// Outside the lock, because a reclaim starts a helper. Meanwhile these
	// users are neither free nor leased, so nothing can hand them out.
	for _, s := range stale {
		if err := m.finish(ctx, s); err != nil {
			log.Printf("isolation: %v", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.free) == 0 {
		return nil, ErrNoFreeUser
	}
	u := m.free[0]
	m.free = m.free[1:]
	s := &Session{id: id, runDir: runDir, user: u, m: m}
	m.leases[id] = lease{session: s, created: m.now()}
	return s, nil
}

// Session returns run id's open session.
func (m *Manager) Session(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[id]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrNoLease, id)
	}
	return l.session, nil
}

// Close ends run id's session: it kills the session's processes, clears its
// files and run directory, and frees its user. A run without a session has
// nothing to close.
func (m *Manager) Close(ctx context.Context, id string) error {
	m.mu.Lock()
	l, ok := m.leases[id]
	delete(m.leases, id)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return m.finish(ctx, l.session)
}

// holds reports whether s is still its run's session. It compares the
// session itself, not its user: once s has closed, its user may already be
// another session's.
func (m *Manager) holds(s *Session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[s.id]
	return ok && l.session == s
}

// finish reclaims s and frees its user. A user whose reclaim failed may still
// have processes running, so it never returns to the pool: the spare users
// absorb it until the executor restarts and sweeps.
func (m *Manager) finish(ctx context.Context, s *Session) error {
	if err := m.reclaim(ctx, s); err != nil {
		return fmt.Errorf("reclaim %s: %w; it stays out of the pool", s.user.name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Freed users join the back, so a user rests as long as possible
	// between sessions.
	m.free = append(m.free, s.user)
	return nil
}

// reclaimSession ends s's branch and deletes its run directory. The helper
// empties workspace/ as the user who owns it; the rest of the run directory is
// root's. By the time root removes it, the helper has killed every process
// of the user and the executor has seen none survive, so nothing is left to
// plant anything in it.
//
// The owner is usually s's user, but not always: when taking back an earlier
// session of the same run failed, its workspace is still there, and only its
// own user can empty it. Reclaiming it as s's user would fail too, and
// quarantine s's user as well.
func (m *Manager) reclaimSession(ctx context.Context, s *Session) error {
	workspace := s.Workspace()
	owner, owned, err := m.workspaceOwner(workspace)
	if err != nil {
		return err
	}
	switch {
	case !owned:
		err = m.helper.reclaim(ctx, s.user, "")
	case owner.uid == s.user.uid:
		err = m.helper.reclaim(ctx, s.user, workspace)
	default:
		err = errors.Join(m.helper.reclaim(ctx, s.user, ""), m.helper.reclaim(ctx, owner, workspace))
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(s.runDir)
}

// makeRunDirectory lays out one session's run directory and hands the session
// its own half. Everything is made as root first: once workspace/ belongs to
// the pool user, root (without CAP_DAC_OVERRIDE) can no longer create
// anything in it. No session code has run yet and root owns every parent, so
// nothing can race.
func makeRunDirectory(runDir string, u user) error {
	// 0711 down to the run: a session can walk to its own workspace but list
	// nobody's.
	if err := os.MkdirAll(runDir, 0o711); err != nil { //nolint:gosec // 0711 lets a session through to its workspace, not list the runs
		return fmt.Errorf("prepare run directory: %w", err)
	}
	if err := os.Chmod(runDir, 0o711); err != nil { //nolint:gosec // as above
		return fmt.Errorf("prepare run directory: %w", err)
	}
	workspace := filepath.Join(runDir, workspaceDirName)
	sessionDirs := []string{workspace, filepath.Join(workspace, "home"), filepath.Join(workspace, "tmp")}
	for _, dir := range sessionDirs {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return fmt.Errorf("prepare workspace: %w", err)
		}
	}
	// Innermost first: chowning workspace/ before its children would lock
	// root out of them.
	for _, dir := range slices.Backward(sessionDirs) {
		if err := os.Lchown(dir, int(u.uid), int(u.gid)); err != nil {
			return fmt.Errorf("hand the workspace to %s: %w", u.name, err)
		}
	}
	return nil
}

// Sweep ends every session an earlier executor process left behind. Run it
// once, before the executor takes sessions: after a restart no lease is
// valid, since the leases died with the process. It fails closed: an
// executor that cannot clear a pool user must not hand that user out.
func (m *Manager) Sweep(ctx context.Context) error {
	// The users are independent, so they are reclaimed at once: one round of
	// helpers and survivor waits rather than one per user.
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, u := range m.users {
		wg.Go(func() {
			if err := m.helper.reclaim(ctx, u, ""); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	tenants, err := os.ReadDir(m.runRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.Join(errs...)
	}
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, tenant := range tenants {
		runs, err := os.ReadDir(filepath.Join(m.runRoot, tenant.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, run := range runs {
			if err := m.sweepRunDirectory(ctx, filepath.Join(m.runRoot, tenant.Name(), run.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) sweepRunDirectory(ctx context.Context, runDir string) error {
	if err := m.sweepWorkspace(ctx, filepath.Join(runDir, workspaceDirName)); err != nil {
		return fmt.Errorf("sweep %s: %w", runDir, err)
	}
	return os.RemoveAll(runDir)
}

// sweepWorkspace empties workspace as the pool user who owns it, so root can
// then remove it.
func (m *Manager) sweepWorkspace(ctx context.Context, workspace string) error {
	owner, owned, err := m.workspaceOwner(workspace)
	if err != nil || !owned {
		return err
	}
	return m.helper.reclaim(ctx, owner, workspace)
}

// workspaceOwner returns the pool user who owns workspace. owned is false when
// there is nothing for a pool user to empty: no workspace, a symlink in its
// place (RemoveAll removes the symlink itself, never what it points to), or a
// workspace still root's because an Open died before the chown.
func (m *Manager) workspaceOwner(workspace string) (owner user, owned bool, err error) {
	info, err := os.Lstat(workspace)
	if errors.Is(err, fs.ErrNotExist) {
		return user{}, false, nil
	}
	if err != nil {
		return user{}, false, err
	}
	if !info.IsDir() {
		return user{}, false, nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return user{}, false, errors.New("workspace has no owner")
	}
	if stat.Uid == 0 {
		return user{}, false, nil
	}
	u, ok := m.userByUID(stat.Uid)
	if !ok {
		return user{}, false, fmt.Errorf("workspace owner %d is not a pool user", stat.Uid)
	}
	return u, true, nil
}

func (m *Manager) userByUID(uid uint32) (user, bool) {
	for _, u := range m.users {
		if u.uid == uid {
			return u, true
		}
	}
	return user{}, false
}
