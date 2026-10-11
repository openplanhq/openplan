package isolation

import (
	"fmt"
	"os"
	"syscall"
)

const (
	// FirstUID is openplan-run-1's uid. The pool sits above the system and
	// login ranges, so its uids do not collide with users on the host: a kill
	// of everything one uid owns would reach theirs too.
	FirstUID = 70001
	// PoolSize is how many pool users Dockerfile.executor creates.
	PoolSize = 40
)

// user is one pool user. Its group has the same number and the user is its
// only member, so group permissions never span two sessions. Callers never
// see one: the package picks, lends, checks and takes back users itself, so
// no caller can run a command as a user it didn't lease.
type user struct {
	name string
	uid  uint32
	gid  uint32
}

// poolUsers returns the users Dockerfile.executor creates.
func poolUsers() []user {
	users := make([]user, PoolSize)
	for i := range users {
		id := uint32(FirstUID + i)
		users[i] = user{name: fmt.Sprintf("openplan-run-%d", i+1), uid: id, gid: id}
	}
	return users
}

// developmentUsers returns count users that are all the current user, so
// tests can run sessions without root or the image's pool users.
func developmentUsers(count int) []user {
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid()) //nolint:gosec // the kernel stores uids and gids in 32 bits
	users := make([]user, count)
	for i := range users {
		users[i] = user{name: fmt.Sprintf("development-%d", i+1), uid: uid, gid: gid}
	}
	return users
}

// isPoolUID reports whether uid belongs to a pool user.
func isPoolUID(uid int) bool {
	return uid >= FirstUID && uid < FirstUID+PoolSize
}

// credential returns what a child needs to start as u, or nil when it needs
// nothing because it already is u. Only root can switch to another user, and
// switching from root to u makes the kernel clear every capability.
func (u user) credential() (*syscall.Credential, error) {
	if os.Geteuid() == 0 {
		// Root can start a child as any uid, its own included. A uid outside
		// the pool, such as the 0 of a user nobody filled in, would run tenant
		// code with root's powers.
		if !isPoolUID(int(u.uid)) {
			return nil, fmt.Errorf("refusing to start a process as uid %d: not a pool user", u.uid)
		}
		// An empty, non-nil Groups drops root's supplementary groups too.
		return &syscall.Credential{Uid: u.uid, Gid: u.gid, Groups: []uint32{}}, nil
	}
	if u.uid == uint32(os.Getuid()) { //nolint:gosec // the kernel stores uids in 32 bits
		return nil, nil
	}
	return nil, fmt.Errorf("start a process as %s: only root can switch users", u.name)
}
