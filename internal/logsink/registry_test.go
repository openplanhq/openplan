package logsink

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/domain"
)

const registryTestKey = "runlog:{run_123}:plan-1"

// recvTimeout bounds every wait on a subscription, so a missing entry fails
// the test instead of hanging it.
const recvTimeout = 5 * time.Second

func newTestRegistry(t *testing.T) (*Registry, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRegistry(client), client
}

// addPiece appends a piece to the test stream, as the flusher does, and
// returns its id.
func addPiece(t *testing.T, client *redis.Client, offset int64, data string) string {
	t.Helper()
	return addEntry(t, client, Entry{Kind: KindPiece, Offset: offset, Data: []byte(data)})
}

func addEntry(t *testing.T, client *redis.Client, entry Entry) string {
	t.Helper()
	id, err := client.XAdd(context.Background(), &redis.XAddArgs{Stream: registryTestKey, Values: entry.Values()}).Result()
	if err != nil {
		t.Fatalf("XADD: %v", err)
	}
	return id
}

// recv takes the subscription's next entry.
func recv(t *testing.T, entries <-chan Entry) Entry {
	t.Helper()
	select {
	case entry, ok := <-entries:
		if !ok {
			t.Fatal("subscription closed, want an entry")
		}
		return entry
	case <-time.After(recvTimeout):
		t.Fatal("no entry arrived")
	}
	return Entry{}
}

// waitClosed drains the subscription until it closes and returns how many
// entries it still held.
func waitClosed(t *testing.T, entries <-chan Entry) int {
	t.Helper()
	held := 0
	deadline := time.After(recvTimeout)
	for {
		select {
		case _, ok := <-entries:
			if !ok {
				return held
			}
			held++
		case <-deadline:
			t.Fatalf("subscription still open after %d entries", held)
		}
	}
}

// assertNothingArrives checks the subscription holds nothing yet. A short
// wait is the only way to see an absence; it never makes a test pass.
func assertNothingArrives(t *testing.T, entries <-chan Entry) {
	t.Helper()
	select {
	case entry, ok := <-entries:
		t.Fatalf("got entry %+v (open=%v), want nothing yet", entry, ok)
	case <-time.After(100 * time.Millisecond):
	}
}

func tailerCount(reg *Registry) int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.tailers)
}

func TestSubscribeReplaysTheBacklogThenFollows(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)
	first := addPiece(t, client, 0, "hello\n")
	second := addPiece(t, client, 6, "world\n")

	entries, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := recv(t, entries); got.ID != first || string(got.Data) != "hello\n" || got.Offset != 0 {
		t.Fatalf("first entry = %+v, want %s hello at 0", got, first)
	}
	if got := recv(t, entries); got.ID != second || string(got.Data) != "world\n" || got.Offset != 6 {
		t.Fatalf("second entry = %+v, want %s world at 6", got, second)
	}

	third := addPiece(t, client, 12, "again\n")
	if got := recv(t, entries); got.ID != third || string(got.Data) != "again\n" {
		t.Fatalf("live entry = %+v, want %s again", got, third)
	}
}

func TestSubscribeAfterAnIDSkipsWhatTheClientHas(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)
	first := addPiece(t, client, 0, "hello\n")
	second := addPiece(t, client, 6, "world\n")

	entries, err := reg.Subscribe(t.Context(), registryTestKey, first)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := recv(t, entries); got.ID != second {
		t.Fatalf("first entry = %+v, want %s: the client already has %s", got, second, first)
	}
	assertNothingArrives(t, entries)
}

func TestSubscribeClosesAfterTheEndEntry(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)

	entries, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	addPiece(t, client, 0, "hello\n")
	addEntry(t, client, Entry{Kind: KindEnd, State: domain.TemplateRunLogComplete})
	// A retried RecordTemplateRunLog appends a second end; nobody sees it.
	addEntry(t, client, Entry{Kind: KindEnd, State: domain.TemplateRunLogLost})

	if got := recv(t, entries); got.Kind != KindPiece {
		t.Fatalf("first entry = %+v, want the piece", got)
	}
	if got := recv(t, entries); got.Kind != KindEnd || got.State != domain.TemplateRunLogComplete {
		t.Fatalf("second entry = %+v, want the first end, complete", got)
	}
	if held := waitClosed(t, entries); held != 0 {
		t.Fatalf("subscription carried %d entries past the end", held)
	}
	if n := tailerCount(reg); n != 0 {
		t.Fatalf("%d tailers left after the end, want 0", n)
	}
}

// countingReader passes through to a real Reader, counting the XREADs in
// flight at once.
type countingReader struct {
	Reader

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (r *countingReader) XRead(ctx context.Context, a *redis.XReadArgs) *redis.XStreamSliceCmd {
	r.mu.Lock()
	r.inFlight++
	r.maxInFlight = max(r.maxInFlight, r.inFlight)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()
	return r.Reader.XRead(ctx, a)
}

func (r *countingReader) max() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxInFlight
}

