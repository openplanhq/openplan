package workflows

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vishu42/openplan/internal/domain"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
)

func TestTemplateSyncWorkflowRecordsCompletedSync(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	input := templateSyncWorkflowInput()
	var statuses []domain.TemplateSyncState
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			statuses = append(statuses, activityInput.Status)
			if activityInput.SyncID != input.SyncID {
				t.Fatalf("sync ID = %q, want %q", activityInput.SyncID, input.SyncID)
			}
			return nil
		})
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncActivityInput) (domain.TemplateSyncActivityOutput, error) {
			if activityInput.SourceRef != "v0.0.1" {
				t.Fatalf("source ref = %q, want v0.0.1", activityInput.SourceRef)
			}
			if activityInput.TemplateID != input.TemplateID {
				t.Fatalf("template ID = %q, want %q", activityInput.TemplateID, input.TemplateID)
			}
			return domain.TemplateSyncActivityOutput{
				Status:             domain.TemplateSyncStateCompleted,
				TemplateRevisionID: domain.TemplateRevisionID("template_123"),
				ResolvedCommitSHA:  "abc123",
			}, nil
		})

	env.ExecuteWorkflow(TemplateSyncWorkflow, input)

	assertWorkflowCompleted(t, env)
	want := []domain.TemplateSyncState{
		domain.TemplateSyncStateRunning,
		domain.TemplateSyncStateCompleted,
	}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
}

// TestTemplateSyncWorkflowRunsOnControlQueue keeps sync on the control plane:
// it writes revisions to the database and mints GitHub tokens.
func TestTemplateSyncWorkflowRunsOnControlQueue(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	var queues []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		queues = append(queues, info.TaskQueue)
	})
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateSyncActivityOutput{Status: domain.TemplateSyncStateCompleted}, nil)

	env.ExecuteWorkflow(TemplateSyncWorkflow, templateSyncWorkflowInput())

	assertWorkflowCompleted(t, env)
	if len(queues) != 4 {
		t.Fatalf("activity queues = %#v, want 4 activities", queues)
	}
	for _, queue := range queues {
		if queue != domain.ControlTaskQueue {
			t.Fatalf("activity queues = %#v, want all %q", queues, domain.ControlTaskQueue)
		}
	}
}

// The sync is a job with one step: the step is recorded for the sync
// once it is running, and before the work it names.
func TestTemplateSyncWorkflowRecordsItsStepBeforeSyncing(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	input := templateSyncWorkflowInput()
	var calls []string
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			calls = append(calls, "status:"+string(activityInput.Status))
			return nil
		})
	env.OnActivity(domain.RecordTemplateSyncStepActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStepActivityInput) error {
			if activityInput.SyncID != input.SyncID || activityInput.TenantID != input.TenantID {
				t.Fatalf("step recorded for %q/%q, want %q/%q", activityInput.TenantID, activityInput.SyncID, input.TenantID, input.SyncID)
			}
			calls = append(calls, "step:"+string(activityInput.Step))
			return nil
		})
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.TemplateSyncActivityInput) (domain.TemplateSyncActivityOutput, error) {
			calls = append(calls, "sync")
			return domain.TemplateSyncActivityOutput{Status: domain.TemplateSyncStateCompleted}, nil
		})

	env.ExecuteWorkflow(TemplateSyncWorkflow, input)

	assertWorkflowCompleted(t, env)
	want := []string{"status:running", "step:syncing", "sync", "status:completed"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

// A step that cannot be recorded fails the sync before its work starts, as a
// run's does.
func TestTemplateSyncWorkflowFailsWhenItsStepCannotBeRecorded(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	var statuses []domain.TemplateSyncState
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			statuses = append(statuses, activityInput.Status)
			return nil
		})
	env.OnActivity(domain.RecordTemplateSyncStepActivityName, mock.Anything, mock.Anything).
		Return(errors.New("database unavailable"))
	synced := false
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.TemplateSyncActivityInput) (domain.TemplateSyncActivityOutput, error) {
			synced = true
			return domain.TemplateSyncActivityOutput{}, nil
		})

	env.ExecuteWorkflow(TemplateSyncWorkflow, templateSyncWorkflowInput())

	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want the step error")
	}
	if synced {
		t.Fatal("sync ran although its step was never recorded")
	}
	want := []domain.TemplateSyncState{domain.TemplateSyncStateRunning, domain.TemplateSyncStateFailed}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
}

func TestTemplateSyncWorkflowRecordsInvalidSync(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	input := templateSyncWorkflowInput()
	var terminal domain.TemplateSyncStatusActivityInput
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			terminal = activityInput
			return nil
		})
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateSyncActivityOutput{
			Status:       domain.TemplateSyncStateInvalid,
			ErrorSummary: "resolve repository head: empty commit sha",
		}, nil)

	env.ExecuteWorkflow(TemplateSyncWorkflow, input)

	assertWorkflowCompleted(t, env)
	if terminal.Status != domain.TemplateSyncStateInvalid {
		t.Fatalf("terminal status = %q, want invalid", terminal.Status)
	}
	if terminal.ErrorSummary != "resolve repository head: empty commit sha" {
		t.Fatalf("error summary = %q", terminal.ErrorSummary)
	}
}

func TestTemplateSyncWorkflowRecordsFailedSync(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	input := templateSyncWorkflowInput()
	syncErr := errors.New("clone process failed")
	var statuses []domain.TemplateSyncState
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			statuses = append(statuses, activityInput.Status)
			return nil
		})
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateSyncActivityOutput{}, syncErr)

	env.ExecuteWorkflow(TemplateSyncWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want sync error")
	}
	want := []domain.TemplateSyncState{
		domain.TemplateSyncStateRunning,
		domain.TemplateSyncStateFailed,
	}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
}

func newTemplateSyncWorkflowTestEnvironment(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(TemplateSyncWorkflow)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateSyncStatusActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateSyncStatusActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateSyncStepActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateSyncStepActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateSyncActivityInput) (domain.TemplateSyncActivityOutput, error) {
			return domain.TemplateSyncActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.SyncTemplateActivityName},
	)
	return env
}

func templateSyncWorkflowInput() domain.TemplateSyncWorkflowInput {
	return domain.TemplateSyncWorkflowInput{
		SyncID:     domain.TemplateSyncID("template_sync_123"),
		TenantID:   domain.TenantID("tenant_123"),
		TemplateID: domain.TemplateID("source_template_123"),
		RepoOwner:  "acme",
		RepoName:   "infra-templates",
		SourceRef:  "v0.0.1",
		RootPath:   "modules/vpc",
	}
}

// A sync that fails, and whose failure then cannot be recorded, returns both
// errors: the sync's is the cause, and dropping it would leave nothing saying
// why the sync never finished.
func TestTemplateSyncWorkflowKeepsTheSyncErrorWhenItsFailureCannotBeRecorded(t *testing.T) {
	t.Parallel()

	env := newTemplateSyncWorkflowTestEnvironment(t)
	env.OnActivity(domain.RecordTemplateSyncStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateSyncStatusActivityInput) error {
			if activityInput.Status == domain.TemplateSyncStateFailed {
				return errors.New("database unavailable")
			}
			return nil
		})
	env.OnActivity(domain.SyncTemplateActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateSyncActivityOutput{}, errors.New("clone process failed"))

	env.ExecuteWorkflow(TemplateSyncWorkflow, templateSyncWorkflowInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("workflow error is nil, want the sync error")
	}
	for _, want := range []string{"clone process failed", "also failed to persist failure status", "database unavailable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("workflow error = %q, want it to mention %q", err, want)
		}
	}
}
