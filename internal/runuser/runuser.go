// Package runuser runs each session of tenant Terraform as a Linux user of its
// own, so the kernel keeps sessions apart: their processes, memory, files and
// signals (#331).
package runuser

import (
	"context"
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

// User is one pool user. Its group has the same number and the user is its
// only member, so group permissions never span two sessions.
type User struct {
	Name string
	UID  uint32
	GID  uint32
}

// PoolUsers returns the users Dockerfile.executor creates.
func PoolUsers() []User {
	users := make([]User, PoolSize)
	for i := range users {
		id := uint32(FirstUID + i)
		users[i] = User{Name: fmt.Sprintf("openplan-run-%d", i+1), UID: id, GID: id}
	}
	return users
}

// DevelopmentUsers returns count users that are all the current user, so
// tests can run sessions without root or the image's pool users. The executor
// itself never uses them: it refuses to start outside its image.
func DevelopmentUsers(count int) []User {
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid()) //nolint:gosec // the kernel stores uids and gids in 32 bits
	users := make([]User, count)
	for i := range users {
		users[i] = User{Name: fmt.Sprintf("development-%d", i+1), UID: uid, GID: gid}
	}
	return users
}

// IsPoolUID reports whether uid belongs to a pool user.
func IsPoolUID(uid int) bool {
	return uid >= FirstUID && uid < FirstUID+PoolSize
}

// Credential returns what a child needs to start as u, or nil when it needs
// nothing because it already is u. Only root can switch to another user, and
// switching from root to u makes the kernel clear every capability.
func (u User) Credential() (*syscall.Credential, error) {
	if os.Geteuid() == 0 {
		// An empty, non-nil Groups drops root's supplementary groups too.
		return &syscall.Credential{Uid: u.UID, Gid: u.GID, Groups: []uint32{}}, nil
	}
	if u.UID == uint32(os.Getuid()) { //nolint:gosec // the kernel stores uids in 32 bits
		return nil, nil
	}
	return nil, fmt.Errorf("start a process as %s: only root can switch users", u.Name)
}

// Branch is everything a session's subprocesses share: the pool user they run
// as, and HOME and TMPDIR inside the session's workspace.
type Branch struct {
	User    User
	Home    string
	TempDir string
}

type branchKey struct{}

// WithBranch returns ctx carrying branch, for the runners that start the
// session's subprocesses.
func WithBranch(ctx context.Context, branch Branch) context.Context {
	return context.WithValue(ctx, branchKey{}, branch)
}

// BranchFrom returns the branch ctx carries, if any.
func BranchFrom(ctx context.Context) (Branch, bool) {
	branch, ok := ctx.Value(branchKey{}).(Branch)
	return branch, ok
}
