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

// replayHistoriesEnv names the directory of histories TestReplayRecordedHistories
// replays, and turns it on. scripts/workflow-replay.sh fills the directory from
// an older release and sets it.
const replayHistoriesEnv = "OPENPLAN_REPLAY_HISTORIES"

// TestReplayRecordedHistories replays every history in the directory
// replayHistoriesEnv names against the workflows as they are now. Temporal
// replays a running workflow's history against whatever code the worker has,
// so a history that no longer replays is a run an upgrade would fail halfway
// with a non-determinism error.
//
// A failure means a change altered the commands a workflow issues. Branch the
// change on workflow.GetVersion, as CONTRIBUTING.md describes, until the old
// path replays again.
func TestReplayRecordedHistories(t *testing.T) {
	dir := os.Getenv(replayHistoriesEnv)
	if dir == "" {
		t.Skipf("set %s to a directory of recorded histories; scripts/workflow-replay.sh records and replays them", replayHistoriesEnv)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no recorded histories in %s", dir)
	}

	replayer := worker.NewWorkflowReplayer()
	for _, registration := range Registered() {
		replayer.RegisterWorkflowWithOptions(registration.Workflow, workflow.RegisterOptions{Name: registration.Name})
	}
	// The replayer logs every replayed command; a failure's error says all
	// that matters.
	logger := log.NewStructuredLogger(slog.New(slog.DiscardHandler))

	for _, path := range paths {
		name := filepath.Base(path)
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

		t.Run(name, func(t *testing.T) {
			if err := replayer.ReplayWorkflowHistory(logger, history); err != nil {
				t.Fatalf("replay %s history: %v", workflowType, err)
			}
		})
	}
}
