package logsink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const flusherTestKey = "runlog:{run_123}:plan-1"

// newTestFlusher returns a flusher of a temp spool file into a miniredis
// stream, with the server, a client of it, and the spool's path. The file is
// not created.
func newTestFlusher(t *testing.T) (*Flusher, *miniredis.Miniredis, *redis.Client, string) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	path := filepath.Join(t.TempDir(), "plan-1.log")
	return NewFlusher(client, flusherTestKey, path), server, client, path
}

// appendFile appends data to the spool at path, as the spool writer does.
func appendFile(t *testing.T, path, data string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(data); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// entries reads every entry of the test stream back.
func entries(t *testing.T, client *redis.Client) []Entry {
	t.Helper()
	messages, err := client.XRange(context.Background(), flusherTestKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE: %v", err)
	}
	got := make([]Entry, 0, len(messages))
	for _, message := range messages {
		entry, err := ParseEntry(message)
		if err != nil {
			t.Fatalf("ParseEntry: %v", err)
		}
		got = append(got, entry)
	}
	return got
}

func flush(t *testing.T, f *Flusher) {
	t.Helper()
	if err := f.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func assertPiece(t *testing.T, entry Entry, offset int64, data string) {
	t.Helper()
	if entry.Kind != KindPiece || entry.Offset != offset || string(entry.Data) != data {
		t.Fatalf("entry = {kind %s off %d data %q}, want a piece at %d of %q", entry.Kind, entry.Offset, entry.Data, offset, data)
	}
}

func TestFlusherAppendsNewBytesAsPieces(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	appendFile(t, path, "abc")
	flush(t, f)
	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries after the first flush, want 1", len(got))
	}
	assertPiece(t, got[0], 0, "abc")

	appendFile(t, path, "def")
	flush(t, f)
	got = entries(t, client)
	if len(got) != 2 {
		t.Fatalf("stream holds %d entries after the second flush, want 2", len(got))
	}
	assertPiece(t, got[1], 3, "def")

	// Nothing new in the file: nothing new in the stream.
	flush(t, f)
	if got := entries(t, client); len(got) != 2 {
		t.Fatalf("stream holds %d entries after an idle flush, want 2", len(got))
	}
}

func TestFlusherSplitsALargeFlushIntoPieces(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	f.PieceSize = 4
	appendFile(t, path, "0123456789")
	flush(t, f)

	got := entries(t, client)
	if len(got) != 3 {
		t.Fatalf("stream holds %d entries, want 3", len(got))
	}
	assertPiece(t, got[0], 0, "0123")
	assertPiece(t, got[1], 4, "4567")
	assertPiece(t, got[2], 8, "89")
}

func TestFlusherResetsExpiryOnARecreatedStream(t *testing.T) {
	t.Parallel()

	f, server, _, path := newTestFlusher(t)
	appendFile(t, path, "abc")
	flush(t, f)
	if ttl := server.TTL(flusherTestKey); ttl != InitialTTL {
		t.Fatalf("TTL after the first flush = %v, want %v", ttl, InitialTTL)
	}

	// The expiry counts from the first piece: later output does not push it
	// out, so a stream whose writer dies still goes InitialTTL after it began.
	server.FastForward(time.Hour)
	appendFile(t, path, "def")
	flush(t, f)
	if ttl := server.TTL(flusherTestKey); ttl != InitialTTL-time.Hour {
		t.Fatalf("TTL after a later flush = %v, want %v", ttl, InitialTTL-time.Hour)
	}

	// Valkey restarted empty: the next piece recreates the stream, which
	// must not be left without an expiry.
	server.FlushAll()
	appendFile(t, path, "ghi")
	flush(t, f)
	if ttl := server.TTL(flusherTestKey); ttl != InitialTTL {
		t.Fatalf("TTL of the recreated stream = %v, want %v", ttl, InitialTTL)
	}
}

// failingExpiryStream fails its first failures ExpireNX calls.
type failingExpiryStream struct {
	Stream
	failures int
}

func (s *failingExpiryStream) ExpireNX(ctx context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	if s.failures > 0 {
		s.failures--
		cmd := redis.NewBoolCmd(ctx)
		cmd.SetErr(errors.New("i/o timeout"))
		return cmd
	}
	return s.Stream.ExpireNX(ctx, key, ttl)
}

// A failed expiry is retried by the next pass even when it has nothing new
// to append, or a stream whose writer then died would never expire.
func TestFlusherRetriesAFailedExpiry(t *testing.T) {
	t.Parallel()

	_, server, client, path := newTestFlusher(t)
	f := NewFlusher(&failingExpiryStream{Stream: client, failures: 1}, flusherTestKey, path)
	appendFile(t, path, "abc")
	if err := f.Flush(context.Background()); err == nil {
		t.Fatal("Flush with a failing expiry returned nil")
	}
	flush(t, f)
	if ttl := server.TTL(flusherTestKey); ttl != InitialTTL {
		t.Fatalf("TTL after the retry = %v, want %v", ttl, InitialTTL)
	}
}

// A piece must decode as text on its own, so a pass that ends inside a
// multi-byte rune leaves the rune's start for the next pass.
func TestFlusherHoldsBackAnIncompleteRune(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	// "─" is E2 94 80.
	appendFile(t, path, "a\xe2\x94")
	flush(t, f)
	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries, want the one piece before the rune", len(got))
	}
	assertPiece(t, got[0], 0, "a")

	appendFile(t, path, "\x80")
	flush(t, f)
	got = entries(t, client)
	if len(got) != 2 {
		t.Fatalf("stream holds %d entries after the rune completed, want 2", len(got))
	}
	assertPiece(t, got[1], 1, "─")

	// The final flush has no later pass to wait for: a rune the command
	// never finished goes out as it is.
	f.Interval = time.Hour
	stop := f.Start(context.Background())
	appendFile(t, path, "\xe2")
	stop()
	got = entries(t, client)
	if len(got) != 3 {
		t.Fatalf("stream holds %d entries after the final flush, want 3", len(got))
	}
	assertPiece(t, got[2], 4, "\xe2")
}

