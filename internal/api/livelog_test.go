package api

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/livelog"
)

const liveLogEventsPath = "/v1/tenants/tenant_123/template-runs/run_123/logs/plan-1/events"

// fakeLiveLogs yields its entries to every subscriber, then closes the
// channel, as a registry does once its tailer is gone.
type fakeLiveLogs struct {
	entries []livelog.Entry
	err     error

	mu         sync.Mutex
	calls      int
	gotKey     string
	gotAfterID string
}

func (f *fakeLiveLogs) Subscribe(_ context.Context, key, afterID string) (<-chan livelog.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotKey, f.gotAfterID = key, afterID
	if f.err != nil {
		return nil, f.err
	}
	entries := make(chan livelog.Entry, len(f.entries))
	for _, entry := range f.entries {
		entries <- entry
	}
	close(entries)
	return entries, nil
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

func piece(id string, offset int64, data string) livelog.Entry {
	return livelog.Entry{ID: id, Kind: livelog.KindPiece, Offset: offset, Data: []byte(data)}
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
	live := &fakeLiveLogs{entries: []livelog.Entry{
		piece("1-0", 0, "hello\n"),
		piece("2-0", 6, "world\n"),
		{ID: "3-0", Kind: livelog.KindTruncated},
		{ID: "4-0", Kind: livelog.KindEnd, State: domain.TemplateRunLogComplete},
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
	want := sseLog("1-0", `{"offset":0,"data":"hello\n"}`) +
		sseLog("2-0", `{"offset":6,"data":"world\n"}`) +
		"event: log_truncated\ndata: {}\n\n" +
		sseEnd("complete")
	if got := response.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if !response.Flushed {
		t.Fatal("stream was never flushed")
	}
	if live.gotKey != livelog.StreamKey("run_123", "plan-1") {
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
	live := &fakeLiveLogs{entries: []livelog.Entry{{ID: "4-0", Kind: livelog.KindEnd, State: domain.TemplateRunLogComplete}}}

	streamLiveLog(t, deps, live, http.Header{"Last-Event-Id": {"3-0"}})

	if live.gotAfterID != "3-0" {
		t.Fatalf("afterID = %q, want 3-0", live.gotAfterID)
	}
}

func TestStreamTemplateRunLogReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []livelog.Entry{
		{ID: "1-0", Kind: livelog.KindPiece, Data: []byte{0xff, 'a'}},
		{ID: "2-0", Kind: livelog.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	if want := sseLog("1-0", `{"offset":0,"data":"�a"}`); !strings.HasPrefix(response.Body.String(), want) {
		t.Fatalf("body = %q, want it to start %q", response.Body.String(), want)
	}
}

func TestStreamTemplateRunLogSkipsADuplicateOffset(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)
	live := &fakeLiveLogs{entries: []livelog.Entry{
		piece("1-0", 0, "abc"),
		// A retried append: the same bytes again, under a new id.
		piece("2-0", 0, "abc"),
		piece("3-0", 3, "def"),
		{ID: "4-0", Kind: livelog.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"data":"abc"}`) +
		sseLog("3-0", `{"offset":3,"data":"def"}`) +
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
	live := &fakeLiveLogs{entries: []livelog.Entry{
		piece("1-0", 0, "abc"),
		piece("2-0", 0, "abcde"),
		{ID: "3-0", Kind: livelog.KindEnd, State: domain.TemplateRunLogComplete},
	}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"data":"abc"}`) +
		sseLog("2-0", `{"offset":3,"data":"de"}`) +
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
	live := &fakeLiveLogs{entries: []livelog.Entry{piece("1-0", 0, "abc")}}

	response := streamLiveLog(t, deps, live, nil)

	want := sseLog("1-0", `{"offset":0,"data":"abc"}`) + sseEnd("live")
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

func TestStreamTemplateRunLogAnswers503WhenTooManyLogsAreFollowed(t *testing.T) {
	t.Parallel()
	deps := newLiveLogDependencies(t)

	response := streamLiveLog(t, deps, &fakeLiveLogs{err: livelog.ErrTooManyTailers}, nil)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", response.Code, response.Body.String())
	}
	if problem := decodeProblem(t, response); problem.Type != problemTypeURI("live-logs-busy") {
		t.Fatalf("type = %q, want live-logs-busy", problem.Type)
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
