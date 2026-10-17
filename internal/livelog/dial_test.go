package livelog

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestMain quiets go-redis's own logger, which prints every failed dial
// attempt to stderr; the tests assert on the errors Dial returns instead.
func TestMain(m *testing.M) {
	redis.SetLogger(discardLogger{})
	os.Exit(m.Run())
}

type discardLogger struct{}

func (discardLogger) Printf(context.Context, string, ...any) {}

func TestDialPingsTheServer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := miniredis.RunT(t)
	client, err := Dial(ctx, "redis://"+server.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// Nothing listens on port 1: a URL that parses still fails at the ping,
	// so a bad VALKEY_URL stops startup instead of the first run's output.
	if _, err := Dial(ctx, "redis://127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "ping") {
		t.Fatalf("Dial to a closed port: error = %v, want a ping error", err)
	}
}

// The registry's client holds a connection per tailer, plus the backlog
// reads of subscribers joining them.
func TestDialReadersSizesThePoolForEveryTailer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := miniredis.RunT(t)
	client, err := DialReaders(ctx, "redis://"+server.Addr())
	if err != nil {
		t.Fatalf("DialReaders: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if got, want := client.Options().PoolSize, MaxTailers+readerPoolHeadroom; got != want {
		t.Fatalf("PoolSize = %d, want %d", got, want)
	}
	if got := client.Options().PoolTimeout; got != readerPoolTimeout {
		t.Fatalf("PoolTimeout = %v, want %v", got, readerPoolTimeout)
	}
}
