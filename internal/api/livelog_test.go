package api

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
)

const liveLogEventsPath = "/v1/tenants/tenant_123/template-runs/run_123/logs/plan-1/events"

// fakeLiveLogs yields its entries to every subscriber, then closes the
// channel, as a registry does once its tailer is gone; with hold, it leaves
// the channel open, as for a command that prints nothing more.
type fakeLiveLogs struct {
	entries []logsink.Entry
	hold    bool
	// delay holds the entries back that long, as for a client that has
	// fallen behind its stream.
	delay time.Duration
	err   error
	// stored is what Entry finds, beside entries: what the stream held
	// before the client's Last-Event-ID.
	stored   []logsink.Entry
	entryErr error

	mu         sync.Mutex
	calls      int
	gotKey     string
	gotAfterID string
}

func (f *fakeLiveLogs) Subscribe(_ context.Context, key, afterID string) (<-chan logsink.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotKey, f.gotAfterID = key, afterID
	if f.err != nil {
		return nil, f.err
	}
	entries := make(chan logsink.Entry, len(f.entries))
	deliver := func() {
		for _, entry := range f.entries {
			entries <- entry
		}
		if !f.hold {
			close(entries)
		}
	}
	if f.delay > 0 {
		time.AfterFunc(f.delay, deliver)
	} else {
		deliver()
	}
	return entries, nil
}

func (f *fakeLiveLogs) Entry(_ context.Context, _, id string) (logsink.Entry, bool, error) {
	if f.entryErr != nil {
		return logsink.Entry{}, false, f.entryErr
	}
	for _, entry := range append(slices.Clone(f.stored), f.entries...) {
		if entry.ID == id {
			return entry, true, nil
		}
	}
	return logsink.Entry{}, false, nil
}

// newLiveLogDependencies is a running run whose plan-1 log is live.
func newLiveLogDependencies(t *testing.T) *apiTestDependencies {
	t.Helper()
	deps := newAPITestDependencies(t)
	deps.templateRuns.run = domain.TemplateRun{
		ID: "run_123", TenantID: "tenant_123", StackTemplateID: "stack_template_123",
		Status: domain.TemplateRunRunning,
	}
	deps.logMetadata.log = domain.TemplateRunLog{
		TenantID: "tenant_123", RunID: "run_123", Phase: "plan-1", State: domain.TemplateRunLogLive,
	}
	return deps
}

