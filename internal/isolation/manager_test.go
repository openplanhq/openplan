package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type reclaimCall struct {
	user user
	dir  string
}

type recordingReclaimer struct {
	mu    sync.Mutex
	calls []reclaimCall
	err   error
}

func (r *recordingReclaimer) reclaim(_ context.Context, s *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, reclaimCall{s.user, s.runDir})
	return r.err
}

// newLeaseTestManager leases real pool users' identities without making any
// directory or starting any helper: the lease bookkeeping on its own.
func newLeaseTestManager(count int, reclaimer *recordingReclaimer) (*Manager, *time.Time) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	m := newManager(Config{}, poolUsers()[:count], inProcessHelper{})
	m.reclaim = reclaimer.reclaim
	m.now = func() time.Time { return now }
	return m, &now
}

// recordingHelper does the in-process helper's work and records what it was
// asked to kill and reclaim.
type recordingHelper struct {
	inProcessHelper
	mu        sync.Mutex
	killed    []user
	reclaimed []string
}

func (h *recordingHelper) kill(ctx context.Context, u user) error {
	h.mu.Lock()
	h.killed = append(h.killed, u)
	h.mu.Unlock()
	return h.inProcessHelper.kill(ctx, u)
}

func (h *recordingHelper) reclaim(ctx context.Context, u user, workspace string) error {
	h.mu.Lock()
	h.reclaimed = append(h.reclaimed, workspace)
	h.mu.Unlock()
	return h.inProcessHelper.reclaim(ctx, u, workspace)
}

func (h *recordingHelper) killedUsers() []user {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]user(nil), h.killed...)
}

// newTestManager runs sessions as the current user under runRoot, through h.
func newTestManager(runRoot string, sessions int, h helper) *Manager {
	return newManager(Config{RunRoot: runRoot}, developmentUsers(sessions), h)
}

func TestLeaseSessionClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reclaimer := &recordingReclaimer{}
	m, _ := newLeaseTestManager(2, reclaimer)

	a, err := m.lease(ctx, "t/a", "/runs/t/a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.lease(ctx, "t/b", "/runs/t/b")
	if err != nil {
		t.Fatal(err)
	}
	if a.user == b.user {
		t.Fatalf("two sessions share %s", a.user.name)
	}
	if got, err := m.Session("t", "a"); err != nil || got != a {
		t.Fatalf("Session = %v, %v; want %v", got, err, a)
	}

	if err := m.Close(ctx, "t", "a"); err != nil {
		t.Fatal(err)
	}
	if len(reclaimer.calls) != 1 || reclaimer.calls[0] != (reclaimCall{a.user, "/runs/t/a"}) {
		t.Fatalf("reclaims = %v, want one for %s", reclaimer.calls, a.user.name)
	}
	if _, err := m.Session("t", "a"); !errors.Is(err, ErrNoLease) {
		t.Fatalf("Session after Close = %v, want ErrNoLease", err)
	}
	// The released user is free again.
	if c, err := m.lease(ctx, "t/c", "/runs/t/c"); err != nil || c.user != a.user {
		t.Fatalf("lease = %v, %v; want the released %v", c, err, a.user)
	}
}

func TestLeaseFailsWhenNoUserIsFree(t *testing.T) {
	t.Parallel()
	m, _ := newLeaseTestManager(1, &recordingReclaimer{})

	if _, err := m.lease(context.Background(), "t/a", "/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.lease(context.Background(), "t/b", "/b"); !errors.Is(err, ErrNoFreeUser) {
		t.Fatalf("lease = %v, want ErrNoFreeUser", err)
	}
}

// A session lives at most 24 hours. A lease older than 25 is one whose cleanup
// never came, and the next lease takes it back.
func TestLeaseTakesBackExpiredLeases(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	m, now := newLeaseTestManager(1, reclaimer)
	stale, _ := m.lease(context.Background(), "t/stale", "/stale")

	*now = now.Add(25*time.Hour + time.Second)
	fresh, err := m.lease(context.Background(), "t/fresh", "/fresh")
	if err != nil {
		t.Fatalf("lease = %v, want the expired user back", err)
	}
	if fresh.user != stale.user || len(reclaimer.calls) != 1 || reclaimer.calls[0].dir != "/stale" {
		t.Fatalf("fresh = %v, reclaims = %v", fresh, reclaimer.calls)
	}
	if _, err := m.Session("t", "stale"); !errors.Is(err, ErrNoLease) {
		t.Fatalf("Session(stale) = %v, want ErrNoLease", err)
	}
}

