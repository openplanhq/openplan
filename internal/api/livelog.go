package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/livelog"
)

// LiveLogSubscriber follows a live log's stream. *livelog.Registry satisfies
// it; handler tests use a fake.
type LiveLogSubscriber interface {
	Subscribe(ctx context.Context, key, afterID string) (<-chan livelog.Entry, error)
	// Entry reads the entry at id, which tells a resuming client's stream
	// how much of the log the client already holds.
	Entry(ctx context.Context, key, id string) (entry livelog.Entry, found bool, err error)
}

// liveLogKeepalive is how often a live stream writes a comment, which keeps
// proxies from closing it while the command is quiet, and re-checks its row.
// A variable so tests can shorten it.
var liveLogKeepalive = 20 * time.Second

// liveLogDrainGrace is how long a stream whose row has ended keeps sending
// what is still on its way before it ends. The row is written before the end
// entry is appended, so a re-check can see the end while a slow client's
// last pieces are still queued behind it. A variable so tests can shorten it.
var liveLogDrainGrace = 2 * time.Second

// liveLogRetryMillis is the reconnection delay a stream that could not follow
// its log asks the browser for.
const liveLogRetryMillis = 3000

var _ LiveLogSubscriber = (*livelog.Registry)(nil)

// handleStreamTemplateRunLog streams a run log as server-sent events while
// its command runs. Authorization is the row read's, as for the body.
func (server *Server) handleStreamTemplateRunLog(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	command := app.GetTemplateRunLogCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
		Name:     request.PathValue("name"),
	}
	runLog, err := server.service.GetTemplateRunLogMetadata(ctx, command)
	if err != nil {
		writeAppError(response, err)
		return
	}
	// Checked before subscribing: without flushing, events would sit in a
	// buffer until the command ends, which is no live view at all.
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeInternalError(response, errors.New("live log stream: response writer cannot flush"))
		return
	}
	if runLog.State != domain.TemplateRunLogLive {
		stream := startEventStream(response, flusher)
		_ = stream.end(runLog.State)
		return
	}
	if server.liveLogs == nil {
		writeProblem(response, problemDocument{
			Type: problemTypeURI("live-logs-unavailable"), Title: "Live logs aren't configured",
			Status: http.StatusServiceUnavailable, Detail: "The API was started without live logs. Read the log once its command finishes.",
		})
		return
	}

	// From here the answer is a stream whatever happens. Failing to follow
	// the log is Valkey's failure, likely a passing one, and an error status
	// would end EventSource for good; a stream that ends live has it
	// reconnect from its last event instead.
	stream := startEventStream(response, flusher)
	key := livelog.StreamKey(command.RunID, command.Name)
	lastEventID := request.Header.Get("Last-Event-ID")
	sent, err := server.resumePoint(ctx, key, lastEventID)
	var entries <-chan livelog.Entry
	if err == nil {
		entries, err = server.liveLogs.Subscribe(ctx, key, lastEventID)
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("api: live log stream: follow %s of run %s: %v", command.Name, command.RunID, err)
			_ = stream.retryLater()
		}
		return
	}

	server.streamEntries(ctx, stream, entries, command, sent)
}

// resumePoint is how much of the log a client resuming after lastEventID
// already holds: the end of that piece, so what the stream replays after it
// is cut to what the client lacks, across reconnects. With no
// Last-Event-ID, or one the stream no longer holds (it expired, or the
// client made it up), it is 0 and the client is sent all that follows.
func (server *Server) resumePoint(ctx context.Context, key, lastEventID string) (int64, error) {
	if lastEventID == "" {
		return 0, nil
	}
	entry, found, err := server.liveLogs.Entry(ctx, key, lastEventID)
	if err != nil {
		return 0, err
	}
	if !found || entry.Kind != livelog.KindPiece {
		return 0, nil
	}
	return entry.Offset + int64(len(entry.Data)), nil
}