func streamLiveLog(t *testing.T, deps *apiTestDependencies, live *fakeLiveLogs, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	var options []ServerOption
	if live != nil {
		options = append(options, WithLiveLogs(live))
	}
	server := NewServer(deps.service(), configuredTenantID, options...)
	request := authenticatedRequest(http.MethodGet, liveLogEventsPath, nil)
	maps.Copy(request.Header, header)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func piece(id string, offset int64, data string) logsink.Entry {
	return logsink.Entry{ID: id, Kind: logsink.KindPiece, Offset: offset, Data: []byte(data)}
}

// sseLog is a log event as it goes over the wire.
func sseLog(id, data string) string {
	return "event: log\nid: " + id + "\ndata: " + data + "\n\n"
}

func sseEnd(state string) string {
	return "event: log_end\ndata: {\"state\":\"" + state + "\"}\n\n"
}

func TestStreamTemplateRunLogSendsPiecesThenEnd(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{
		piece("1-0", 0, "hello\n"),
		piece("2-0", 6, "world\n"),
		{ID: "3-0", Kind: logsink.KindTruncated},
		{ID: "4-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	for header, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-store",
		"X-Accel-Buffering": "no",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	want := sseLog("1-0", `{"offset":0,"end":6,"data":"hello\n"}`) +
		sseLog("2-0", `{"offset":6,"end":12,"data":"world\n"}`) +
		"event: log_truncated\ndata: {}\n\n" +
		sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if !response.Flushed {
		t.Fatal("stream was never flushed")
	}
	if live.gotKey != logsink.StreamKey("run_123", "plan-1") {
		t.Fatalf("subscribed to %q, want the log's stream", live.gotKey)
	}
}

func TestStreamTemplateRunLogAnswersEndForACompleteRow(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	deps.logMetadata.log.State = domain.TemplateRunLogComplete
	live := &fakeLiveLogs{}

	response := streamLiveLog(t, deps, live, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if got, want := response.Body.String(), sseEnd("complete"); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if live.calls != 0 {
		t.Fatalf("subscribed %d times for a complete log, want 0", live.calls)
	}
}

func TestStreamTemplateRunLogPassesLastEventID(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{{ID: "4-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete}}}

	streamLiveLog(t, deps, live, http.Header{"Last-Event-Id": {"3-0"}})

	if live.gotAfterID != "3-0" {
		t.Fatalf("afterID = %q, want 3-0", live.gotAfterID)
	}
}

func TestStreamTemplateRunLogReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{
		{ID: "1-0", Kind: logsink.KindPiece, Data: []byte{0xff, 'a'}},
		{ID: "2-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	// end counts the raw bytes, two, not the three of the replaced text.
	if want := sseLog("1-0", `{"offset":0,"end":2,"data":"�a"}`); !strings.HasPrefix(response.Body.String(), want) {
		t.Fatalf("body = %q, want it to start %q", response.Body.String(), want)
	}
}

func TestStreamTemplateRunLogSkipsADuplicateOffset(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{
		piece("1-0", 0, "abc"),
		// A retried append: the same bytes again, under a new id.
		piece("2-0", 0, "abc"),
		piece("3-0", 3, "def"),
		{ID: "4-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"end":3,"data":"abc"}`) +
		sseLog("3-0", `{"offset":3,"end":6,"data":"def"}`) +
		sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// A retried append that read further than the first try overlaps what was
// sent; only its new bytes go out.
func TestStreamTemplateRunLogSendsOnlyTheNewBytesOfAnOverlap(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{
		piece("1-0", 0, "abc"),
		piece("2-0", 0, "abcde"),
		{ID: "3-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"end":3,"data":"abc"}`) +
		sseLog("2-0", `{"offset":3,"end":5,"data":"de"}`) +
		sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// A channel that closes without an end (the subscriber lagged, or the tailer
// died) ends the stream with the row's state; live tells the client to
// reconnect from its last event.
func TestStreamTemplateRunLogEndsWithTheRowsStateWhenTheChannelCloses(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{piece("1-0", 0, "abc")}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"end":3,"data":"abc"}`) + sseEnd("live")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

func TestStreamTemplateRunLogAnswers503WithoutLiveLogs(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)

	response := streamLiveLog(t, deps, nil, nil)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", response.Code, response.Body.String())
	}
	if problem := decodeProblem(t, response); problem.Type != problemTypeURI("live-logs-unavailable") {
		t.Fatalf("type = %q, want live-logs-unavailable", problem.Type)
	}
}

// sseRetryLater is how a stream that could not follow its log ends: a
// reconnection delay, then an end that leaves the log live.
const sseRetryLater = "retry: 3000\n\n" + "event: log_end\ndata: {\"state\":\"live\"}\n\n"

// A Valkey that fails the subscription, likely for a moment, must not end
// the browser's live view: EventSource gives up for good on an error status,
// so the answer is a stream that asks it to reconnect.
func TestStreamTemplateRunLogRecoversFromASubscribeError(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"read failure": errors.New("i/o timeout"),
		// A full replica is as passing as a failed read.
		"too many tailers": logsink.ErrTooManyTailers,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deps := newLiveLogDependencies(t)

			response := streamLiveLog(t, deps, &fakeLiveLogs{err: err}, nil)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
				t.Fatalf("Content-Type = %q, want text/event-stream", got)
			}
			if got := response.Body.String(); got != sseRetryLater {
				t.Fatalf("body = %q, want %q", got, sseRetryLater)
			}
		})
	}
}

// Failing to read where a resuming client left off is the same passing
// failure, and is not answered by replaying the log from its start, which
// the client would show twice.
func TestStreamTemplateRunLogRecoversFromAFailedResumeRead(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entryErr: errors.New("i/o timeout"), entries: []logsink.Entry{piece("1-0", 0, "abc")}}

	response := streamLiveLog(t, deps, live, http.Header{"Last-Event-Id": {"1-0"}})

	if got := response.Body.String(); got != sseRetryLater {
		t.Fatalf("body = %q, want %q", got, sseRetryLater)
	}
	if live.calls != 0 {
		t.Fatalf("subscribed %d times without a resume point, want 0", live.calls)
	}
}

// The server is the only deduper: a reconnecting client is sent nothing it
// already holds, including a retried append of bytes it had before the
// reconnect.
func TestStreamTemplateRunLogResumesFromLastEventID(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{
		stored: []logsink.Entry{piece("1-0", 0, "abc")},
		// What the stream holds after 1-0, as the registry yields it.
		entries: []logsink.Entry{
			// A retry of the first append, which the client has.
			piece("1-1", 0, "abc"),
			piece("2-0", 3, "def"),
			// A retry of the second.
			piece("2-1", 3, "def"),
			piece("3-0", 6, "ghi"),
			{ID: "4-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
		},
	}

	response := streamLiveLog(t, deps, live, http.Header{"Last-Event-Id": {"1-0"}})

	want := sseLog("2-0", `{"offset":3,"end":6,"data":"def"}`) +
		sseLog("3-0", `{"offset":6,"end":9,"data":"ghi"}`) +
		sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// A Last-Event-ID the stream no longer holds says nothing about what the
// client has, so it is sent everything that follows.
func TestStreamTemplateRunLogResumesFromTheStartWhenTheEntryIsGone(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []logsink.Entry{
		piece("5-0", 0, "abc"),
		{ID: "6-0", Kind: logsink.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, http.Header{"Last-Event-Id": {"1-0"}})

	want := sseLog("5-0", `{"offset":0,"end":3,"data":"abc"}`) + sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// The end entry is appended after the row is written and can fail to land;
// the keepalive's re-read of the row is what ends such a stream. Not
// parallel: it shortens the package's keepalive interval.
// shortenLiveLogTimers makes the keepalive tick at once and the drain after a
// row ends brief. Tests that call it change package variables, so they are
// not parallel.
func shortenLiveLogTimers(t *testing.T, grace time.Duration) {
	t.Helper()
	interval, drain := liveLogKeepalive, liveLogDrainGrace
	liveLogKeepalive, liveLogDrainGrace = 10*time.Millisecond, grace
	t.Cleanup(func() { liveLogKeepalive, liveLogDrainGrace = interval, drain })
}

func TestStreamTemplateRunLogEndsWhenTheRowFlipsWhileConnected(t *testing.T) {
	shortenLiveLogTimers(t, 20*time.Millisecond)
	deps := newLiveLogDependencies(t)
	complete := deps.logMetadata.log
	complete.State = domain.TemplateRunLogComplete
	deps.logMetadata.thenLog = &complete
	live := &fakeLiveLogs{hold: true}

	response := streamLiveLog(t, deps, live, nil)

	if got, want := response.Body.String(), ":\n\n"+sseEnd("complete"); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// A failed re-read leaves the stream following the log: the next tick asks
// again. Not parallel, as above.
func TestStreamTemplateRunLogKeepsFollowingWhenARowReReadFails(t *testing.T) {
	shortenLiveLogTimers(t, 20*time.Millisecond)
	deps := newLiveLogDependencies(t)
	complete := deps.logMetadata.log
	complete.State = domain.TemplateRunLogComplete
	deps.logMetadata.thenLog = &complete
	deps.logMetadata.thenErrs = 1
	live := &fakeLiveLogs{hold: true}

	response := streamLiveLog(t, deps, live, nil)

	if got, want := response.Body.String(), ":\n\n:\n\n"+sseEnd("complete"); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// A row can end while a slow client's last pieces are still on their way:
// the row is written before the end entry. They go out before the end. Not
// parallel, as above.
func TestStreamTemplateRunLogSendsQueuedPiecesBeforeEndingOnARowCheck(t *testing.T) {
	shortenLiveLogTimers(t, time.Second)
	deps := newLiveLogDependencies(t)
	complete := deps.logMetadata.log
	complete.State = domain.TemplateRunLogComplete
	deps.logMetadata.thenLog = &complete
	// The pieces arrive after the first tick has seen the row complete.
	live := &fakeLiveLogs{entries: []logsink.Entry{piece("1-0", 0, "last\n")}, hold: true, delay: 100 * time.Millisecond}

	response := streamLiveLog(t, deps, live, nil)

	want := ":\n\n" + sseLog("1-0", `{"offset":0,"end":5,"data":"last\n"}`) + sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// Every viewer of a log on a replica shares its re-check: a second ask
// within the interval reads nothing.
func TestLiveLogStateCacheSharesOneReadPerInterval(t *testing.T) {
	t.Parallel()

	cache := newLiveLogStateCache()
	now := time.Unix(0, 0)
	cache.now = func() time.Time { return now }
	reads := 0
	fetch := func(context.Context) (domain.TemplateRunLogState, error) {
		reads++
		return domain.TemplateRunLogLive, nil
	}

	for range 3 {
		if _, err := cache.get(context.Background(), "t/r/plan-1", 20*time.Second, fetch); err != nil {
			t.Fatalf("get returned error: %v", err)
		}
	}
	if reads != 1 {
		t.Fatalf("reads = %d within one interval, want 1", reads)
	}

	now = now.Add(20 * time.Second)
	if _, err := cache.get(context.Background(), "t/r/plan-1", 20*time.Second, fetch); err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if reads != 2 {
		t.Fatalf("reads = %d after the interval, want 2", reads)
	}
}

// A failed read is not kept, and a log nobody asks about is swept.
func TestLiveLogStateCacheKeepsNoFailureAndSweepsStaleLogs(t *testing.T) {
	t.Parallel()

	cache := newLiveLogStateCache()
	now := time.Unix(0, 0)
	cache.now = func() time.Time { return now }
	failing := func(context.Context) (domain.TemplateRunLogState, error) {
		return "", errors.New("database unavailable")
	}
	if _, err := cache.get(context.Background(), "t/r/plan-1", time.Second, failing); err == nil {
		t.Fatal("get returned no error for a failed read")
	}
	if len(cache.entries) != 0 {
		t.Fatalf("entries = %v, want the failure not kept", cache.entries)
	}

	live := func(context.Context) (domain.TemplateRunLogState, error) { return domain.TemplateRunLogLive, nil }
	if _, err := cache.get(context.Background(), "t/r/plan-1", time.Second, live); err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	now = now.Add(4 * time.Second)
	if _, err := cache.get(context.Background(), "t/r/plan-2", time.Second, live); err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if _, ok := cache.entries["t/r/plan-1"]; ok {
		t.Fatal("plan-1, unasked for three intervals, was not swept")
	}
}

func TestStreamTemplateRunLogIsATenantRoute(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{}
	server := NewServer(deps.service(), configuredTenantID, WithLiveLogs(live))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, liveLogEventsPath, nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", response.Code, response.Body.String())
	}
	if live.calls != 0 {
		t.Fatal("subscribed for an unauthenticated request")
	}
}

// unflushableWriter is a ResponseWriter that cannot flush, which would hold
// every event back until the stream ends.
type unflushableWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *unflushableWriter) Header() http.Header         { return w.header }
func (w *unflushableWriter) WriteHeader(code int)        { w.code = code }
func (w *unflushableWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

func TestStreamTemplateRunLogAnswers500WhenItCannotFlush(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{}
	server := NewServer(deps.service(), configuredTenantID, WithLiveLogs(live))
	response := &unflushableWriter{header: http.Header{}}

	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, liveLogEventsPath, nil))

	if response.code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", response.code, response.body.String())
	}
	if live.calls != 0 {
		t.Fatal("subscribed although no event could reach the client")
	}
}
