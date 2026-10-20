package logsink

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
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

// Flusher publishes a command's spool file to its stream as the file grows.
// It tails the file rather than sitting in the writer's path, so a slow or
// unreachable Valkey costs the live view, never the command. The spool is
// already capped; the flusher publishes its bytes as they are.
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
	// expiryOwed is set while a piece has been appended since the last
	// expiry that succeeded, so a failed one is retried even when no new
	// output comes.
	expiryOwed bool
}

// finalFlushTimeout bounds the flush stop runs. It is longer than a pass
// because it is the last chance to publish, but short enough that RunTerraform,
// which waits for it with the heartbeat still running, is not held up by a
// Valkey that has gone away.
const finalFlushTimeout = 5 * time.Second

// NewFlusher returns a flusher of the spool file at path into the stream at
// key. Nothing is read or published until Flush or Start.
func NewFlusher(stream Stream, key, path string) *Flusher {
	return &Flusher{
		Limit: LiveLimit, PieceSize: PieceSize, Interval: FlushInterval,
		stream: stream, key: key, path: path,
	}
}

// Flush does one pass: reads the file from the last offset to EOF and appends
// pieces of at most PieceSize; gives the stream InitialTTL if it has no
// expiry; once the offset reaches Limit, appends one truncated entry and
// publishes no more. A missing file is not an error (tofu has not written
// yet). A pass gives up after twice Interval, so a Valkey that stops answering
// delays the next pass rather than stalling the flusher.
//
// On an error the offset stays at the first byte not appended, so the next
// Flush retries from there.
func (f *Flusher) Flush(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*f.Interval)
	defer cancel()
	return f.pass(ctx, false)
}

// pass is one Flush. final says no more output is coming, so a rune the
// file ends inside will never complete and is published as it is.
func (f *Flusher) pass(ctx context.Context, final bool) error {
	before := f.offset
	err := f.appendNew(ctx, final)
	// NX, after every pass that appended: a stream Valkey lost (a restart
	// without persistence) is recreated by the next XADD with no expiry, and
	// gets InitialTTL back here; a stream that has one keeps it, so a stream
	// whose writer died still expires InitialTTL after it began.
	if f.offset > before || f.expiryOwed {
		if expireErr := f.stream.ExpireNX(ctx, f.key, InitialTTL).Err(); expireErr != nil {
			f.expiryOwed = true
			return errors.Join(err, fmt.Errorf("expire %s: %w", f.key, expireErr))
		}
		f.expiryOwed = false
	}
	return err
}

func (f *Flusher) appendNew(ctx context.Context, final bool) error {
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
		// A reader decodes each piece as text on its own, so a piece must
		// not end inside a multi-byte rune: the rest of it starts the next
		// piece. Not at Limit, where no next piece comes, nor at the end of
		// the final pass, where the rune can no longer complete.
		end := f.offset + int64(n)
		if publishWhole := end == f.Limit || (final && end == size); !publishWhole {
			if hold := incompleteTail(data[:n]); hold > 0 {
				switch {
				case hold < n:
					n -= hold
				case end == size:
					// Only the start of a rune is new: it waits for the
					// pass that sees the rest.
					return nil
				}
				// Otherwise a rune is longer than a piece, which only a
				// test's PieceSize allows; it goes out split rather than
				// never.
			}
		}
		if err := f.add(ctx, Entry{Kind: KindPiece, Offset: f.offset, Data: data[:n]}); err != nil {
			return err
		}
		f.offset += int64(n)
	}
	return nil
}

// incompleteTail is how many bytes at the end of b start a UTF-8 sequence
// that b does not finish: at most utf8.UTFMax-1. Bytes that are not valid
// UTF-8 at all count as complete; holding them back would not help.
func incompleteTail(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return 0
			}
			return len(b) - i
		}
	}
	return 0
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
// called has been offered to the stream, and the goroutine has ended. The
// final flush runs even when ctx is already cancelled, since a cancelled
// command's last lines are the ones worth seeing, and gives up after
// finalFlushTimeout.
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
				finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalFlushTimeout)
				f.logFailure(finalCtx, f.pass(finalCtx, true))
				cancel()
				return
			case <-ticker.C:
				f.logFailure(ctx, f.Flush(ctx))
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}

// logFailure logs rather than returns a failed pass: the live view is best
// effort, and the uploaded log is the record.
func (f *Flusher) logFailure(ctx context.Context, err error) {
	if err != nil {
		slog.WarnContext(ctx, "publish live log", "key", f.key, "offset", f.offset, "error", err)
	}
}