// A plan session that lost its cleanup, then the apply session of the same
// run on the same executor: the old branch ends before the new one starts.
func TestLeaseReclaimsTheSameRunsEarlierLeaseFirst(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	m, _ := newLeaseTestManager(2, reclaimer)
	first, _ := m.lease(context.Background(), "t/run", "/runs/t/run")

	if _, err := m.lease(context.Background(), "t/run", "/runs/t/run"); err != nil {
		t.Fatal(err)
	}
	if len(reclaimer.calls) != 1 || reclaimer.calls[0] != (reclaimCall{first.user, "/runs/t/run"}) {
		t.Fatalf("reclaims = %v, want the first lease's", reclaimer.calls)
	}
}

// A user whose reclaim failed may still have processes running. Handing it to
// another session would put two sessions on one uid, so it leaves the pool.
func TestManagerQuarantinesAUserWhoseReclaimFailed(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{err: errors.New("helper failed")}
	m, _ := newLeaseTestManager(1, reclaimer)
	if _, err := m.lease(context.Background(), "t/a", "/a"); err != nil {
		t.Fatal(err)
	}

	if err := m.Close(context.Background(), "t", "a"); err == nil {
		t.Fatal("Close returned no error for a failed reclaim")
	}
	if _, err := m.lease(context.Background(), "t/b", "/b"); !errors.Is(err, ErrNoFreeUser) {
		t.Fatalf("lease = %v, want ErrNoFreeUser: the user must stay out", err)
	}
}

func TestCloseWithoutASessionIsANoOp(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	m, _ := newLeaseTestManager(1, reclaimer)

	if err := m.Close(context.Background(), "t", "none"); err != nil || len(reclaimer.calls) != 0 {
		t.Fatalf("Close = %v, reclaims = %v; want nil and none", err, reclaimer.calls)
	}
}

func TestConcurrentLeasesGetDistinctUsers(t *testing.T) {
	t.Parallel()
	m, _ := newLeaseTestManager(PoolSize, &recordingReclaimer{})

	var wg sync.WaitGroup
	users := make(chan user, 20)
	for i := range 20 {
		wg.Go(func() {
			s, err := m.lease(context.Background(), fmt.Sprintf("t/%d", i), "/d")
			if err != nil {
				t.Error(err)
				return
			}
			users <- s.user
		})
	}
	wg.Wait()
	close(users)
	seen := map[user]bool{}
	for u := range users {
		if seen[u] {
			t.Fatalf("%s leased twice", u.name)
		}
		seen[u] = true
	}
}

func TestUserByUID(t *testing.T) {
	t.Parallel()
	m, _ := newLeaseTestManager(3, &recordingReclaimer{})

	if u, ok := m.userByUID(70002); !ok || u.name != "openplan-run-2" {
		t.Fatalf("userByUID(70002) = %v, %v", u, ok)
	}
	if _, ok := m.userByUID(10001); ok {
		t.Fatal("userByUID(10001) found a pool user")
	}
}

func TestOpenLaysOutTheRunDirectory(t *testing.T) {
	t.Parallel()
	runRoot := t.TempDir()
	m := newTestManager(runRoot, 2, inProcessHelper{})
	runDir := filepath.Join(runRoot, "tenant_123", "run_123")

	s, err := m.Open(context.Background(), "tenant_123", "run_123")
	if err != nil {
		t.Fatal(err)
	}
	if s.Workspace() != filepath.Join(runDir, "workspace") || s.LogsDir() != filepath.Join(runDir, "logs") {
		t.Fatalf("Workspace = %q, LogsDir = %q", s.Workspace(), s.LogsDir())
	}
	for path, want := range map[string]os.FileMode{
		runDir:                                     0o711,
		filepath.Join(runDir, "logs"):              0o700,
		filepath.Join(runDir, "workspace"):         0o700,
		filepath.Join(runDir, "workspace", "home"): 0o700,
		filepath.Join(runDir, "workspace", "tmp"):  0o700,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want mode %o", path, info, err, want)
		}
	}
	if got, err := m.Session("tenant_123", "run_123"); err != nil || got != s {
		t.Fatalf("Session = %v, %v; want the opened session", got, err)
	}
}