func TestSubscribersShareOneTailer(t *testing.T) {
	t.Parallel()
	_, client := newTestRegistry(t)
	reader := &countingReader{Reader: client}
	reg := NewRegistry(reader)

	ctx, cancel := context.WithCancel(t.Context())
	first, err := reg.Subscribe(ctx, registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe first: %v", err)
	}
	second, err := reg.Subscribe(ctx, registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe second: %v", err)
	}
	id := addPiece(t, client, 0, "hello\n")
	if got := recv(t, first); got.ID != id {
		t.Fatalf("first subscriber got %+v, want %s", got, id)
	}
	if got := recv(t, second); got.ID != id {
		t.Fatalf("second subscriber got %+v, want %s", got, id)
	}
	if got := reader.max(); got != 1 {
		t.Fatalf("%d XREADs in flight at once, want 1 shared tailer", got)
	}

	// The last subscriber leaving stops the tailer.
	cancel()
	waitClosed(t, first)
	waitClosed(t, second)
	if n := tailerCount(reg); n != 0 {
		t.Fatalf("%d tailers left after every subscriber left, want 0", n)
	}
}

func TestSubscribeDropsASlowSubscriber(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)

	slow, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe slow: %v", err)
	}
	// A subscriber that keeps up shows when the tailer has fanned out every
	// entry: entries go to every subscriber in order, so once it has the
	// last one, the slow one was offered them all.
	fast, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe fast: %v", err)
	}
	const added = 70
	for i := range added {
		id := addPiece(t, client, int64(i), strconv.Itoa(i%10))
		if got := recv(t, fast); got.ID != id {
			t.Fatalf("fast subscriber got %+v, want %s", got, id)
		}
	}

	if held := waitClosed(t, slow); held >= added {
		t.Fatalf("slow subscriber received all %d entries without reading, want it dropped", held)
	}
	assertNothingArrives(t, fast)
}

// Falling behind is also measured in bytes: a burst of full-size pieces must
// not pin megabytes per slow viewer while it is still under subscriberBuffer
// entries behind.
func TestSubscribeDropsASubscriberHoldingTooManyBytes(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)

	slow, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe slow: %v", err)
	}
	fast, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe fast: %v", err)
	}
	// Enough full pieces to pass the byte bound with one in the slow
	// subscriber's forwarder, well under the entry bound.
	added := int(subscriberBufferBytes/PieceSize) + 2
	if added >= subscriberBuffer {
		t.Fatalf("added = %d pieces would also pass the entry bound of %d", added, subscriberBuffer)
	}
	data := strings.Repeat("x", int(PieceSize))
	for i := range added {
		id := addPiece(t, client, int64(i)*PieceSize, data)
		if got := recv(t, fast); got.ID != id {
			t.Fatalf("fast subscriber got %s, want %s", got.ID, id)
		}
	}

	if held := waitClosed(t, slow); held >= added {
		t.Fatalf("slow subscriber received all %d pieces without reading, want it dropped", held)
	}
	assertNothingArrives(t, fast)
}

func TestSubscribeOnAMissingStreamWaitsForIt(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)

	entries, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	assertNothingArrives(t, entries)

	id := addPiece(t, client, 0, "hello\n")
	if got := recv(t, entries); got.ID != id {
		t.Fatalf("entry = %+v, want %s", got, id)
	}
}

// blockingReader has no streams: every XREAD waits for its context and every
// XRANGE is empty, so a tailer costs nothing but its goroutine.
type blockingReader struct{}

func (blockingReader) XRead(ctx context.Context, _ *redis.XReadArgs) *redis.XStreamSliceCmd {
	<-ctx.Done()
	return redis.NewXStreamSliceCmdResult(nil, ctx.Err())
}

func (blockingReader) XRange(context.Context, string, string, string) *redis.XMessageSliceCmd {
	return redis.NewXMessageSliceCmdResult(nil, nil)
}

func (blockingReader) XRevRangeN(context.Context, string, string, string, int64) *redis.XMessageSliceCmd {
	return redis.NewXMessageSliceCmdResult(nil, nil)
}

func TestSubscribeRefusesBeyondMaxTailers(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(blockingReader{})

	for i := range MaxTailers {
		if _, err := reg.Subscribe(t.Context(), "runlog:{run_"+strconv.Itoa(i)+"}:plan-1", ""); err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
	}
	// Another subscriber of a key already tailed costs no tailer.
	if _, err := reg.Subscribe(t.Context(), "runlog:{run_0}:plan-1", ""); err != nil {
		t.Fatalf("Subscribe to a tailed key: %v", err)
	}
	if _, err := reg.Subscribe(t.Context(), "runlog:{run_new}:plan-1", ""); !errors.Is(err, ErrTooManyTailers) {
		t.Fatalf("Subscribe beyond MaxTailers = %v, want ErrTooManyTailers", err)
	}
}

