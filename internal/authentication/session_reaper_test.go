package authentication

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type reapingStore struct {
	fakeSessionStore
	mu      sync.Mutex
	cutoffs []time.Time
	err     error
	swept   chan struct{}
}

func (store *reapingStore) DeleteSessionsExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
	store.mu.Lock()
	store.cutoffs = append(store.cutoffs, cutoff)
	store.mu.Unlock()
	if store.swept != nil {
		store.swept <- struct{}{}
	}
	return 1, store.err
}

func (store *reapingStore) sweeps() []time.Time {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]time.Time(nil), store.cutoffs...)
}

// startReaper runs ReapSessions in the background and returns a channel closed
// once it returns.
func startReaper(ctx context.Context, sessions SessionStore, interval time.Duration, now time.Time) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		ReapSessions(ctx, sessions, interval, func() time.Time { return now })
		close(done)
	}()
	return done
}

// within waits for ready to deliver, failing with message after a second.
func within(t *testing.T, ready <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

// TestReapSessionsSweepsBeforeWaiting pins the sweep-then-wait order. A process
// restarted more often than the interval would otherwise never sweep at all,
// and the rows it should have deleted each hold an encrypted ID token.
func TestReapSessionsSweepsBeforeWaiting(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store := &reapingStore{swept: make(chan struct{}, 1)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startReaper(ctx, store, time.Hour, now)

	within(t, store.swept, "no sweep before the first tick — a short-lived process would never reap")
	cancel()
	within(t, done, "ReapSessions did not return when its context was cancelled")

	sweeps := store.sweeps()
	if len(sweeps) == 0 || !sweeps[0].Equal(now) {
		t.Fatalf("first sweep cutoff = %v, want %v — rows are dead against the clock, not the interval", sweeps, now)
	}
}

// TestReapSessionsSurvivesAFailedSweep keeps a database blip from silently
// ending the only thing that removes session rows.
func TestReapSessionsSurvivesAFailedSweep(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store := &reapingStore{err: errors.New("connection refused"), swept: make(chan struct{}, 4)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startReaper(ctx, store, time.Millisecond, now)

	for range 2 {
		within(t, store.swept, "the loop stopped after a failed sweep")
	}
	cancel()
	within(t, done, "ReapSessions did not return when its context was cancelled")
}

// TestReapSessionsIgnoresAMissingStore covers the server built without
// WithAuth: there is nothing to reap, and a nil interface would panic in a
// goroutine, which takes the whole process down rather than one request.
func TestReapSessionsIgnoresAMissingStore(t *testing.T) {
	within(t, startReaper(context.Background(), nil, time.Hour, time.Time{}), "ReapSessions blocked on a nil store")
}