// The sweep after a restart walks <RunRoot>/<tenant>/<run> only. Each ID must
// be one path component, so no run directory lies anywhere the sweep misses.
func TestOpenRefusesIDsThatLeaveTheRunLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newTestManager(t.TempDir(), 1, inProcessHelper{})
	for _, ids := range [][2]string{{"..", "run"}, {"t", ".."}, {".", "run"}, {"t", "a/b"}, {"", "run"}, {"t", " "}} {
		if _, err := m.Open(ctx, ids[0], ids[1]); err == nil {
			t.Errorf("Open(%q, %q) succeeded", ids[0], ids[1])
		}
		if err := m.Close(ctx, ids[0], ids[1]); err == nil {
			t.Errorf("Close(%q, %q) succeeded", ids[0], ids[1])
		}
	}
	// No refused Open kept the one user.
	if _, err := m.Open(ctx, "t", "run"); err != nil {
		t.Fatalf("Open after the refusals = %v", err)
	}
	if _, err := newTestManager("", 1, inProcessHelper{}).Open(ctx, "t", "run"); err == nil {
		t.Error("Open with no run root succeeded")
	}
}

func TestOpenTwiceReclaimsTheFirstSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	helper := &recordingHelper{}
	m := newTestManager(runRoot, 2, helper)

	first, err := m.Open(ctx, "t", "run")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Workspace(), "left-by-plan"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := m.Open(ctx, "t", "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(helper.reclaimed) != 1 || helper.reclaimed[0] != first.Workspace() {
		t.Fatalf("reclaimed = %v, want the first workspace", helper.reclaimed)
	}
	if _, err := os.Stat(filepath.Join(second.Workspace(), "left-by-plan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the second session sees the first's file: %v", err)
	}
	if err := first.open(); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("first session after the second Open: %v, want ErrSessionClosed", err)
	}
}

func TestCloseRemovesTheRunDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	m := newTestManager(runRoot, 1, inProcessHelper{})
	runDir := filepath.Join(runRoot, "t", "run")
	s, err := m.Open(ctx, "t", "run")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Workspace(), "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := m.Close(ctx, "t", "run"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run directory still exists: %v", err)
	}
	// The one user is free again.
	if _, err := m.Open(ctx, "t", "next"); err != nil {
		t.Fatalf("Open after Close = %v", err)
	}
}

// After a restart no lease is valid: the leases died with the process. The
// sweep ends every pool user's processes and removes every run directory.
func TestSweepRemovesWhatAnEarlierProcessLeft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runRoot := t.TempDir()
	before := newTestManager(runRoot, 4, inProcessHelper{})
	left, err := before.Open(ctx, "tenant_123", "run_123")
	if err != nil {
		t.Fatal(err)
	}

	helper := &recordingHelper{}
	after := newTestManager(runRoot, 4, helper)
	if err := after.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var perUser, workspaces int
	for _, workspace := range helper.reclaimed {
		switch workspace {
		case "":
			perUser++
		case left.Workspace():
			workspaces++
		}
	}
	if perUser != 4 || workspaces != 1 {
		t.Fatalf("reclaimed = %v, want every user once and the left workspace", helper.reclaimed)
	}
	if entries, _ := os.ReadDir(filepath.Join(runRoot, "tenant_123")); len(entries) != 0 {
		t.Fatalf("run directories remain: %v", entries)
	}
}

func TestSweepOfAMissingRunRootSweepsTheUsers(t *testing.T) {
	t.Parallel()
	helper := &recordingHelper{}
	m := newTestManager(filepath.Join(t.TempDir(), "missing"), 2, helper)

	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(helper.reclaimed) != 2 {
		t.Fatalf("reclaimed = %v, want both users", helper.reclaimed)
	}
}
