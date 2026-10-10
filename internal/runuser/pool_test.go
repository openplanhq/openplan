package runuser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type reclaimCall struct {
	user User
	dir  string
}

type recordingReclaimer struct {
	mu    sync.Mutex
	calls []reclaimCall
	err   error
}

func (r *recordingReclaimer) reclaim(_ context.Context, user User, dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, reclaimCall{user, dir})
	return r.err
}

func newTestPool(count int, reclaimer *recordingReclaimer) (*Pool, *time.Time) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pool := NewPool(PoolUsers()[:count], reclaimer.reclaim)
	pool.now = func() time.Time { return now }
	return pool, &now
}

func TestLeaseLookupRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reclaimer := &recordingReclaimer{}
	pool, _ := newTestPool(2, reclaimer)

	a, err := pool.Lease(ctx, "t/a", "/runs/t/a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pool.Lease(ctx, "t/b", "/runs/t/b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two sessions share %s", a.Name)
	}
	if got, err := pool.Lookup("t/a"); err != nil || got != a {
		t.Fatalf("Lookup = %v, %v; want %v", got, err, a)
	}

	if err := pool.Release(ctx, "t/a"); err != nil {
		t.Fatal(err)
	}
	if len(reclaimer.calls) != 1 || reclaimer.calls[0] != (reclaimCall{a, "/runs/t/a"}) {
		t.Fatalf("reclaims = %v, want one for %s", reclaimer.calls, a.Name)
	}
	if _, err := pool.Lookup("t/a"); !errors.Is(err, ErrNoLease) {
		t.Fatalf("Lookup after release = %v, want ErrNoLease", err)
	}
	// The released user is free again.
	if c, err := pool.Lease(ctx, "t/c", "/runs/t/c"); err != nil || c != a {
		t.Fatalf("Lease = %v, %v; want the released %v", c, err, a)
	}
}

func TestLeaseFailsWhenNoUserIsFree(t *testing.T) {
	t.Parallel()
	pool, _ := newTestPool(1, &recordingReclaimer{})

	if _, err := pool.Lease(context.Background(), "t/a", "/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Lease(context.Background(), "t/b", "/b"); !errors.Is(err, ErrNoFreeUser) {
		t.Fatalf("Lease = %v, want ErrNoFreeUser", err)
	}
}

// A session lives at most 24 hours. A lease older than 25 is one whose cleanup
// never came, and the next lease takes it back.
func TestLeaseTakesBackExpiredLeases(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	pool, now := newTestPool(1, reclaimer)
	stale, _ := pool.Lease(context.Background(), "t/stale", "/stale")

	*now = now.Add(25*time.Hour + time.Second)
	fresh, err := pool.Lease(context.Background(), "t/fresh", "/fresh")
	if err != nil {
		t.Fatalf("Lease = %v, want the expired user back", err)
	}
	if fresh != stale || len(reclaimer.calls) != 1 || reclaimer.calls[0].dir != "/stale" {
		t.Fatalf("fresh = %v, reclaims = %v", fresh, reclaimer.calls)
	}
	if _, err := pool.Lookup("t/stale"); !errors.Is(err, ErrNoLease) {
		t.Fatalf("Lookup(stale) = %v, want ErrNoLease", err)
	}
}

// A plan session that lost its cleanup, then the apply session of the same
// run on the same executor: the old branch ends before the new one starts.
func TestLeaseReclaimsTheSameRunsEarlierLeaseFirst(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	pool, _ := newTestPool(2, reclaimer)
	first, _ := pool.Lease(context.Background(), "t/run", "/runs/t/run")

	if _, err := pool.Lease(context.Background(), "t/run", "/runs/t/run"); err != nil {
		t.Fatal(err)
	}
	if len(reclaimer.calls) != 1 || reclaimer.calls[0] != (reclaimCall{first, "/runs/t/run"}) {
		t.Fatalf("reclaims = %v, want the first lease's", reclaimer.calls)
	}
}

// A user whose reclaim failed may still have processes running. Handing it to
// another session would put two sessions on one uid, so it leaves the pool.
func TestPoolQuarantinesAUserWhoseReclaimFailed(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{err: errors.New("helper failed")}
	pool, _ := newTestPool(1, reclaimer)
	if _, err := pool.Lease(context.Background(), "t/a", "/a"); err != nil {
		t.Fatal(err)
	}

	if err := pool.Release(context.Background(), "t/a"); err == nil {
		t.Fatal("Release returned no error for a failed reclaim")
	}
	if _, err := pool.Lease(context.Background(), "t/b", "/b"); !errors.Is(err, ErrNoFreeUser) {
		t.Fatalf("Lease = %v, want ErrNoFreeUser: the user must stay out", err)
	}
}

func TestReleaseWithoutALeaseIsANoOp(t *testing.T) {
	t.Parallel()
	reclaimer := &recordingReclaimer{}
	pool, _ := newTestPool(1, reclaimer)

	if err := pool.Release(context.Background(), "t/none"); err != nil || len(reclaimer.calls) != 0 {
		t.Fatalf("Release = %v, reclaims = %v; want nil and none", err, reclaimer.calls)
	}
}

func TestConcurrentLeasesGetDistinctUsers(t *testing.T) {
	t.Parallel()
	pool, _ := newTestPool(PoolSize, &recordingReclaimer{})

	var wg sync.WaitGroup
	users := make(chan User, 20)
	for i := range 20 {
		wg.Go(func() {
			user, err := pool.Lease(context.Background(), fmt.Sprintf("t/%d", i), "/d")
			if err != nil {
				t.Error(err)
				return
			}
			users <- user
		})
	}
	wg.Wait()
	close(users)
	seen := map[User]bool{}
	for user := range users {
		if seen[user] {
			t.Fatalf("%s leased twice", user.Name)
		}
		seen[user] = true
	}
}

func TestByUID(t *testing.T) {
	t.Parallel()
	pool, _ := newTestPool(3, &recordingReclaimer{})

	if user, ok := pool.ByUID(70002); !ok || user.Name != "openplan-run-2" {
		t.Fatalf("ByUID(70002) = %v, %v", user, ok)
	}
	if _, ok := pool.ByUID(10001); ok {
		t.Fatal("ByUID(10001) found a pool user")
	}
}
