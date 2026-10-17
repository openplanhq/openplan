package livelog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// MaxTailers caps the tailers one replica runs at once, counting a
	// stopped one whose last read is still in flight. Each holds a Valkey
	// connection in a blocking read, so the cap bounds the connections
	// viewers can tie up, however many there are or however fast they come
	// and go.
	MaxTailers = 256
	// subscriberBuffer is how far a subscriber may fall behind its tailer.
	// Past it the subscriber is dropped rather than slowing the tailer down
	// for everyone; its client reconnects from the last event it saw.
	subscriberBuffer = 64
	// tailBlock bounds one blocking read. go-redis does not abandon a read
	// when its context ends, so it is also how long a stopped tailer can
	// linger before it notices.
	tailBlock = 5 * time.Second
)

// ErrTooManyTailers refuses a subscriber whose log would need a tailer past
// MaxTailers.
var ErrTooManyTailers = errors.New("too many live log tailers")

// Registry shares one tailer per live log among every subscriber to it on
// this replica, so a log watched by many browsers costs one Valkey read.
type Registry struct {
	reader Reader

	// mu guards tailers, slots, and every tailer's subscribers, closed and
	// started. One lock for all of it keeps the hand-offs between a tailer,
	// its subscribers and the map obvious; nothing slow happens under it.
	mu      sync.Mutex
	tailers map[string]*tailer
	// slots counts tailers that hold or will hold a connection: from
	// creation until their read loop returns, or until teardown for one
	// that never started. A torn-down tailer leaves the map at once, but
	// go-redis keeps its read, and the connection, for up to tailBlock.
	slots int
}

// tailer follows one stream and fans each entry out to its subscribers.
type tailer struct {
	key string
	// ready closes once the creating subscriber has read its backlog and
	// fixed where the tailer starts reading. A joining subscriber reads its
	// own backlog only after that: read earlier, the entries between its
	// backlog and the tailer's start would reach it by neither.
	ready chan struct{}
	// stop ends the tailer's read loop.
	stop context.CancelFunc
	ctx  context.Context

	subscribers map[*subscriber]struct{}
	// closed is set once the tailer has left the registry: it ended, failed,
	// or lost its last subscriber. Nothing is fanned out after it.
	closed bool
	// started is set once the read loop runs; from then on the loop, not
	// teardown, gives back the tailer's slot.
	started bool
}

// subscriber is one Subscribe call's view of its tailer. The registry closes
// inbox, under mu, when it removes the subscriber from the tailer.
type subscriber struct {
	inbox chan Entry
}

// NewRegistry returns a registry reading streams through reader.
func NewRegistry(reader Reader) *Registry {
	return &Registry{reader: reader, tailers: map[string]*tailer{}}
}

// Subscribe returns entries after afterID ("" or "0-0" for the start):
// first the backlog by XRANGE, then live from a tailer shared by every
// subscriber of key on this replica. The channel closes after an end entry,
// when ctx ends, or when the subscriber falls 64 entries behind.
//
// An afterID that is not a stream id is read as the start: it can only come
// from a hand-written Last-Event-ID, and replaying the log is harmless.
func (reg *Registry) Subscribe(ctx context.Context, key, afterID string) (<-chan Entry, error) {
	if _, _, ok := parseID(afterID); !ok {
		afterID = ""
	}
	t, sub, created, err := reg.join(key)
	if err != nil {
		return nil, err
	}
	if !created {
		// If the creator failed, this subscriber's inbox is already closed:
		// it still gets its backlog, then a close that sends its client
		// back to reconnect.
		select {
		case <-t.ready:
		case <-ctx.Done():
			reg.leave(t, sub)
			return nil, ctx.Err()
		}
	}

	backlog, err := reg.backlog(ctx, key, afterID)
	var from string
	var ended bool
	if err == nil && created {
		from, ended, err = reg.position(ctx, key, backlog, afterID)
	}
	if err != nil {
		if created {
			reg.mu.Lock()
			reg.teardown(t)
			reg.mu.Unlock()
			close(t.ready)
		} else {
			reg.leave(t, sub)
		}
		return nil, err
	}
	if created {
		reg.start(t, from, ended)
	}

	// Inbox entries at or before last are already in the backlog: the
	// tailer may have fanned them out while the backlog was being read.
	last := afterID
	if len(backlog) > 0 {
		last = backlog[len(backlog)-1].ID
	}
	out := make(chan Entry)
	go reg.forward(ctx, t, sub, backlog, last, out)
	return out, nil
}

