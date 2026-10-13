// Package isolation keeps the sessions of tenant Terraform apart from each
// other and from the executor (#331). A session is one phase, plan or apply,
// of one run on one executor. The package promises:
//
//  1. Own user: each session runs as a Linux user of its own, one of the
//     image's pool users.
//  2. No reaching out: a session can't touch the executor or other sessions,
//     their programs, memory, files or environment.
//  3. Only way in: the only way into a session's folder is through the
//     session, as its user. Root never reads or writes there itself.
//  4. Nothing left behind: when a session ends, none of its programs or files
//     remain.
//  5. Fail closed: if the machine can't keep these promises, New refuses to
//     build a Manager, and the executor refuses to start.
//
// Callers never see a user. They get a Session for a run, tag a call with it
// (Session.Context), and the package picks, lends, checks and takes back the
// user, so whatever replaces pool users later changes nothing for callers.
//
// Not yet: a session can still see the parts of the filesystem everyone can
// read, it shares the network, and its CPU and memory aren't limited.
package isolation

import (
	"errors"
	"testing"
)

// Config is what the executor's configuration says about sessions.
type Config struct {
	// RunRoot holds every session's run directory, <RunRoot>/<tenant>/<run>.
	RunRoot string
	// MaxSessions is how many sessions the executor runs at once. The pool
	// keeps two users for each, so the spares absorb leases stuck until they
	// expire.
	MaxSessions int
}

var (
	// ErrNoLease reports a run that holds no session: its lease was taken
	// back, or the executor restarted since the run's Open.
	ErrNoLease = errors.New("no pool user leased to this session")
	// ErrNoFreeUser reports a pool with every user leased or quarantined.
	ErrNoFreeUser = errors.New("no free pool user")
	// ErrNoSession reports a subprocess the session executor was asked to
	// start outside every session: it would run as root.
	ErrNoSession = errors.New("refusing to start a subprocess outside a session")
	// ErrSessionClosed reports a session used after it ended: its user may
	// already belong to another session.
	ErrSessionClosed = errors.New("the session has ended")
)

// NewForTest returns a Manager for the tests of packages built on this one.
// Every session runs as the current user, the helper's file work runs in this
// process, and nothing is killed, since killing every process of the current
// user would end the test run. It isolates nothing and skips New's checks.
//
// It takes a testing.TB so that only a test can call it: only the testing
// package can make one, and the executor has none to give. Wired into the
// executor, which runs as root, its file work would run as root.
func NewForTest(t testing.TB, cfg Config, sessions int) *Manager {
	t.Helper()
	return newManager(cfg, developmentUsers(sessions), inProcessHelper{})
}