// streamEntries sends each entry as its event until the log ends, the
// channel closes, or the client goes away. sent is how much of the log the
// client already holds.
func (server *Server) streamEntries(ctx context.Context, stream eventStream, entries <-chan livelog.Entry, command app.GetTemplateRunLogCommand, sent int64) {
	keepalive := time.NewTicker(liveLogKeepalive)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			state, ok := server.keepAlive(ctx, stream, command)
			if !ok {
				return
			}
			if state != domain.TemplateRunLogLive {
				drainThenEnd(ctx, stream, entries, &sent, state)
				return
			}
		case entry, open := <-entries:
			if !open {
				server.endClosedStream(ctx, stream, command)
				return
			}
			if !stream.sendEntry(entry, &sent) {
				return
			}
		}
	}
}

// drainThenEnd ends a stream whose row has ended before its end entry
// arrived. What is still queued goes out first, for up to liveLogDrainGrace,
// so a client that fell behind still gets the last lines, usually the ones
// that say how the command ended; the end entry, if it comes in that time,
// ends the stream itself.
func drainThenEnd(ctx context.Context, stream eventStream, entries <-chan livelog.Entry, sent *int64, state domain.TemplateRunLogState) {
	grace := time.NewTimer(liveLogDrainGrace)
	defer grace.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-grace.C:
			_ = stream.end(state)
			return
		case entry, open := <-entries:
			if !open {
				_ = stream.end(state)
				return
			}
			if !stream.sendEntry(entry, sent) {
				return
			}
		}
	}
}

// endClosedStream ends a stream whose channel closed without an end entry:
// the subscriber fell behind, or its tailer stopped. The row says what
// happened since; if the log is still live, the client reconnects from its
// last event.
func (server *Server) endClosedStream(ctx context.Context, stream eventStream, command app.GetTemplateRunLogCommand) {
	if ctx.Err() != nil {
		return
	}
	runLog, err := server.service.GetTemplateRunLogMetadata(ctx, command)
	if err != nil {
		// The 200 is already sent; dropping the connection makes the client
		// reconnect, which reads the row again.
		log.Printf("api: live log stream: re-read %s of run %s: %v", command.Name, command.RunID, err)
		return
	}
	_ = stream.end(runLog.State)
}

// keepAlive writes a comment and re-checks the row, returning its state and
// whether the stream goes on. The row is written before the end entry is
// appended, so a row that ended while its end entry never landed would
// otherwise hold the stream open until the client gave up.
//
// The re-check is the replica's, shared by every viewer of the log through
// liveLogStates, so a log watched by many costs one read per interval. It is
// not authorized again: the viewer was when the stream opened, as a body read
// is when it starts.
func (server *Server) keepAlive(ctx context.Context, stream eventStream, command app.GetTemplateRunLogCommand) (domain.TemplateRunLogState, bool) {
	if err := stream.comment(); err != nil {
		return "", false
	}
	key := string(command.TenantID) + "/" + string(command.RunID) + "/" + command.Name
	state, err := server.liveLogStates.get(ctx, key, liveLogKeepalive, func(ctx context.Context) (domain.TemplateRunLogState, error) {
		return server.service.TemplateRunLogState(ctx, command.TenantID, command.RunID, command.Name)
	})
	if err != nil {
		// Asked again next tick; a failed read is no reason to stop
		// following a log that is likely still live.
		if ctx.Err() == nil {
			log.Printf("api: live log stream: re-check %s of run %s: %v", command.Name, command.RunID, err)
		}
		return domain.TemplateRunLogLive, ctx.Err() == nil
	}
	return state, true
}

// liveLogStateCache holds each live log's last re-checked state for the
// replica's streams of it, so the re-checks cost reads per log, not per
// viewer.
type liveLogStateCache struct {
	mu      sync.Mutex
	entries map[string]cachedLogState
	// now is time.Now; tests replace it.
	now func() time.Time
}

type cachedLogState struct {
	state domain.TemplateRunLogState
	at    time.Time
}

func newLiveLogStateCache() *liveLogStateCache {
	return &liveLogStateCache{entries: map[string]cachedLogState{}, now: time.Now}
}