// join registers a subscriber with key's tailer, creating the tailer if key
// has none; created says this subscriber has to start it.
func (reg *Registry) join(key string) (*tailer, *subscriber, bool, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	t, ok := reg.tailers[key]
	if !ok {
		if reg.slots >= MaxTailers {
			return nil, nil, false, ErrTooManyTailers
		}
		reg.slots++
		// Not the creating request's context: the tailer outlives that
		// request when others have joined it.
		ctx, stop := context.WithCancel(context.Background())
		t = &tailer{key: key, ready: make(chan struct{}), ctx: ctx, stop: stop, subscribers: map[*subscriber]struct{}{}}
		reg.tailers[key] = t
	}
	sub := &subscriber{inbox: make(chan Entry, subscriberBuffer)}
	t.subscribers[sub] = struct{}{}
	return t, sub, !ok, nil
}

// position says where the tailer starts: after the creator's backlog, so
// the two meet with no gap. ended says the stream already holds its end, so
// no tailer is needed.
func (reg *Registry) position(ctx context.Context, key string, backlog []Entry, afterID string) (string, bool, error) {
	if len(backlog) > 0 {
		last := backlog[len(backlog)-1]
		return last.ID, last.Kind == KindEnd, nil
	}
	if afterID == "" {
		return "0-0", false, nil
	}
	// An empty backlog leaves only the client's afterID, which every viewer
	// of the log then shares. Past the stream's last entry it would park the
	// tailer where no generated id ever arrives, so it is clamped to that
	// entry.
	messages, err := reg.reader.XRevRangeN(ctx, key, "+", "-", 1).Result()
	if err != nil {
		return "", false, fmt.Errorf("read last entry of %s: %w", key, err)
	}
	if len(messages) == 0 {
		return "0-0", false, nil
	}
	last := messages[0]
	if !idAfter(afterID, last.ID) {
		return afterID, false, nil
	}
	entry, err := ParseEntry(last)
	return last.ID, err == nil && entry.Kind == KindEnd, nil
}

// start sets the tailer reading after from, then lets joiners in.
func (reg *Registry) start(t *tailer, from string, ended bool) {
	reg.mu.Lock()
	if ended {
		reg.teardown(t)
	} else if !t.closed {
		t.started = true
		go reg.tail(t, from)
	}
	reg.mu.Unlock()
	close(t.ready)
}

// backlog reads the stream's entries after afterID, up to and including the
// first end entry.
func (reg *Registry) backlog(ctx context.Context, key, afterID string) ([]Entry, error) {
	start := afterID
	if start == "" {
		start = "-"
	}
	messages, err := reg.reader.XRange(ctx, key, start, "+").Result()
	if err != nil {
		return nil, fmt.Errorf("read backlog of %s: %w", key, err)
	}
	entries := make([]Entry, 0, len(messages))
	for _, message := range messages {
		// XRANGE's start is inclusive, and the client already has afterID.
		if message.ID == afterID {
			continue
		}
		entry, ok := parseOrSkip(key, message)
		if !ok {
			continue
		}
		entries = append(entries, entry)
		if entry.Kind == KindEnd {
			break
		}
	}
	return entries, nil
}

// tail reads t's stream from the entry after from until the end entry, a
// read error, or the last subscriber leaving.
func (reg *Registry) tail(t *tailer, from string) {
	defer func() {
		reg.mu.Lock()
		reg.slots--
		reg.mu.Unlock()
	}()
	for {
		// A missing key is not an error: XREAD blocks until the first
		// piece creates the stream, which is the first viewer's usual case.
		streams, err := reg.reader.XRead(t.ctx, &redis.XReadArgs{Streams: []string{t.key, from}, Block: tailBlock}).Result()
		if t.ctx.Err() != nil {
			return
		}
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			// Its subscribers' channels close without an end, which tells
			// each client to reconnect, and the next one starts afresh.
			slog.Warn("live log tailer stopped", "key", t.key, "error", err)
			reg.mu.Lock()
			reg.teardown(t)
			reg.mu.Unlock()
			return
		}
		var batch []Entry
		for _, stream := range streams {
			for _, message := range stream.Messages {
				from = message.ID
				if entry, ok := parseOrSkip(t.key, message); ok {
					batch = append(batch, entry)
				}
			}
		}
		if reg.fanOut(t, batch) {
			return
		}
	}
}