// lingeringReader is go-redis's blocking read as the tailer meets it: an
// XREAD ignores its context and holds its connection until released. It
// counts the reads in flight, which is the connections tailers hold.
type lingeringReader struct {
	blockingReader

	release chan struct{}

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (r *lingeringReader) XRead(context.Context, *redis.XReadArgs) *redis.XStreamSliceCmd {
	r.mu.Lock()
	r.inFlight++
	r.maxInFlight = max(r.maxInFlight, r.inFlight)
	r.mu.Unlock()
	<-r.release
	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return redis.NewXStreamSliceCmdResult(nil, redis.Nil)
}

// A viewer that opens and closes the stream in a loop leaves each stopped
// tailer's read in flight. Those still count, so the reads one replica
// holds stay within MaxTailers however fast the loop runs.
func TestSubscribeCountsStoppedTailersStillReading(t *testing.T) {
	t.Parallel()
	reader := &lingeringReader{release: make(chan struct{})}
	reg := NewRegistry(reader)

	refused := false
	for range MaxTailers + 10 {
		ctx, cancel := context.WithCancel(t.Context())
		entries, err := reg.Subscribe(ctx, registryTestKey, "")
		if errors.Is(err, ErrTooManyTailers) {
			refused = true
			cancel()
			continue
		}
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		cancel()
		waitClosed(t, entries)
	}
	if !refused {
		t.Fatal("never refused, though every stopped tailer's read was still in flight")
	}
	reader.mu.Lock()
	maxInFlight := reader.maxInFlight
	reader.mu.Unlock()
	if maxInFlight > MaxTailers {
		t.Fatalf("%d reads in flight at once, want at most %d", maxInFlight, MaxTailers)
	}

	// Once the reads return, their slots are free again.
	close(reader.release)
	deadline := time.Now().Add(recvTimeout)
	for {
		ctx, cancel := context.WithCancel(t.Context())
		_, err := reg.Subscribe(ctx, registryTestKey, "")
		cancel()
		if err == nil {
			break
		}
		if !errors.Is(err, ErrTooManyTailers) || time.Now().After(deadline) {
			t.Fatalf("Subscribe after the reads returned: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

// The first subscriber's Last-Event-ID must not decide where the shared
// tailer reads from: one far past the stream would leave every other viewer
// waiting for entries no generated id ever reaches.
func TestSubscribeIgnoresAnAfterIDPastTheStream(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)
	first := addPiece(t, client, 0, "hello\n")

	if _, err := reg.Subscribe(t.Context(), registryTestKey, "99999999999999-0"); err != nil {
		t.Fatalf("Subscribe past the stream: %v", err)
	}
	entries, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := recv(t, entries); got.ID != first {
		t.Fatalf("backlog entry = %+v, want %s", got, first)
	}
	second := addPiece(t, client, 6, "world\n")
	if got := recv(t, entries); got.ID != second {
		t.Fatalf("live entry = %+v, want %s", got, second)
	}
	addEntry(t, client, Entry{Kind: KindEnd, State: domain.TemplateRunLogComplete})
	if got := recv(t, entries); got.Kind != KindEnd {
		t.Fatalf("entry = %+v, want the end", got)
	}
	waitClosed(t, entries)
}

func TestSubscribeIgnoresAnAfterIDPastAMissingStream(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)

	if _, err := reg.Subscribe(t.Context(), registryTestKey, "99999999999999-0"); err != nil {
		t.Fatalf("Subscribe past the stream: %v", err)
	}
	entries, err := reg.Subscribe(t.Context(), registryTestKey, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	id := addPiece(t, client, 0, "hello\n")
	if got := recv(t, entries); got.ID != id {
		t.Fatalf("entry = %+v, want %s", got, id)
	}
}

func TestEntryReadsOneEntryByID(t *testing.T) {
	t.Parallel()
	reg, client := newTestRegistry(t)
	addPiece(t, client, 0, "abc")
	second := addPiece(t, client, 3, "defg")

	entry, found, err := reg.Entry(t.Context(), registryTestKey, second)
	if err != nil || !found {
		t.Fatalf("Entry(%s) = found %v, error %v; want the piece", second, found, err)
	}
	if entry.Kind != KindPiece || entry.Offset != 3 || string(entry.Data) != "defg" {
		t.Fatalf("Entry(%s) = %+v, want the piece at 3", second, entry)
	}

	// An id the stream no longer holds, or one that is not an id at all,
	// is nothing the reader can be said to have.
	for _, id := range []string{"9999999999999-0", "not-an-id", ""} {
		if _, found, err := reg.Entry(t.Context(), registryTestKey, id); err != nil || found {
			t.Fatalf("Entry(%q) = found %v, error %v; want not found", id, found, err)
		}
	}
}