// get returns key's state, read by fetch unless a read younger than maxAge is
// held. A failed read is not kept, so the next caller asks again. Entries no
// one has asked for in a while are swept as reads are stored, which bounds
// the map by the logs being watched.
func (cache *liveLogStateCache) get(ctx context.Context, key string, maxAge time.Duration, fetch func(context.Context) (domain.TemplateRunLogState, error)) (domain.TemplateRunLogState, error) {
	cache.mu.Lock()
	cached, ok := cache.entries[key]
	now := cache.now()
	cache.mu.Unlock()
	if ok && now.Sub(cached.at) < maxAge {
		return cached.state, nil
	}

	// Read outside the lock: one slow database read must not hold up every
	// other log's streams. Two viewers whose ticks meet may both read, which
	// is harmless.
	state, err := fetch(ctx)
	if err != nil {
		return "", err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now = cache.now()
	for other, entry := range cache.entries {
		if now.Sub(entry.at) > 3*maxAge {
			delete(cache.entries, other)
		}
	}
	cache.entries[key] = cachedLogState{state: state, at: now}
	return state, nil
}

type logEventData struct {
	Offset int64 `json:"offset"`
	// End is the byte after the piece, counted on the raw bytes, which
	// Data's replacement of invalid UTF-8 would make a client miscount.
	End  int64  `json:"end"`
	Data string `json:"data"`
}

type logEndData struct {
	State domain.TemplateRunLogState `json:"state"`
}

// eventStream writes server-sent events, flushing each so it reaches the
// browser as it happens.
type eventStream struct {
	response http.ResponseWriter
	flusher  http.Flusher
}

func startEventStream(response http.ResponseWriter, flusher http.Flusher) eventStream {
	header := response.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	// nginx buffers proxied responses by default, which would hold events
	// back; this header turns that off for this response alone.
	header.Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	return eventStream{response: response, flusher: flusher}
}

// send writes one event. id is left out when empty. The error is the
// client's connection failing, after which nothing more can be sent.
func (stream eventStream) send(event, id string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", event, err)
	}
	var frame strings.Builder
	frame.WriteString("event: " + event + "\n")
	if id != "" {
		frame.WriteString("id: " + id + "\n")
	}
	frame.WriteString("data: ")
	frame.Write(payload)
	frame.WriteString("\n\n")
	if _, err := stream.response.Write([]byte(frame.String())); err != nil {
		return fmt.Errorf("write %s event: %w", event, err)
	}
	stream.flusher.Flush()
	return nil
}

// sendEntry sends entry as its event and reports whether the stream goes on:
// it does not after the end entry, or once the client's connection fails.
//
// sent is how much of the log the client holds, and grows with every piece
// sent. A retried append repeats bytes under a new id, so a piece is cut to
// what lies past sent, and one wholly behind it is skipped.
func (stream eventStream) sendEntry(entry livelog.Entry, sent *int64) bool {
	switch entry.Kind {
	case livelog.KindPiece:
		end := entry.Offset + int64(len(entry.Data))
		if end <= *sent {
			return true
		}
		data, offset := entry.Data, entry.Offset
		if offset < *sent {
			data, offset = data[*sent-offset:], *sent
		}
		*sent = end
		return stream.send("log", entry.ID, logEventData{Offset: offset, End: end, Data: strings.ToValidUTF8(string(data), "�")}) == nil
	case livelog.KindTruncated:
		return stream.send("log_truncated", "", struct{}{}) == nil
	case livelog.KindEnd:
		_ = stream.end(entry.State)
		return false
	}
	return true
}

func (stream eventStream) end(state domain.TemplateRunLogState) error {
	return stream.send("log_end", "", logEndData{State: state})
}

// comment writes an SSE comment, which a browser ignores.
func (stream eventStream) comment() error {
	if _, err := stream.response.Write([]byte(":\n\n")); err != nil {
		return fmt.Errorf("write keepalive: %w", err)
	}
	stream.flusher.Flush()
	return nil
}

// retryLater ends a stream that could not follow its log: it sets the
// browser's reconnection delay, then ends the stream as still live, which
// has EventSource reconnect with Last-Event-ID.
func (stream eventStream) retryLater() error {
	if _, err := fmt.Fprintf(stream.response, "retry: %d\n\n", liveLogRetryMillis); err != nil {
		return fmt.Errorf("write retry: %w", err)
	}
	return stream.end(domain.TemplateRunLogLive)
}
