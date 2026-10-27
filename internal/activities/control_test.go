package activities

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
)

// planLogStream is the stream planLog's output goes to.
const planLogStream = "runlog:{run_123}:plan-3"

// planLog is a run's third plan log, complete unless a test says otherwise.
func planLog() domain.TemplateRunLog {
	return domain.TemplateRunLog{
		TenantID: "tenant_123", RunID: "run_123", RunPhase: domain.RunPhasePlan, Seq: 3, Command: domain.TerraformCommandPlan,
		State: domain.TemplateRunLogComplete, ContentType: "text/plain; charset=utf-8", SizeBytes: 6,
		UploadedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	}
}

// newLiveLogServer is a Valkey for a test, and a client to it.
func newLiveLogServer(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestRecordTemplateRunLogLiveWritesOnlyTheRow(t *testing.T) {
	t.Parallel()

	server, client := newLiveLogServer(t)
	recorder := &recordingLogMetadataRecorder{}
	log := planLog()
	log.State, log.ContentType, log.SizeBytes, log.UploadedAt = domain.TemplateRunLogLive, "", 0, time.Time{}

	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, client).RecordTemplateRunLog(context.Background(), log); err != nil {
		t.Fatalf("RecordTemplateRunLog returned error: %v", err)
	}
	if recorder.log.State != domain.TemplateRunLogLive || recorder.log.Name != "plan-3" {
		t.Fatalf("recorded %q in state %q, want plan-3 live", recorder.log.Name, recorder.log.State)
	}
	// The executor is still writing a live log's stream; ending it here would
	// tell viewers the command is over.
	if server.Exists(planLogStream) {
		t.Fatalf("stream %s exists, want no stream call for a live log", planLogStream)
	}
}

func TestRecordTemplateRunLogCompleteEndsTheStream(t *testing.T) {
	t.Parallel()

	assertRecordEndsTheStream(t, planLog(), domain.TemplateRunLogComplete)
}

func TestRecordTemplateRunLogLostEndsTheStreamWithLost(t *testing.T) {
	t.Parallel()

	log := planLog()
	log.State, log.LostReason, log.ContentType, log.SizeBytes, log.UploadedAt = domain.TemplateRunLogLost, domain.LogLostUploadFailed, "", 0, time.Time{}
	assertRecordEndsTheStream(t, log, domain.TemplateRunLogLost)
}

// assertRecordEndsTheStream records log and checks the row went to the store
// under its derived key, then that the stream holds one end entry with state,
// expiring in EndTTL.
func assertRecordEndsTheStream(t *testing.T, log domain.TemplateRunLog, state domain.TemplateRunLogState) {
	t.Helper()

	server, client := newLiveLogServer(t)
	recorder := &recordingLogMetadataRecorder{}
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, client).RecordTemplateRunLog(context.Background(), log); err != nil {
		t.Fatalf("RecordTemplateRunLog returned error: %v", err)
	}
	want := log
	want.Name, want.ObjectKey = "plan-3", "tenants/tenant_123/runs/run_123/logs/plan-3.log"
	if recorder.log != want {
		t.Fatalf("recorded log = %#v, want %#v", recorder.log, want)
	}

	messages, err := client.XRange(context.Background(), planLogStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("stream holds %d entries, want 1", len(messages))
	}
	entry, err := logsink.ParseEntry(messages[0])
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if entry.Kind != logsink.KindEnd || entry.State != state {
		t.Fatalf("entry = %+v, want an end entry with state %q", entry, state)
	}
	if ttl := server.TTL(planLogStream); ttl != logsink.EndTTL {
		t.Fatalf("TTL = %v, want %v", ttl, logsink.EndTTL)
	}
}

func TestRecordTemplateRunLogRejectsAnUnknownState(t *testing.T) {
	t.Parallel()

	log := planLog()
	log.State = "uploaded"
	assertRecordRejects(t, log)
}

func TestRecordTemplateRunLogRejectsLostWithoutAReason(t *testing.T) {
	t.Parallel()

	log := planLog()
	log.State, log.ContentType, log.SizeBytes, log.UploadedAt = domain.TemplateRunLogLost, "", 0, time.Time{}
	assertRecordRejects(t, log)
}

// A complete row says where the output is; without these the store would
// persist an empty content type and year 1 as a log anyone can open.
func TestRecordTemplateRunLogRejectsCompleteWithoutTheUpload(t *testing.T) {
	t.Parallel()

	unuploaded := planLog()
	unuploaded.UploadedAt = time.Time{}
	assertRecordRejects(t, unuploaded)

	untyped := planLog()
	untyped.ContentType = ""
	assertRecordRejects(t, untyped)
}

// assertRecordRejects records log and checks it failed before touching the
// store or the stream.
func assertRecordRejects(t *testing.T, log domain.TemplateRunLog) {
	t.Helper()

	server, client := newLiveLogServer(t)
	recorder := &recordingLogMetadataRecorder{}
	if err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, client).RecordTemplateRunLog(context.Background(), log); err == nil {
		t.Fatalf("RecordTemplateRunLog accepted %#v", log)
	}
	if recorder.log != (domain.TemplateRunLog{}) {
		t.Fatalf("recorded %#v, want nothing", recorder.log)
	}
	if server.Exists(planLogStream) {
		t.Fatalf("stream %s exists, want nothing written", planLogStream)
	}
}

// A viewer told a log is complete reads the row next; if the row was never
// written, that read finds nothing.
func TestRecordTemplateRunLogWritesTheRowBeforeTheEndEntry(t *testing.T) {
	t.Parallel()

	server, client := newLiveLogServer(t)
	recorder := &recordingLogMetadataRecorder{err: errors.New("database unavailable")}

	err := NewControlActivities(&controlStoreStub{logs: recorder}, nil, client).RecordTemplateRunLog(context.Background(), planLog())
	if !errors.Is(err, recorder.err) {
		t.Fatalf("error = %v, want the recorder's", err)
	}
	if server.Exists(planLogStream) {
		t.Fatalf("stream %s exists, want no end entry when the row was not written", planLogStream)
	}
}