// A piece cut by PieceSize also ends on a rune boundary: the rune starts the
// next piece.
func TestFlusherSplitsPiecesOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	f.PieceSize = 4
	appendFile(t, path, "ab──")
	flush(t, f)

	got := entries(t, client)
	if len(got) != 3 {
		t.Fatalf("stream holds %d entries, want 3", len(got))
	}
	assertPiece(t, got[0], 0, "ab")
	assertPiece(t, got[1], 2, "─")
	assertPiece(t, got[2], 5, "─")
}

func TestFlusherStopsAtTheLimit(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	f.Limit = 5
	appendFile(t, path, "01234567")
	flush(t, f)

	got := entries(t, client)
	if len(got) != 2 {
		t.Fatalf("stream holds %d entries, want a piece and the truncated entry", len(got))
	}
	assertPiece(t, got[0], 0, "01234")
	if got[1].Kind != KindTruncated {
		t.Fatalf("last entry kind = %s, want truncated", got[1].Kind)
	}

	appendFile(t, path, "89")
	flush(t, f)
	if got := entries(t, client); len(got) != 2 {
		t.Fatalf("stream holds %d entries after more output, want still 2", len(got))
	}
}

// A log of exactly Limit bytes did not outgrow the stream, so it is not
// marked truncated.
func TestFlusherDoesNotTruncateALogOfExactlyTheLimit(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	f.Limit = 5
	appendFile(t, path, "01234")
	flush(t, f)

	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries, want the one piece", len(got))
	}
	assertPiece(t, got[0], 0, "01234")
}

func TestFlusherToleratesAMissingFile(t *testing.T) {
	t.Parallel()

	f, server, client, _ := newTestFlusher(t)
	flush(t, f)
	if got := entries(t, client); len(got) != 0 {
		t.Fatalf("stream holds %d entries, want none", len(got))
	}
	if server.Exists(flusherTestKey) {
		t.Fatal("a flush of a missing file created the stream")
	}
}

// failingStream fails its first failures XAdds, as a Valkey that timed out
// would.
type failingStream struct {
	Stream
	failures int
}

func (s *failingStream) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	if s.failures > 0 {
		s.failures--
		cmd := redis.NewStringCmd(ctx)
		cmd.SetErr(errors.New("i/o timeout"))
		return cmd
	}
	return s.Stream.XAdd(ctx, a)
}

func TestFlusherRetriesAfterAFailedAppend(t *testing.T) {
	t.Parallel()

	_, _, client, path := newTestFlusher(t)
	f := NewFlusher(&failingStream{Stream: client, failures: 1}, flusherTestKey, path)
	appendFile(t, path, "abc")

	if err := f.Flush(context.Background()); err == nil {
		t.Fatal("Flush with a failing append returned nil")
	}
	if got := entries(t, client); len(got) != 0 {
		t.Fatalf("stream holds %d entries after the failed append, want none", len(got))
	}

	flush(t, f)
	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries after the retry, want 1", len(got))
	}
	assertPiece(t, got[0], 0, "abc")
}

func TestStartFlushesOnceMoreOnStop(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	// No tick comes in the test's lifetime: only the final flush can publish.
	f.Interval = time.Hour
	stop := f.Start(context.Background())
	appendFile(t, path, "last words\n")
	stop()

	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries, want the final flush's piece", len(got))
	}
	assertPiece(t, got[0], 0, "last words\n")
}

// A cancelled command's last lines are the ones that say why it stopped, so
// the final flush outlives the context RunTerraform started the flusher on.
func TestStartFinalFlushSurvivesACancelledContext(t *testing.T) {
	t.Parallel()

	f, _, client, path := newTestFlusher(t)
	f.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	stop := f.Start(ctx)
	cancel()
	appendFile(t, path, "interrupted\n")
	stop()

	got := entries(t, client)
	if len(got) != 1 {
		t.Fatalf("stream holds %d entries, want the final flush's piece", len(got))
	}
	assertPiece(t, got[0], 0, "interrupted\n")
}

// blockingStream answers no XAdd until its context ends, as a Valkey that
// accepted the connection and then stopped responding would.
type blockingStream struct {
	Stream
}

func (s blockingStream) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	<-ctx.Done()
	cmd := redis.NewStringCmd(ctx)
	cmd.SetErr(ctx.Err())
	return cmd
}

func TestFlushPassIsBounded(t *testing.T) {
	t.Parallel()

	_, _, client, path := newTestFlusher(t)
	f := NewFlusher(blockingStream{Stream: client}, flusherTestKey, path)
	f.Interval = 50 * time.Millisecond
	appendFile(t, path, "abc")

	began := time.Now()
	err := f.Flush(context.Background())
	elapsed := time.Since(began)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Flush against a stalled stream = %v, want a deadline error", err)
	}
	// Twice Interval, with room for a slow test machine.
	if elapsed > 10*f.Interval {
		t.Fatalf("Flush against a stalled stream took %v, want about %v", elapsed, 2*f.Interval)
	}
}
