package workflows

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TestReplayRecordedHistories replays every recorded history against the
// workflows as they are now. Temporal replays a running workflow's history
// against whatever code the worker has, so a history that no longer replays is
// a run an upgrade would fail halfway with a non-determinism error.
//
// A failure here means a change altered the commands a workflow issues. If the
// history is under next/, nothing released ran it: re-record with `make
// workflow-histories`. If it is under a release, runs like it may be in flight
// when an install upgrades: branch the change on workflow.GetVersion, as
// CONTRIBUTING.md describes, until the old path replays again.
func TestReplayRecordedHistories(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "histories", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no recorded histories under testdata/histories")
	}

	replayer := worker.NewWorkflowReplayer()
	for _, registration := range Registered() {
		replayer.RegisterWorkflowWithOptions(registration.Workflow, workflow.RegisterOptions{Name: registration.Name})
	}
	// The replayer logs every replayed command; a failure's error says all
	// that matters.
	logger := log.NewStructuredLogger(slog.New(slog.DiscardHandler))

	inNext := map[string]bool{}
	for _, path := range paths {
		name, err := filepath.Rel(filepath.Join("testdata", "histories"), path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		history, err := client.HistoryFromJSON(file, client.HistoryJSONOptions{})
		_ = file.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		events := history.GetEvents()
		if len(events) == 0 || events[0].GetEventType() != enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED {
			t.Fatalf("%s: history does not start with WorkflowExecutionStarted", name)
		}
		workflowType := events[0].GetWorkflowExecutionStartedEventAttributes().GetWorkflowType().GetName()
		if filepath.Dir(name) == "next" {
			inNext[workflowType] = true
		}

		t.Run(name, func(t *testing.T) {
			if err := replayer.ReplayWorkflowHistory(logger, history); err != nil {
				t.Fatalf("replay %s history: %v", workflowType, err)
			}
		})
	}

	// Without this a new workflow would replay nothing and pass.
	for _, registration := range Registered() {
		if !inNext[registration.Name] {
			t.Errorf("no history of %s under testdata/histories/next: record one in TestRecordHistories", registration.Name)
		}
	}
}