// fanOut hands batch to every subscriber of t and reports whether the tailer
// is done: it saw the end, or nobody is left to read.
func (reg *Registry) fanOut(t *tailer, batch []Entry) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if t.closed {
		return true
	}
	for _, entry := range batch {
		for sub := range t.subscribers {
			select {
			case sub.inbox <- entry:
			default:
				// Fallen subscriberBuffer behind: dropped, not waited for.
				delete(t.subscribers, sub)
				close(sub.inbox)
			}
		}
		// Only the first end counts: a retried RecordTemplateRunLog can
		// append a second one.
		if entry.Kind == KindEnd {
			reg.teardown(t)
			return true
		}
	}
	if len(t.subscribers) == 0 {
		reg.teardown(t)
		return true
	}
	return false
}

// forward feeds out the backlog at the reader's pace, then the inbox,
// skipping what the backlog already carried. It closes out when the log
// ends, the inbox closes, or ctx ends.
func (reg *Registry) forward(ctx context.Context, t *tailer, sub *subscriber, backlog []Entry, last string, out chan<- Entry) {
	defer close(out)
	defer reg.leave(t, sub)
	send := func(entry Entry) bool {
		select {
		case out <- entry:
			return entry.Kind != KindEnd
		case <-ctx.Done():
			return false
		}
	}
	for _, entry := range backlog {
		if !send(entry) {
			return
		}
	}
	for {
		select {
		case entry, ok := <-sub.inbox:
			if !ok {
				return
			}
			if last != "" && !idAfter(entry.ID, last) {
				continue
			}
			if !send(entry) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// leave removes sub from t, and stops t if sub was its last subscriber.
func (reg *Registry) leave(t *tailer, sub *subscriber) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, ok := t.subscribers[sub]; ok {
		delete(t.subscribers, sub)
		close(sub.inbox)
	}
	if len(t.subscribers) == 0 {
		reg.teardown(t)
	}
}

// teardown takes t out of the registry, closes every subscriber's inbox and
// stops its read loop. The caller holds mu.
func (reg *Registry) teardown(t *tailer) {
	if t.closed {
		return
	}
	t.closed = true
	for sub := range t.subscribers {
		delete(t.subscribers, sub)
		close(sub.inbox)
	}
	if reg.tailers[t.key] == t {
		delete(reg.tailers, t.key)
	}
	if !t.started {
		reg.slots--
	}
	t.stop()
}

// parseOrSkip parses a stream message, skipping one it cannot make sense
// of: stopping on it would leave every reconnecting client stuck on it.
func parseOrSkip(key string, message redis.XMessage) (Entry, bool) {
	entry, err := ParseEntry(message)
	if err != nil {
		slog.Warn("live log entry skipped", "key", key, "error", err)
		return Entry{}, false
	}
	return entry, true
}

// parseID splits a stream id, milliseconds-sequence, into its parts.
func parseID(id string) (ms, seq uint64, ok bool) {
	msPart, seqPart, found := strings.Cut(id, "-")
	if !found {
		return 0, 0, false
	}
	ms, msErr := strconv.ParseUint(msPart, 10, 64)
	seq, seqErr := strconv.ParseUint(seqPart, 10, 64)
	return ms, seq, msErr == nil && seqErr == nil
}

// idAfter reports whether stream id a comes after b. Both come from Valkey,
// or from afterID, which Subscribe has checked.
func idAfter(a, b string) bool {
	aMS, aSeq, _ := parseID(a)
	bMS, bSeq, _ := parseID(b)
	return aMS > bMS || (aMS == bMS && aSeq > bSeq)
}
