package livelog

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/domain"
)

const (
	// LiveLimit is how much of a log its stream carries. Past it the writer
	// appends one truncated entry and stops: the rest is read once uploaded.
	LiveLimit int64 = 8 << 20
	// PieceSize caps one piece entry, so a burst of output is several
	// entries rather than one that a reader has to take whole.
	PieceSize int64 = 1 << 20
	// FlushInterval is the longest output waits before it reaches the
	// stream.
	FlushInterval = time.Second
	// InitialTTL outlives any command, so a stream only expires on its own
	// when its writer died before ending it.
	InitialTTL = 24 * time.Hour
	// EndTTL is how long an ended stream stays: long enough for a reader to
	// see the end entry, after which the uploaded log serves.
	EndTTL = time.Minute
)

// StreamKey is the stream a run log's output goes to. The braces are a
// cluster hash tag: every log of a run lands on the same slot.
func StreamKey(runID domain.TemplateRunID, name string) string {
	return "runlog:{" + string(runID) + "}:" + name
}

// Kind is what a stream entry says about its log.
type Kind string

const (
	// KindPiece carries the log's next bytes.
	KindPiece Kind = "piece"
	// KindTruncated says the log outgrew LiveLimit; no piece follows it.
	KindTruncated Kind = "truncated"
	// KindEnd says the command finished, and where its output now lives.
	KindEnd Kind = "end"
)

// The entry's field names, kept short: they repeat in every entry.
const (
	fieldKind   = "kind"
	fieldOffset = "off"
	fieldData   = "data"
	fieldState  = "state"
)

// Entry is one stream entry.
type Entry struct {
	ID   string
	Kind Kind
	// Offset is a KindPiece's first byte in the log, so a reader that
	// already holds some of the log can tell which bytes are new.
	Offset int64
	// Data is a KindPiece's bytes.
	Data []byte
	// State is a KindEnd's outcome: complete or lost.
	State domain.TemplateRunLogState
}

// Values is the entry as an XADD field map. Every value is a string, as a
// read hands it back, so ParseEntry reads both alike.
func (e Entry) Values() map[string]any {
	values := map[string]any{fieldKind: string(e.Kind)}
	switch e.Kind {
	case KindPiece:
		values[fieldOffset] = strconv.FormatInt(e.Offset, 10)
		values[fieldData] = string(e.Data)
	case KindEnd:
		values[fieldState] = string(e.State)
	}
	return values
}

// ParseEntry reads an entry back from a stream message. It refuses one it
// cannot make sense of rather than hand a reader a half-read entry.
func ParseEntry(m redis.XMessage) (Entry, error) {
	entry := Entry{ID: m.ID, Kind: Kind(stringValue(m.Values, fieldKind))}
	switch entry.Kind {
	case KindPiece:
		raw, ok := m.Values[fieldOffset].(string)
		if !ok {
			return Entry{}, fmt.Errorf("live log entry %s: piece has no offset", m.ID)
		}
		offset, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || offset < 0 {
			return Entry{}, fmt.Errorf("live log entry %s: piece offset %q is not a byte offset", m.ID, raw)
		}
		data, ok := m.Values[fieldData].(string)
		if !ok {
			return Entry{}, fmt.Errorf("live log entry %s: piece has no data", m.ID)
		}
		entry.Offset, entry.Data = offset, []byte(data)
	case KindTruncated:
	case KindEnd:
		// A log ends complete or lost; live would claim it is still running.
		entry.State = domain.TemplateRunLogState(stringValue(m.Values, fieldState))
		if entry.State != domain.TemplateRunLogComplete && entry.State != domain.TemplateRunLogLost {
			return Entry{}, fmt.Errorf("live log entry %s: end state %q is not complete or lost", m.ID, entry.State)
		}
	default:
		return Entry{}, fmt.Errorf("live log entry %s: unknown kind %q", m.ID, entry.Kind)
	}
	return entry, nil
}

func stringValue(values map[string]any, field string) string {
	value, _ := values[field].(string)
	return value
}

// Stream is what a writer needs; *redis.Client satisfies it.
type Stream interface {
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	Expire(ctx context.Context, key string, ttl time.Duration) *redis.BoolCmd
}

// Reader is what the api's tailer needs; *redis.Client satisfies it.
type Reader interface {
	XRead(ctx context.Context, a *redis.XReadArgs) *redis.XStreamSliceCmd
	XRange(ctx context.Context, stream, start, stop string) *redis.XMessageSliceCmd
	// XRevRangeN reads the stream's last entry, which bounds where a
	// tailer may start.
	XRevRangeN(ctx context.Context, stream, start, stop string, count int64) *redis.XMessageSliceCmd
}

var (
	_ Stream = (*redis.Client)(nil)
	_ Reader = (*redis.Client)(nil)
)

// AppendEnd ends the stream at key with the log's state, then shortens its
// expiry to EndTTL: once ended, the stream only has to last until its readers
// have seen the end.
func AppendEnd(ctx context.Context, s Stream, key string, state domain.TemplateRunLogState) error {
	end := Entry{Kind: KindEnd, State: state}
	if err := s.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: end.Values()}).Err(); err != nil {
		return fmt.Errorf("append end to %s: %w", key, err)
	}
	if err := s.Expire(ctx, key, EndTTL).Err(); err != nil {
		return fmt.Errorf("expire %s: %w", key, err)
	}
	return nil
}
