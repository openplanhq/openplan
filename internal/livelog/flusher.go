package livelog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Flusher publishes a command's spool file to its stream as the file grows.
// It tails the file rather than sitting in the writer's path, so a slow or
// unreachable Valkey costs the live view, never the command. The spool is
// already redacted and capped; the flusher publishes its bytes as they are.
//
// A Flusher is not safe for concurrent use: Start's goroutine owns it until
// stop returns.
type Flusher struct {
	// Limit, PieceSize and Interval default to LiveLimit, PieceSize,
	// FlushInterval.
	Limit, PieceSize int64
	Interval         time.Duration

	stream Stream
	key    string
	path   string
	// offset is how much of the file the stream holds: the next piece's
	// first byte.
	offset int64
	// truncated is set once the truncated entry is in: nothing follows it.
	truncated bool
	// expirySet is set once the stream carries InitialTTL. It is set once,
	// not refreshed, so a stream whose writer died expires on its own.
	expirySet bool
}

// NewFlusher returns a flusher of the spool file at path into the stream at
// key. Nothing is read or published until Flush or Start.
func NewFlusher(stream Stream, key, path string) *Flusher {
	return &Flusher{
		Limit: LiveLimit, PieceSize: PieceSize, Interval: FlushInterval,
		stream: stream, key: key, path: path,
	}
}

// Flush does one pass: reads the file from the last offset to EOF and appends
// pieces of at most PieceSize; sets the InitialTTL on the first piece; once
// the offset reaches Limit, appends one truncated entry and publishes no more.
// A missing file is not an error (tofu has not written yet).
//
// On an error the offset stays at the first byte not appended, so the next
// Flush retries from there.
func (f *Flusher) Flush(ctx context.Context) error {
	err := f.appendNew(ctx)
	// Outside appendNew so a failed expiry is retried on a later pass even
	// when the file has not grown since; the stream exists once anything
	// is in it.
	if f.offset > 0 && !f.expirySet {
		if expireErr := f.stream.Expire(ctx, f.key, InitialTTL).Err(); expireErr != nil {
			return errors.Join(err, fmt.Errorf("expire %s: %w", f.key, expireErr))
		}
		f.expirySet = true
	}
	return err
}

func (f *Flusher) appendNew(ctx context.Context) error {
	if f.truncated {
		return nil
	}
	file, err := os.Open(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open live log spool: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat live log spool: %w", err)
	}
	// The spool only grows, so its size now bounds this pass: output written
	// meanwhile waits for the next one.
	size := info.Size()
	for f.offset < size {
		// Truncated only when a byte lies past Limit: a log of exactly
		// Limit bytes fits the stream whole.
		if f.offset >= f.Limit {
			if err := f.add(ctx, Entry{Kind: KindTruncated}); err != nil {
				return err
			}
			f.truncated = true
			return nil
		}
		data := make([]byte, min(size-f.offset, f.PieceSize, f.Limit-f.offset))
		n, err := file.ReadAt(data, f.offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read live log spool: %w", err)
		}
		if n == 0 {
			return nil
		}
		if err := f.add(ctx, Entry{Kind: KindPiece, Offset: f.offset, Data: data[:n]}); err != nil {
			return err
		}
		f.offset += int64(n)
	}
	return nil
}

func (f *Flusher) add(ctx context.Context, entry Entry) error {
	if err := f.stream.XAdd(ctx, &redis.XAddArgs{Stream: f.key, Values: entry.Values()}).Err(); err != nil {
		return fmt.Errorf("append %s to %s: %w", entry.Kind, f.key, err)
	}
	return nil
}

// Start runs Flush every Interval until stop is called, then flushes once
// more and returns. Errors are logged with slog and retried next tick.
//
// stop is synchronous: once it returns, everything the file held when it was
// called has been offered to the stream, and the goroutine has ended.
func (f *Flusher) Start(ctx context.Context) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(f.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				f.flushAndLog(ctx)
				return
			case <-ticker.C:
				f.flushAndLog(ctx)
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}

// flushAndLog logs rather than returns a failed pass: the live view is best
// effort, and the uploaded log is the record.
func (f *Flusher) flushAndLog(ctx context.Context) {
	if err := f.Flush(ctx); err != nil {
		slog.WarnContext(ctx, "publish live log", "key", f.key, "offset", f.offset, "error", err)
	}
}
