package livelog

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

func TestFlusherSetsTheInitialExpiryOnce(t *testing.T) {
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
