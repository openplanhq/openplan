package workflows

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TestTemplateWorkflowsUseSessionForWorkspaceActivities protects the executor
// affinity contract for the filesystem-backed workspace. If a future change
// schedules any workspace activity on the normal queue, it could run on a
// different executor that does not have the run's local files. Each phase
// holds one session: the plan phase's, and the apply workflow's own.
func TestTemplateWorkflowsUseSessionForWorkspaceActivities(t *testing.T) {
	for _, testCase := range []struct {
		name                   string
		workflow               any
		operation              domain.OperationType
		workspaceActivityCount int
	}{
		{name: "plan", workflow: TemplatePlanWorkflow, operation: domain.OperationPlan, workspaceActivityCount: 5},
		{name: "destroy plan", workflow: TemplatePlanWorkflow, operation: domain.OperationDestroy, workspaceActivityCount: 5},
		{name: "apply", workflow: TemplateApplyWorkflow, operation: domain.OperationApply, workspaceActivityCount: 5},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := newTemplateRunWorkflowTestEnvironment(t)
			input := templateRunWorkflowInput(testCase.operation)
			var workspaceTaskQueues []string
			var statusTaskQueues []string

			env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
				Return(func(ctx context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
					workspaceTaskQueues = append(workspaceTaskQueues, activity.GetInfo(ctx).TaskQueue)
					return domain.PrepareWorkspaceActivityOutput{WorkspacePath: "run/workspace"}, nil
				})
			env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
				Return(func(ctx context.Context, _ domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
					workspaceTaskQueues = append(workspaceTaskQueues, activity.GetInfo(ctx).TaskQueue)
					return domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil
				})
			env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
				Return(func(ctx context.Context, _ domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
					workspaceTaskQueues = append(workspaceTaskQueues, activity.GetInfo(ctx).TaskQueue)
					return domain.RunTerraformActivityOutput{}, nil
				})
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
				Return(func(ctx context.Context, activityInput domain.TemplateRunStatusActivityInput) error {
					statusTaskQueues = append(statusTaskQueues, activity.GetInfo(ctx).TaskQueue)
					_ = activityInput
					return nil
				})

			env.ExecuteWorkflow(testCase.workflow, input)

			assertWorkflowCompleted(t, env)
			if len(workspaceTaskQueues) != testCase.workspaceActivityCount {
				t.Fatalf("workspace activity task queues = %#v, want %d activity queues", workspaceTaskQueues, testCase.workspaceActivityCount)
			}
			for _, taskQueue := range workspaceTaskQueues[1:] {
				if taskQueue != workspaceTaskQueues[0] {
					t.Fatalf("workspace activity task queues = %#v, want one shared session queue", workspaceTaskQueues)
				}
			}
			if len(statusTaskQueues) == 0 {
				t.Fatal("status activity task queue is empty")
			}
			for _, taskQueue := range statusTaskQueues {
				if taskQueue != statusTaskQueues[0] {
					t.Fatalf("status activity task queues = %#v, want one normal queue", statusTaskQueues)
				}
			}
			if workspaceTaskQueues[0] == statusTaskQueues[0] {
				t.Fatalf("workspace task queue = %q, want a session queue distinct from status queue", workspaceTaskQueues[0])
			}
		})
	}
}

// TestTemplatePlanWorkflowRoutesActivitiesByPlane protects the control/data
// plane split. Status writes need the database, so they must stay on the
// control queue; the session must be created on the execution queue, or
// Terraform would run on a host that holds the control plane's secrets.
func TestTemplatePlanWorkflowRoutesActivitiesByPlane(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	queues := map[string][]string{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		queues[info.ActivityType.Name] = append(queues[info.ActivityType.Name], info.TaskQueue)
	})
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "run/workspace"}, nil)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).Return(domain.RunTerraformActivityOutput{}, nil)
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	statusQueues := queues[domain.RecordTemplateRunStatusActivityName]
	if len(statusQueues) == 0 {
		t.Fatal("no status activity ran")
	}
	for _, queue := range statusQueues {
		if queue != domain.ControlTaskQueue {
			t.Fatalf("status activity queues = %#v, want all %q", statusQueues, domain.ControlTaskQueue)
		}
	}
	for _, name := range []string{domain.RecordTemplateRunStepActivityName} {
		for _, queue := range queues[name] {
			if queue != domain.ControlTaskQueue {
				t.Fatalf("%s queues = %#v, want all %q", name, queues[name], domain.ControlTaskQueue)
			}
		}
		if len(queues[name]) == 0 {
			t.Fatalf("no %s ran", name)
		}
	}
	// The SDK creates a session through an internal activity on
	// "<base queue>__internal_session_creation"; the base is what places it.
	wantCreation := domain.ExecutionTaskQueue + "__internal_session_creation"
	if got := queues["internalSessionCreationActivity"]; len(got) != 1 || got[0] != wantCreation {
		t.Fatalf("session creation queues = %#v, want [%q]", got, wantCreation)
	}
}

// A plan run, as driven above, records no event: only a destroy does. A
// destroy plan with nothing left to destroy records the destroyed event,
// which must stay on the control queue like the run's other writes.
func TestTemplatePlanWorkflowRoutesTheEventActivityByPlane(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var eventQueues []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		if info.ActivityType.Name == domain.RecordTemplateRunEventActivityName {
			eventQueues = append(eventQueues, info.TaskQueue)
		}
	})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationDestroy))

	assertWorkflowCompleted(t, env)
	if len(eventQueues) == 0 {
		t.Fatal("no event activity ran")
	}
	for _, queue := range eventQueues {
		if queue != domain.ControlTaskQueue {
			t.Fatalf("event activity queues = %#v, want all %q", eventQueues, domain.ControlTaskQueue)
		}
	}
}

// The executor has no database, so a log's rows are recorded by the control
// plane: live before its command starts, so the run page finds it when the
// step changes, and complete after the upload, before the run completes.
func TestTemplatePlanWorkflowRecordsCommandLogsOnControlQueue(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var events []string
	var logQueues []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		if info.ActivityType.Name == domain.RecordTemplateRunLogActivityName {
			logQueues = append(logQueues, info.TaskQueue)
		}
	})
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			events = append(events, "terraform:"+string(input.Command))
			return domain.RunTerraformActivityOutput{}, nil
		})
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			events = append(events, "log:"+log.Name+":"+string(log.State))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			if input.Status == domain.TemplateRunCompleted {
				events = append(events, "status:"+string(input.Status))
			}
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	want := []string{
		"log:plan-1:live", "terraform:init", "log:plan-1:complete",
		"log:plan-2:live", "terraform:select_workspace", "log:plan-2:complete",
		"log:plan-3:live", "terraform:plan", "log:plan-3:complete",
		"status:completed",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	for _, queue := range logQueues {
		if queue != domain.ControlTaskQueue {
			t.Fatalf("log activity queues = %#v, want all %q", logQueues, domain.ControlTaskQueue)
		}
	}
}

// A failed command's log explains the failure, so it is uploaded and recorded
// before the run is marked failed.
func TestTemplatePlanWorkflowRecordsLogOfFailedCommand(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var recorded []domain.TemplateRunLog
	var failedStatus domain.TemplateRunStatusActivityInput
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command != domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, nil
			}
			return domain.RunTerraformActivityOutput{}, errors.New("plan: exit status 1")
		})
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log)
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			if input.Status == domain.TemplateRunFailed {
				failedStatus = input
			}
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v, want a failed run", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(recorded) == 0 {
		t.Fatal("no log recorded")
	}
	if last := recorded[len(recorded)-1]; last.Command != domain.TerraformCommandPlan || last.State != domain.TemplateRunLogComplete {
		t.Fatalf("recorded logs = %#v, want the failed plan's, complete, last", recorded)
	}
	if !strings.Contains(failedStatus.ErrorSummary, "plan: exit status 1") {
		t.Fatalf("failure summary = %q, want the command error", failedStatus.ErrorSummary)
	}
	// Review focus 4: an error that carries no Terraform message records none.
	if failedStatus.CommandError != "" {
		t.Fatalf("command error = %q, want none", failedStatus.CommandError)
	}
}

// Issue 522: the run keeps what Terraform said apart from the workflow's
// wrapper around it.
func TestTemplatePlanWorkflowRecordsTheCommandError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var failedStatus domain.TemplateRunStatusActivityInput
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command != domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, nil
			}
			return domain.RunTerraformActivityOutput{}, temporal.NewApplicationError("run terraform: plan: exit status 1",
				domain.TerraformCommandFailedErrorType, domain.TerraformCommandFailure{Message: "Error: bad creds"})
		})
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			if input.Status == domain.TemplateRunFailed {
				failedStatus = input
			}
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v, want a failed run", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if failedStatus.CommandError != "Error: bad creds" {
		t.Fatalf("command error = %q, want Terraform's", failedStatus.CommandError)
	}
	if !strings.Contains(failedStatus.ErrorSummary, "plan: exit status 1") {
		t.Fatalf("failure summary = %q, want the wrapper kept", failedStatus.ErrorSummary)
	}
}

// A log that cannot be uploaded is lost, not a reason to fail the run: the
// command it describes succeeded, and the row says why its output is gone.
func TestTemplatePlanWorkflowRecordsALostLogWhenTheUploadFails(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var recorded []string
	var statuses []domain.TemplateRunStatus
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(domain.RunTerraformActivityOutput{}, nil)
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateRunLog{}, errors.New("store unavailable"))
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log.Name+":"+string(log.State)+":"+string(log.LostReason))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			statuses = append(statuses, input.Status)
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	want := []string{
		"plan-1:live:", "plan-1:lost:upload_failed",
		"plan-2:live:", "plan-2:lost:upload_failed",
		"plan-3:live:", "plan-3:lost:upload_failed",
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded logs = %#v, want %#v", recorded, want)
	}
	if want := []domain.TemplateRunStatus{domain.TemplateRunRunning, domain.TemplateRunCompleted}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
}

// An upload whose result does not describe its object (no content type, no
// upload time) cannot make a complete row, which the control plane would
// refuse and leave live for good, so it is recorded as lost.
func TestTemplatePlanWorkflowRecordsLostWhenTheUploadResultIsIncomplete(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var recorded []string
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(domain.RunTerraformActivityOutput{}, nil)
	uploads := 0
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
			uploads++
			switch uploads {
			case 1:
				return domain.TemplateRunLog{SizeBytes: 3, UploadedAt: time.Unix(1, 0)}, nil
			case 2:
				return domain.TemplateRunLog{SizeBytes: 3, ContentType: "text/plain; charset=utf-8"}, nil
			}
			return domain.TemplateRunLog{SizeBytes: 3, ContentType: "text/plain; charset=utf-8", UploadedAt: time.Unix(1, 0)}, nil
		})
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log.Name+":"+string(log.State)+":"+string(log.LostReason))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	want := []string{
		"plan-1:live:", "plan-1:lost:upload_failed",
		"plan-2:live:", "plan-2:lost:upload_failed",
		"plan-3:live:", "plan-3:complete:",
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("recorded logs = %#v, want %#v", recorded, want)
	}
}

// A command whose heartbeat stopped had no executor left to upload its log, so
// the log is lost to the executor, not to the store. The run still fails with
// the command's own error: the log's fate never replaces it.
func TestTemplatePlanWorkflowRecordsExecutorGoneWhenTheCommandHeartbeatTimesOut(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var recorded []domain.TemplateRunLog
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command != domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, nil
			}
			return domain.RunTerraformActivityOutput{}, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_HEARTBEAT, nil)
		})
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
			if input.LogName != "plan-3" {
				return uploadedLog(input), nil
			}
			return domain.TemplateRunLog{}, errors.New("session failed")
		})
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log)
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var timeoutErr *temporal.TimeoutError
	if err := env.GetWorkflowError(); !errors.As(err, &timeoutErr) || timeoutErr.TimeoutType() != enumspb.TIMEOUT_TYPE_HEARTBEAT {
		t.Fatalf("workflow error = %v, want the command's heartbeat timeout", err)
	}
	if len(recorded) == 0 {
		t.Fatal("no log recorded")
	}
	last := recorded[len(recorded)-1]
	if last.Name != "plan-3" || last.State != domain.TemplateRunLogLost || last.LostReason != domain.LogLostExecutorGone {
		t.Fatalf("last recorded log = %s %s/%s, want plan-3 lost/%s", last.Name, last.State, last.LostReason, domain.LogLostExecutorGone)
	}
}

// When an executor dies mid-command, its session's own heartbeat (20 s) times
// out long before the command's (120 s): the session fails and cancels the
// command, so the command's error is a cancellation, not a heartbeat timeout.
// The log is still lost to the executor, not to the store. The session here
// fails while the plan is running, with the error the SDK's creation activity
// returns when its worker goes away. (A heartbeat timeout returned from a mock
// is retried by the test environment, unlike the server's.)
func TestTemplatePlanWorkflowRecordsExecutorGoneWhenTheSessionFails(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	planStarted := make(chan struct{})
	// The plan never finishes on its own: a cancelled activity's goroutine is
	// not stopped by the test environment, so it is released at cleanup.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	env.OnActivity(sessionCreationActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, sessionID string) error {
			// What the SDK's own creation activity signals once the session
			// is placed on a host.
			env.SignalWorkflow(sessionID, struct{ Taskqueue, HostName, ResourceID string }{
				Taskqueue: "executor-host-1", HostName: "executor-host-1", ResourceID: "executor-host-1",
			})
			<-planStarted
			return temporal.NewNonRetryableApplicationError("session failed due to worker shutdown", "SessionWorkerShutdown", nil)
		})
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command == domain.TerraformCommandPlan {
				close(planStarted)
				<-release
			}
			return domain.RunTerraformActivityOutput{}, nil
		})
	var recorded []domain.TemplateRunLog
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log)
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var canceledErr *temporal.CanceledError
	if err := env.GetWorkflowError(); !errors.As(err, &canceledErr) {
		t.Fatalf("workflow error = %v, want the command cancelled with its session", err)
	}
	if len(recorded) == 0 {
		t.Fatal("no log recorded")
	}
	last := recorded[len(recorded)-1]
	if last.Name != "plan-3" || last.State != domain.TemplateRunLogLost || last.LostReason != domain.LogLostExecutorGone {
		t.Fatalf("last recorded log = %s %s/%s, want plan-3 lost/%s", last.Name, last.State, last.LostReason, domain.LogLostExecutorGone)
	}
}

// A log row that cannot be written is a gap in the run's history, not a
// failure of the command it describes: the run completes, after the record
// has had its own retries.
func TestTemplatePlanWorkflowKeepsGoingWhenRecordingALogFails(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	completeAttempts := map[string]int{}
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(domain.RunTerraformActivityOutput{}, nil)
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			if log.State != domain.TemplateRunLogComplete {
				return nil
			}
			completeAttempts[log.Name]++
			return errors.New("database unavailable")
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	attempts := int(recordLogRetryPolicy.MaximumAttempts)
	if want := map[string]int{"plan-1": attempts, "plan-2": attempts, "plan-3": attempts}; !reflect.DeepEqual(completeAttempts, want) {
		t.Fatalf("complete record attempts = %#v, want %#v (recordLogRetryPolicy.MaximumAttempts each)", completeAttempts, want)
	}
}

// UploadLog retries under its own policy rather than the run's default of a
// single attempt: an upload is idempotent, and losing a log to one blip in
// the object store is worse than a few seconds' wait.
func TestUploadLogRetriesWithItsOwnPolicy(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	starts := map[string]int{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, args converter.EncodedValues) {
		if info.ActivityType.Name != domain.UploadLogActivityName {
			return
		}
		var input domain.UploadLogActivityInput
		if err := args.Get(&input); err != nil {
			t.Errorf("decode UploadLog input: %v", err)
		}
		starts[input.LogName]++
	})
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(domain.RunTerraformActivityOutput{}, nil)
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(domain.TemplateRunLog{}, errors.New("store unavailable"))
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	attempts := int(uploadLogRetryPolicy.MaximumAttempts)
	if want := map[string]int{"plan-1": attempts, "plan-2": attempts, "plan-3": attempts}; !reflect.DeepEqual(starts, want) {
		t.Fatalf("UploadLog starts = %#v, want %#v (uploadLogRetryPolicy.MaximumAttempts each)", starts, want)
	}
}

// Secrets cross Temporal only sealed to the key the executor returned from
// PrepareWorkspace: the control plane seals, on the control queue, and the
// executor receives only ciphertext. The key is released when the run ends.
func TestTemplatePlanWorkflowSealsSecretsToTheExecutorKey(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	publicKey := []byte("executor-public-key-0123456789ab")
	queues := map[string][]string{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		queues[info.ActivityType.Name] = append(queues[info.ActivityType.Name], info.TaskQueue)
	})
	var sealRequests int
	var terraformEnvironments [][]byte
	var fetchInput domain.FetchSourceActivityInput
	var released domain.ReleaseRunKeyActivityInput
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "run/workspace", PublicKey: publicKey}, nil)
	env.OnActivity(domain.SealSourceTokenActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.SealSourceTokenActivityInput) (domain.SealSourceTokenActivityOutput, error) {
			if !bytes.Equal(input.PublicKey, publicKey) || input.RepoOwner != "acme" {
				t.Fatalf("seal source token input = %#v", input)
			}
			return domain.SealSourceTokenActivityOutput{SealedToken: []byte("sealed-token"), FetchHint: "; hint"}, nil
		})
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
			fetchInput = input
			return domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil
		})
	env.OnActivity(domain.SealRunCredentialsActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.SealRunCredentialsActivityInput) (domain.SealRunCredentialsActivityOutput, error) {
			if !bytes.Equal(input.PublicKey, publicKey) || input.StackTemplateID != "stack_template_123" {
				t.Fatalf("seal credentials input = %#v", input)
			}
			sealRequests++
			return domain.SealRunCredentialsActivityOutput{SealedEnvironment: []byte(fmt.Sprintf("sealed-env-%d", sealRequests))}, nil
		})
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			terraformEnvironments = append(terraformEnvironments, input.SealedEnvironment)
			return domain.RunTerraformActivityOutput{}, nil
		})
	env.OnActivity(domain.ReleaseRunKeyActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.ReleaseRunKeyActivityInput) error {
			released = input
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	if string(fetchInput.SealedToken) != "sealed-token" || fetchInput.FetchHint != "; hint" {
		t.Fatalf("fetch source input = %#v, want the sealed token and hint", fetchInput)
	}
	// One fresh seal per command: init, select_workspace, plan.
	want := [][]byte{[]byte("sealed-env-1"), []byte("sealed-env-2"), []byte("sealed-env-3")}
	if !reflect.DeepEqual(terraformEnvironments, want) {
		t.Fatalf("terraform sealed environments = %q, want %q", terraformEnvironments, want)
	}
	for _, name := range []string{domain.SealSourceTokenActivityName, domain.SealRunCredentialsActivityName} {
		for _, queue := range queues[name] {
			if queue != domain.ControlTaskQueue {
				t.Fatalf("%s queues = %#v, want all %q", name, queues[name], domain.ControlTaskQueue)
			}
		}
	}
	if released.RunID != "run_123" || released.TenantID != "tenant_123" {
		t.Fatalf("released key = %#v, want run_123's", released)
	}
	if got, prepare := queues[domain.ReleaseRunKeyActivityName], queues[domain.PrepareWorkspaceActivityName]; len(got) != 1 || got[0] != prepare[0] {
		t.Fatalf("release key queues = %#v, want the session queue %#v", got, prepare)
	}
}

// Opened credentials live on RunTerraformActivityInput inside the executor. The
// field must never serialize, or a workflow that set it would write plaintext
// into history.
func TestRunTerraformActivityInputNeverSerializesEnvironment(t *testing.T) {
	t.Parallel()

	payload, err := converter.GetDefaultDataConverter().ToPayload(domain.RunTerraformActivityInput{
		RunID:       "run_123",
		Environment: map[string]string{"AWS_SECRET_ACCESS_KEY": "canary-7f3a"},
	})
	if err != nil {
		t.Fatalf("ToPayload returned error: %v", err)
	}
	if strings.Contains(string(payload.GetData()), "canary-7f3a") {
		t.Fatalf("payload = %s, want no plaintext environment", payload.GetData())
	}
}

// TestTemplatePlanWorkflowGivesFetchSourceALongerTimeout guards against the
// default one-minute activity budget silently swallowing FetchSource again.
// That activity now resolves a GitHub token before it ever invokes git, which
// can cost up to two HTTP round trips on top of the clone/checkout itself, so
// it needs -- and must keep -- a longer StartToCloseTimeout than the rest of
// the run's activities.
func TestTemplatePlanWorkflowGivesFetchSourceALongerTimeout(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)
	var fetchSourceTimeout time.Duration

	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "run/workspace"}, nil)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(func(ctx context.Context, _ domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
			fetchSourceTimeout = activity.GetInfo(ctx).StartToCloseTimeout
			return domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil
		})
	var commands []domain.TerraformCommandType
	mockRunTerraform(t, env, &commands)
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	assertWorkflowCompleted(t, env)
	if fetchSourceTimeout != 3*time.Minute {
		t.Fatalf("FetchSource StartToCloseTimeout = %v, want 3m", fetchSourceTimeout)
	}
}

// With one session per executor, every session busy is the normal case, not a
// fault, so a plan waits for a free executor as long as an apply does instead
// of failing a teammate's run after a minute.
func TestTemplateWorkflowsWaitTenMinutesForAnExecutor(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		workflow any
		input    domain.TemplateRunWorkflowInput
	}{
		{name: "plan", workflow: TemplatePlanWorkflow, input: templateRunWorkflowInput(domain.OperationPlan)},
		{name: "apply", workflow: TemplateApplyWorkflow, input: templateRunWorkflowInput(domain.OperationApply)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			var creationTimeouts []time.Duration
			env.SetWorkerOptions(worker.Options{
				EnableSessionWorker: true,
				Interceptors:        []interceptor.WorkerInterceptor{&sessionCreationRecorder{timeouts: &creationTimeouts}},
			})
			mockPrepareWorkspace(t, env)
			mockFetchSource(t, env)
			var commands []domain.TerraformCommandType
			mockRunTerraform(t, env, &commands)
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

			env.ExecuteWorkflow(testCase.workflow, testCase.input)

			assertWorkflowCompleted(t, env)
			if want := []time.Duration{10 * time.Minute}; !reflect.DeepEqual(creationTimeouts, want) {
				t.Fatalf("session creation timeouts = %v, want %v", creationTimeouts, want)
			}
		})
	}
}

// sessionCreationRecorder records the timeout each session is given to find an
// executor. The SDK passes it as the ScheduleToStartTimeout of its creation
// activity, which activity.Info does not carry, so it is read from the
// options the workflow schedules that activity with.
type sessionCreationRecorder struct {
	interceptor.WorkerInterceptorBase
	timeouts *[]time.Duration
}

func (r *sessionCreationRecorder) InterceptWorkflow(_ workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &sessionCreationInbound{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}, timeouts: r.timeouts}
}

type sessionCreationInbound struct {
	interceptor.WorkflowInboundInterceptorBase
	timeouts *[]time.Duration
}

func (i *sessionCreationInbound) Init(outbound interceptor.WorkflowOutboundInterceptor) error {
	return i.Next.Init(&sessionCreationOutbound{WorkflowOutboundInterceptorBase: interceptor.WorkflowOutboundInterceptorBase{Next: outbound}, timeouts: i.timeouts})
}

type sessionCreationOutbound struct {
	interceptor.WorkflowOutboundInterceptorBase
	timeouts *[]time.Duration
}

func (o *sessionCreationOutbound) ExecuteActivity(ctx workflow.Context, activityType string, args ...any) workflow.Future {
	if activityType == sessionCreationActivityName {
		*o.timeouts = append(*o.timeouts, workflow.GetActivityOptions(ctx).ScheduleToStartTimeout)
	}
	return o.Next.ExecuteActivity(ctx, activityType, args...)
}

// TestTemplatePlanWorkflowBoundsTerraformCommandsByTheConfiguredTimeout pins the
// two halves of how a long Terraform command survives: the budget comes from
// the run's input, so a deployment can raise it, and a heartbeat timeout is
// always set, so a lost executor fails within a couple of heartbeat intervals
// instead of consuming the whole budget. A hard-coded StartToCloseTimeout here is what killed runs
// longer than ten minutes.
func TestTemplatePlanWorkflowBoundsTerraformCommandsByTheConfiguredTimeout(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		configured  time.Duration
		wantTimeout time.Duration
	}{
		{name: "configured", configured: 90 * time.Minute, wantTimeout: 90 * time.Minute},
		{name: "unset", configured: 0, wantTimeout: domain.DefaultTerraformTimeout},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			env := newTemplateRunWorkflowTestEnvironment(t)
			input := templateRunWorkflowInput(domain.OperationPlan)
			input.TerraformTimeout = testCase.configured
			var timeouts []time.Duration
			var heartbeatTimeouts []time.Duration

			env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
				Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "run/workspace"}, nil)
			env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
				Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)
			env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
				Return(func(ctx context.Context, _ domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
					timeouts = append(timeouts, activity.GetInfo(ctx).StartToCloseTimeout)
					heartbeatTimeouts = append(heartbeatTimeouts, activity.GetInfo(ctx).HeartbeatTimeout)
					return domain.RunTerraformActivityOutput{}, nil
				})
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

			env.ExecuteWorkflow(TemplatePlanWorkflow, input)

			assertWorkflowCompleted(t, env)
			if len(timeouts) == 0 {
				t.Fatal("no Terraform command ran")
			}
			for _, timeout := range timeouts {
				if timeout != testCase.wantTimeout {
					t.Fatalf("RunTerraform StartToCloseTimeout = %v, want %v", timeout, testCase.wantTimeout)
				}
			}
			for _, timeout := range heartbeatTimeouts {
				if timeout != domain.TerraformHeartbeatTimeout {
					t.Fatalf("RunTerraform HeartbeatTimeout = %v, want %v", timeout, domain.TerraformHeartbeatTimeout)
				}
			}
		})
	}
}

func TestTemplatePlanWorkflowRecordsPlanSteps(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)
	var events []string
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			if activityInput.RunID != input.RunID {
				t.Fatalf("prepare workspace RunID = %q, want %q", activityInput.RunID, input.RunID)
			}
			if activityInput.TenantID != input.TenantID {
				t.Fatalf("prepare workspace TenantID = %q, want %q", activityInput.TenantID, input.TenantID)
			}
			events = append(events, "prepare_workspace")
			return domain.PrepareWorkspaceActivityOutput{WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123"}, nil
		})
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
			if activityInput.RunID != input.RunID {
				t.Fatalf("fetch source RunID = %q, want %q", activityInput.RunID, input.RunID)
			}
			if activityInput.TenantID != input.TenantID {
				t.Fatalf("fetch source TenantID = %q, want %q", activityInput.TenantID, input.TenantID)
			}
			if activityInput.WorkspacePath != "/tmp/openplan/runs/tenant_123/run_123" {
				t.Fatalf("fetch source WorkspacePath = %q", activityInput.WorkspacePath)
			}
			if activityInput.RepoOwner != input.RepoOwner {
				t.Fatalf("fetch source RepoOwner = %q, want %q", activityInput.RepoOwner, input.RepoOwner)
			}
			if activityInput.RepoName != input.RepoName {
				t.Fatalf("fetch source RepoName = %q, want %q", activityInput.RepoName, input.RepoName)
			}
			if activityInput.SourceRef != input.SourceRef {
				t.Fatalf("fetch source SourceRef = %q, want %q", activityInput.SourceRef, input.SourceRef)
			}
			if activityInput.RootPath != input.RootPath {
				t.Fatalf("fetch source RootPath = %q, want %q", activityInput.RootPath, input.RootPath)
			}
			events = append(events, "fetch_source")
			return domain.FetchSourceActivityOutput{TerraformPath: "/tmp/openplan/runs/tenant_123/run_123/source/modules/vpc"}, nil
		})
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if activityInput.RunID != input.RunID {
				t.Fatalf("run terraform RunID = %q, want %q", activityInput.RunID, input.RunID)
			}
			if activityInput.TenantID != input.TenantID {
				t.Fatalf("run terraform TenantID = %q, want %q", activityInput.TenantID, input.TenantID)
			}
			if activityInput.WorkspacePath != "/tmp/openplan/runs/tenant_123/run_123" {
				t.Fatalf("run terraform WorkspacePath = %q", activityInput.WorkspacePath)
			}
			if activityInput.TerraformPath != "/tmp/openplan/runs/tenant_123/run_123/source/modules/vpc" {
				t.Fatalf("run terraform TerraformPath = %q", activityInput.TerraformPath)
			}
			if activityInput.WorkspaceName != input.WorkspaceName {
				t.Fatalf("run terraform WorkspaceName = %q, want %q", activityInput.WorkspaceName, input.WorkspaceName)
			}
			if string(activityInput.ConfigJSON) != string(input.ConfigJSON) {
				t.Fatalf("run terraform ConfigJSON = %s, want %s", activityInput.ConfigJSON, input.ConfigJSON)
			}
			events = append(events, "terraform:"+string(activityInput.Command))
			return domain.RunTerraformActivityOutput{}, nil
		})
	mockRunWrites(env, &events)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	assertWorkflowCompleted(t, env)
	want := []string{
		"status:running",
		"step:waiting_for_executor",
		"step:preparing_workspace",
		"prepare_workspace",
		"step:fetching_source",
		"fetch_source",
		"step:initializing",
		"terraform:" + string(domain.TerraformCommandInit),
		"step:selecting_workspace",
		"terraform:" + string(domain.TerraformCommandSelectWorkspace),
		"step:planning",
		"terraform:" + string(domain.TerraformCommandPlan),
		"status:completed",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

// An apply run's plan with changes is saved and then waits for approval as a
// row, not as a workflow: the workflow ends, releasing its executor, and
// nothing is applied.
func TestTemplatePlanWorkflowSavesAPlanWithChangesAndEnds(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var writes []string
	var events []string
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			events = append(events, "terraform:"+string(activityInput.Command))
			if activityInput.Command == domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{HasChanges: true, Summary: domain.PlanSummary{Add: 2}}, nil
			}
			return domain.RunTerraformActivityOutput{}, nil
		})
	env.OnActivity(domain.SealPlanKeyActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.SealPlanKeyActivityInput) (domain.SealPlanKeyActivityOutput, error) {
			if !input.Create {
				t.Fatal("plan phase read a plan key instead of creating one")
			}
			events = append(events, "seal_plan_key")
			return domain.SealPlanKeyActivityOutput{SealedPlanKey: []byte("sealed")}, nil
		})
	env.OnActivity(domain.UploadPlanActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.PlanArtifactActivityInput) error {
			if input.TerraformPath != "/tmp/openplan/runs/tenant_123/run_123/source/modules/vpc" || string(input.SealedPlanKey) != "sealed" {
				t.Fatalf("upload input = %#v", input)
			}
			events = append(events, "upload_plan")
			return nil
		})
	env.OnActivity(domain.FinishPlanActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.FinishPlanActivityInput) (domain.PlanOutcome, error) {
			if !input.HasChanges || input.Summary != (domain.PlanSummary{Add: 2}) {
				t.Fatalf("finish plan input = %#v", input)
			}
			events = append(events, "finish_plan")
			return domain.PlanOutcomeWaiting, nil
		})
	env.OnActivity(domain.CleanupWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.CleanupWorkspaceActivityInput) error {
			if input.DeletePlan {
				t.Fatal("plan phase deleted the plan it just saved")
			}
			events = append(events, "cleanup")
			return nil
		})
	mockRunWrites(env, &writes)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationApply))

	assertWorkflowCompleted(t, env)
	wantWrites := append([]string{"status:running"}, setupWrites(false)...)
	wantWrites = append(wantWrites, "step:planning", "step:saving_plan")
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("writes = %#v, want %#v", writes, wantWrites)
	}
	// The plan is saved inside the session; the session is torn down before
	// the plan is finished, so no executor is held while it waits.
	wantEvents := []string{
		"terraform:init", "terraform:select_workspace", "terraform:plan",
		"seal_plan_key", "upload_plan", "cleanup", "finish_plan",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
}

// The apply workflow puts the saved plan back before init, applies it, and
// deletes it along with the workspace. It records its setup steps again,
// because they are what it is doing, and runs them in the apply phase so
// their logs do not replace the plan phase's.
func TestTemplateApplyWorkflowAppliesTheSavedPlan(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		operation   domain.OperationType
		command     domain.TerraformCommandType
		applyWrites []string
	}{
		{name: "apply run", operation: domain.OperationApply, command: domain.TerraformCommandApply, applyWrites: []string{"step:applying", "event:applied"}},
		{name: "destroy run", operation: domain.OperationDestroy, command: domain.TerraformCommandDestroy, applyWrites: []string{"event:destroying", "step:applying", "event:destroyed"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			var writes []string
			var events []string
			terraformRuns := 0
			mockPrepareWorkspace(t, env)
			mockFetchSource(t, env)
			env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
					events = append(events, "terraform:"+string(activityInput.Command))
					terraformRuns++
					// The apply workflow numbers its own logs from 1.
					if want := fmt.Sprintf("apply-%d", terraformRuns); activityInput.LogName != want {
						t.Errorf("%s logged to %q, want %q", activityInput.Command, activityInput.LogName, want)
					}
					return domain.RunTerraformActivityOutput{}, nil
				})
			env.OnActivity(domain.SealPlanKeyActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.SealPlanKeyActivityInput) (domain.SealPlanKeyActivityOutput, error) {
					if input.Create {
						t.Fatal("apply phase created a new plan key instead of reading the plan's")
					}
					return domain.SealPlanKeyActivityOutput{SealedPlanKey: []byte("sealed")}, nil
				})
			env.OnActivity(domain.DownloadPlanActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, _ domain.PlanArtifactActivityInput) error {
					events = append(events, "download_plan")
					return nil
				})
			env.OnActivity(domain.CleanupWorkspaceActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.CleanupWorkspaceActivityInput) error {
					if !input.DeletePlan {
						t.Fatal("apply phase kept the plan it applied")
					}
					events = append(events, "cleanup")
					return nil
				})
			mockRunWrites(env, &writes)

			env.ExecuteWorkflow(TemplateApplyWorkflow, templateRunWorkflowInput(testCase.operation))

			assertWorkflowCompleted(t, env)
			wantWrites := append(setupWrites(true), testCase.applyWrites...)
			wantWrites = append(wantWrites, "status:completed")
			if !reflect.DeepEqual(writes, wantWrites) {
				t.Fatalf("writes = %#v, want %#v", writes, wantWrites)
			}
			wantEvents := []string{"download_plan", "terraform:init", "terraform:select_workspace", "terraform:" + string(testCase.command), "cleanup"}
			if !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("events = %#v, want %#v", events, wantEvents)
			}
		})
	}
}

// An apply that loses its claim finds its plan discarded since it was
// approved. The discard already recorded the run's end, so it opens no session and
// writes nothing.
func TestTemplateApplyWorkflowStopsWhenItLosesTheClaim(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var started []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		started = append(started, info.ActivityType.Name)
	})
	env.OnActivity(domain.BeginApplyActivityName, mock.Anything, mock.Anything).
		Return(domain.BeginApplyActivityOutput{Claimed: false}, nil)

	env.ExecuteWorkflow(TemplateApplyWorkflow, templateRunWorkflowInput(domain.OperationApply))

	assertWorkflowCompleted(t, env)
	if !reflect.DeepEqual(started, []string{domain.BeginApplyActivityName}) {
		t.Fatalf("started activities = %#v, want only the claim", started)
	}
}

// A plan run keeps nothing but its log: its plan is not sealed or uploaded,
// and a plan with changes completes the run instead of waiting for approval.
func TestTemplatePlanWorkflowPlanRunKeepsNoPlan(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var writes []string
	var started []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		started = append(started, info.ActivityType.Name)
	})
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if activityInput.Command == domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{HasChanges: true, Summary: domain.PlanSummary{Add: 2}}, nil
			}
			return domain.RunTerraformActivityOutput{}, nil
		})
	mockRunWrites(env, &writes)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	assertWorkflowCompleted(t, env)
	for _, name := range []string{domain.SealPlanKeyActivityName, domain.UploadPlanActivityName, domain.BeginApplyActivityName} {
		if slices.Contains(started, name) {
			t.Fatalf("started activities = %#v, want no %s", started, name)
		}
	}
	tail := writes[len(writes)-2:]
	if want := []string{"step:planning", "status:completed"}; !reflect.DeepEqual(tail, want) {
		t.Fatalf("final writes = %#v, want %#v", tail, want)
	}
}

// An auto-approved apply run has no plan: the apply workflow claims it from
// queued, records its setup steps, applies without a saved plan, and records
// the counts the apply reported with applied.
func TestTemplateApplyWorkflowAppliesAnAutoApprovedRunWithoutAPlan(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationApply)
	input.AutoApprove = true
	var commands []domain.TerraformCommandType
	var writes []string
	var finished *domain.PlanSummary
	var started []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		started = append(started, info.ActivityType.Name)
	})
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.BeginApplyActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, claim domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
			if !claim.AutoApprove {
				t.Fatal("auto-approved run claimed as an approved one")
			}
			return domain.BeginApplyActivityOutput{Claimed: true}, nil
		})
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			commands = append(commands, activityInput.Command)
			if want := fmt.Sprintf("apply-%d", len(commands)); activityInput.LogName != want {
				t.Errorf("%s logged to %q, want %q", activityInput.Command, activityInput.LogName, want)
			}
			if activityInput.Command == domain.TerraformCommandApplyAutoApprove {
				return domain.RunTerraformActivityOutput{HasChanges: true, Summary: domain.PlanSummary{Add: 2, Destroy: 1}}, nil
			}
			return domain.RunTerraformActivityOutput{}, nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			writes = append(writes, "status:"+string(input.Status))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStepActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStepActivityInput) error {
			writes = append(writes, "step:"+string(input.Step))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunEventActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunEventActivityInput) error {
			writes = append(writes, "event:"+string(input.Event))
			finished = input.Summary
			return nil
		})

	env.ExecuteWorkflow(TemplateApplyWorkflow, input)

	assertWorkflowCompleted(t, env)
	wantCommands := []domain.TerraformCommandType{domain.TerraformCommandInit, domain.TerraformCommandSelectWorkspace, domain.TerraformCommandApplyAutoApprove}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	wantWrites := append(setupWrites(false), "step:applying", "event:applied", "status:completed")
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("writes = %#v, want %#v", writes, wantWrites)
	}
	if finished == nil || *finished != (domain.PlanSummary{Add: 2, Destroy: 1}) {
		t.Fatalf("applied summary = %#v, want the apply's counts", finished)
	}
	for _, name := range []string{domain.SealPlanKeyActivityName, domain.DownloadPlanActivityName} {
		if slices.Contains(started, name) {
			t.Fatalf("started activities = %#v, want no %s", started, name)
		}
	}
}

// Each workflow refuses the runs it does not run: the plan workflow never
// takes an auto-approved run, which has no plan, and the apply workflow never
// takes a plan run, or an auto-approved destroy.
func TestTemplateWorkflowsRejectRunsTheyDoNotRun(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		workflow    any
		operation   domain.OperationType
		autoApprove bool
	}{
		{name: "plan workflow, auto-approved apply", workflow: TemplatePlanWorkflow, operation: domain.OperationApply, autoApprove: true},
		{name: "apply workflow, plan run", workflow: TemplateApplyWorkflow, operation: domain.OperationPlan},
		{name: "apply workflow, auto-approved destroy", workflow: TemplateApplyWorkflow, operation: domain.OperationDestroy, autoApprove: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			var statuses []domain.TemplateRunStatus
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, activityInput domain.TemplateRunStatusActivityInput) error {
					statuses = append(statuses, activityInput.Status)
					return nil
				})
			input := templateRunWorkflowInput(testCase.operation)
			input.AutoApprove = testCase.autoApprove

			env.ExecuteWorkflow(testCase.workflow, input)

			if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
				t.Fatal("workflow ran a run it does not run")
			}
			if !reflect.DeepEqual(statuses, []domain.TemplateRunStatus{domain.TemplateRunFailed}) {
				t.Fatalf("statuses = %#v, want only failed", statuses)
			}
		})
	}
}

// A destroy plan with nothing left to destroy is a finished destroy:
// recording destroyed is what moves the stack template to destroyed.
func TestTemplatePlanWorkflowFinishesADestroyWithNothingToDestroy(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var writes []string
	var commands []domain.TerraformCommandType
	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			commands = append(commands, activityInput.Command)
			return domain.RunTerraformActivityOutput{}, nil
		})
	mockRunWrites(env, &writes)

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationDestroy))

	assertWorkflowCompleted(t, env)
	if !slices.Contains(commands, domain.TerraformCommandPlanDestroy) {
		t.Fatalf("commands = %#v, want a destroy plan", commands)
	}
	tail := writes[len(writes)-3:]
	if want := []string{"step:planning", "event:destroyed", "status:completed"}; !reflect.DeepEqual(tail, want) {
		t.Fatalf("final writes = %#v, want %#v", tail, want)
	}
	for _, command := range commands {
		if command == domain.TerraformCommandDestroy || command == domain.TerraformCommandApply {
			t.Fatalf("commands = %#v, want nothing applied", commands)
		}
	}
}

// TestTemplatePlanWorkflowRejectsUnsupportedOperation verifies that an
// unrecognized operation fails the run before any side effect: no workspace is
// prepared and no terraform command is dispatched. The run records a single
// Failed status carrying the reason, and never starts running.
func TestTemplatePlanWorkflowRejectsUnsupportedOperation(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationType("migrate"))
	var startedActivityNames []string
	env.SetOnActivityStartedListener(func(activityInfo *activity.Info, _ context.Context, _ converter.EncodedValues) {
		startedActivityNames = append(startedActivityNames, activityInfo.ActivityType.Name)
	})
	var statuses []domain.TemplateRunStatus
	var summaries []string
	var commands []domain.TerraformCommandType
	prepared := false
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			prepared = true
			return domain.PrepareWorkspaceActivityOutput{}, nil
		})
	mockRunTerraform(t, env, &commands)
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.TemplateRunStatusActivityInput) error {
			statuses = append(statuses, activityInput.Status)
			summaries = append(summaries, activityInput.ErrorSummary)
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("workflow error = nil, want unsupported operation error")
	}
	if !strings.Contains(err.Error(), "unsupported template run operation") {
		t.Fatalf("workflow error = %v, want unsupported operation error", err)
	}
	if prepared {
		t.Fatal("prepare workspace ran, unsupported operation must fail before side effects")
	}
	if len(commands) != 0 {
		t.Fatalf("commands = %#v, want no terraform commands", commands)
	}
	want := []domain.TemplateRunStatus{domain.TemplateRunFailed}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
	if !strings.Contains(summaries[0], "unsupported template run operation") {
		t.Fatalf("error summary = %q, want the unsupported operation reason", summaries[0])
	}
	if !reflect.DeepEqual(startedActivityNames, []string{domain.RecordTemplateRunStatusActivityName}) {
		t.Fatalf("started activity names = %#v, want only status persistence without session creation", startedActivityNames)
	}
}

func newTemplateRunWorkflowTestEnvironment(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{EnableSessionWorker: true})
	env.RegisterWorkflow(TemplatePlanWorkflow)
	env.RegisterWorkflow(TemplateApplyWorkflow)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.CleanupWorkspaceActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.CleanupWorkspaceActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.SealPlanKeyActivityInput) (domain.SealPlanKeyActivityOutput, error) {
			return domain.SealPlanKeyActivityOutput{SealedPlanKey: []byte("sealed")}, nil
		},
		activity.RegisterOptions{Name: domain.SealPlanKeyActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.PlanArtifactActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.UploadPlanActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.PlanArtifactActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.DownloadPlanActivityName},
	)
	// By default a plan with changes waits for approval (or, on a plan run,
	// completes) and one without completes, as the store decides, and an
	// apply always wins its claim.
	env.RegisterActivityWithOptions(
		func(_ context.Context, input domain.FinishPlanActivityInput) (domain.PlanOutcome, error) {
			if !input.HasChanges {
				return domain.PlanOutcomeNoChanges, nil
			}
			if input.Operation == domain.OperationPlan {
				return domain.PlanOutcomePlanned, nil
			}
			return domain.PlanOutcomeWaiting, nil
		},
		activity.RegisterOptions{Name: domain.FinishPlanActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
			return domain.BeginApplyActivityOutput{Claimed: true}, nil
		},
		activity.RegisterOptions{Name: domain.BeginApplyActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateRunStatusActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateRunStatusActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateRunStepActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateRunStepActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateRunEventActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateRunEventActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.TemplateRunLog) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.RecordTemplateRunLogActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.SealSourceTokenActivityInput) (domain.SealSourceTokenActivityOutput, error) {
			return domain.SealSourceTokenActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.SealSourceTokenActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.SealRunCredentialsActivityInput) (domain.SealRunCredentialsActivityOutput, error) {
			return domain.SealRunCredentialsActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.SealRunCredentialsActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.ReleaseRunKeyActivityInput) error {
			return nil
		},
		activity.RegisterOptions{Name: domain.ReleaseRunKeyActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			return domain.PrepareWorkspaceActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.PrepareWorkspaceActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
			return domain.FetchSourceActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.FetchSourceActivityName},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			return domain.RunTerraformActivityOutput{}, nil
		},
		activity.RegisterOptions{Name: domain.RunTerraformActivityName},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
			return uploadedLog(input), nil
		},
		activity.RegisterOptions{Name: domain.UploadLogActivityName},
	)
	return env
}

// sessionCreationActivityName is the SDK's internal activity that holds an
// executor session open, heartbeating, for as long as the session lives.
const sessionCreationActivityName = "internalSessionCreationActivity"

// uploadedLog is what an honest UploadLog returns for input: the object it
// wrote, described as RecordTemplateRunLog needs a complete log to be.
func uploadedLog(input domain.UploadLogActivityInput) domain.TemplateRunLog {
	return domain.TemplateRunLog{
		Name:        input.LogName,
		ObjectKey:   "logs/" + input.LogName + ".log",
		ContentType: "text/plain; charset=utf-8",
		SizeBytes:   42,
		UploadedAt:  time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

func mockPrepareWorkspace(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()

	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123"}, nil)
}

func mockFetchSource(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()

	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "/tmp/openplan/runs/tenant_123/run_123/source/modules/vpc"}, nil)
}

func mockRunTerraform(t *testing.T, env *testsuite.TestWorkflowEnvironment, commands *[]domain.TerraformCommandType) {
	t.Helper()

	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			*commands = append(*commands, activityInput.Command)
			return domain.RunTerraformActivityOutput{}, nil
		})
}

// mockRunWrites mocks the three control-plane writes a run makes (its status,
// its step, and its stack template events) and appends each to writes, in
// order, as "status:<status>", "step:<step>" or "event:<event>".
func mockRunWrites(env *testsuite.TestWorkflowEnvironment, writes *[]string) {
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			*writes = append(*writes, "status:"+string(input.Status))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStepActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStepActivityInput) error {
			*writes = append(*writes, "step:"+string(input.Step))
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunEventActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunEventActivityInput) error {
			*writes = append(*writes, "event:"+string(input.Event))
			return nil
		})
}

// setupWrites is what every phase records while it readies its workspace,
// with the plan restored in between for an approved apply.
func setupWrites(restoresPlan bool) []string {
	writes := []string{"step:waiting_for_executor", "step:preparing_workspace", "step:fetching_source"}
	if restoresPlan {
		writes = append(writes, "step:restoring_plan")
	}
	return append(writes, "step:initializing", "step:selecting_workspace")
}

func templateRunWorkflowInput(operation domain.OperationType) domain.TemplateRunWorkflowInput {
	return domain.TemplateRunWorkflowInput{
		RunID:           domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       operation,
		SourceRef:       "main",
		WorkspaceName:   "mtp_acme_prod_vpc_a13f9c",
		RepoOwner:       "acme",
		RepoName:        "infra-templates",
		RootPath:        "modules/vpc",
		ConfigJSON:      []byte(`{"region":"us-east-1"}`),
	}
}

func assertWorkflowCompleted(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow returned error: %v", err)
	}
}

// TestTemplatePlanWorkflowExhaustsRetriesOnTransientActivityError asserts a
// persistently transient activity error fails the run after exactly
// defaultRunRetryPolicy.MaximumAttempts attempts, rather than hardcoding the
// current value (1) — so this test doesn't need updating every time the
// policy's attempt count changes.
func TestTemplatePlanWorkflowExhaustsRetriesOnTransientActivityError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)

	var prepareAttempts int
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			prepareAttempts++
			return domain.PrepareWorkspaceActivityOutput{}, errors.New("connection reset")
		})

	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want transient error to fail the run once retries are exhausted")
	}
	if want := defaultRunRetryPolicy.MaximumAttempts; prepareAttempts != int(want) {
		t.Fatalf("prepare attempts = %d, want %d (defaultRunRetryPolicy.MaximumAttempts)", prepareAttempts, want)
	}
}

func TestTemplatePlanWorkflowStopsOnNonRetryableError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)

	var prepareAttempts int
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			prepareAttempts++
			return domain.PrepareWorkspaceActivityOutput{}, temporal.NewNonRetryableApplicationError(
				"invalid configuration",
				"InvalidConfig",
				nil,
			)
		})

	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want non-retryable error")
	}
	if prepareAttempts != 1 {
		t.Fatalf("prepare attempts = %d, want 1 (non-retryable should not retry)", prepareAttempts)
	}
}

func TestTemplatePlanWorkflowPersistsFailedStatusAfterActivityError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)
	var statuses []domain.TemplateRunStatusActivityInput
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			return domain.PrepareWorkspaceActivityOutput{}, temporal.NewNonRetryableApplicationError(
				"invalid configuration",
				"InvalidConfig",
				nil,
			)
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, status domain.TemplateRunStatusActivityInput) error {
			statuses = append(statuses, status)
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("workflow error = %v, want invalid configuration", err)
	}
	if len(statuses) == 0 || statuses[len(statuses)-1].Status != domain.TemplateRunFailed {
		t.Fatalf("final statuses = %#v, want failed", statuses)
	}
	if !strings.Contains(statuses[len(statuses)-1].ErrorSummary, "invalid configuration") {
		t.Fatalf("failure summary = %q, want root activity error", statuses[len(statuses)-1].ErrorSummary)
	}
}

func TestTemplatePlanWorkflowPreservesActivityErrorWhenFailurePersistenceFails(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
			return domain.PrepareWorkspaceActivityOutput{}, temporal.NewNonRetryableApplicationError(
				"invalid configuration",
				"InvalidConfig",
				nil,
			)
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, status domain.TemplateRunStatusActivityInput) error {
			if status.Status == domain.TemplateRunFailed {
				return errors.New("status database unavailable")
			}
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("workflow error = %v, want original activity error", err)
	}
}

// TestTemplatePlanWorkflowExhaustsRetriesOnTerraformError asserts a
// persistently transient plan error fails the run after exactly
// terraformRetryPolicy.MaximumAttempts attempts, rather than hardcoding the
// current value (1) — so this test doesn't need updating every time the
// policy's attempt count changes.
func TestTemplatePlanWorkflowExhaustsRetriesOnTerraformError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)

	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)

	var planAttempts int
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if activityInput.Command == domain.TerraformCommandPlan {
				planAttempts++
				return domain.RunTerraformActivityOutput{}, errors.New("rate limit exceeded")
			}
			return domain.RunTerraformActivityOutput{}, nil
		})

	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want transient error to fail the run once retries are exhausted")
	}
	if want := terraformRetryPolicy.MaximumAttempts; planAttempts != int(want) {
		t.Fatalf("plan attempts = %d, want %d (terraformRetryPolicy.MaximumAttempts)", planAttempts, want)
	}
}

func TestTemplatePlanWorkflowNonRetryableTerraformError(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	input := templateRunWorkflowInput(domain.OperationPlan)

	mockPrepareWorkspace(t, env)
	mockFetchSource(t, env)

	var terraformAttempts int
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, activityInput domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			terraformAttempts++
			if activityInput.Command == domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, temporal.NewNonRetryableApplicationError(
					"unsupported terraform command",
					"UnsupportedCommand",
					nil,
				)
			}
			return domain.RunTerraformActivityOutput{}, nil
		})

	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).Return(nil)

	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow error is nil, want non-retryable error")
	}
	// Attempts: init (1), select_workspace (1), plan (1 - non-retryable)
	if terraformAttempts != 3 {
		t.Fatalf("terraform attempts = %d, want 3 (non-retryable should not retry)", terraformAttempts)
	}
}

func TestDefaultRunRetryPolicy(t *testing.T) {
	t.Parallel()

	if defaultRunRetryPolicy.MaximumAttempts != 1 {
		t.Fatalf("MaximumAttempts = %d, want 1", defaultRunRetryPolicy.MaximumAttempts)
	}
	if defaultRunRetryPolicy.InitialInterval != 30*time.Second {
		t.Fatalf("InitialInterval = %v, want 30s", defaultRunRetryPolicy.InitialInterval)
	}
	if defaultRunRetryPolicy.BackoffCoefficient != 2.0 {
		t.Fatalf("BackoffCoefficient = %f, want 2.0", defaultRunRetryPolicy.BackoffCoefficient)
	}
	if defaultRunRetryPolicy.MaximumInterval != 5*time.Minute {
		t.Fatalf("MaximumInterval = %v, want 5m", defaultRunRetryPolicy.MaximumInterval)
	}
	wantNonRetryable := []string{"InvalidConfig", "UnsupportedCommand"}
	if !reflect.DeepEqual(defaultRunRetryPolicy.NonRetryableErrorTypes, wantNonRetryable) {
		t.Fatalf("NonRetryableErrorTypes = %v, want %v", defaultRunRetryPolicy.NonRetryableErrorTypes, wantNonRetryable)
	}
}

func TestTerraformRetryPolicy(t *testing.T) {
	t.Parallel()

	if terraformRetryPolicy.MaximumAttempts != 1 {
		t.Fatalf("MaximumAttempts = %d, want 1", terraformRetryPolicy.MaximumAttempts)
	}
	if terraformRetryPolicy.InitialInterval != time.Minute {
		t.Fatalf("InitialInterval = %v, want 1m", terraformRetryPolicy.InitialInterval)
	}
	if terraformRetryPolicy.BackoffCoefficient != 2.0 {
		t.Fatalf("BackoffCoefficient = %f, want 2.0", terraformRetryPolicy.BackoffCoefficient)
	}
	if terraformRetryPolicy.MaximumInterval != 10*time.Minute {
		t.Fatalf("MaximumInterval = %v, want 10m", terraformRetryPolicy.MaximumInterval)
	}
	wantNonRetryable := []string{"InvalidConfig", "UnsupportedCommand"}
	if !reflect.DeepEqual(terraformRetryPolicy.NonRetryableErrorTypes, wantNonRetryable) {
		t.Fatalf("NonRetryableErrorTypes = %v, want %v", terraformRetryPolicy.NonRetryableErrorTypes, wantNonRetryable)
	}
}

// The log policies retry, unlike the run's default: these are the values the
// workflow is built around.
func TestLogRetryPolicies(t *testing.T) {
	t.Parallel()

	wantUpload := temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    30 * time.Second,
		MaximumAttempts:    5,
	}
	if !reflect.DeepEqual(*uploadLogRetryPolicy, wantUpload) {
		t.Fatalf("uploadLogRetryPolicy = %+v, want %+v", *uploadLogRetryPolicy, wantUpload)
	}
	wantRecord := temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumAttempts:    3,
	}
	if !reflect.DeepEqual(*recordLogRetryPolicy, wantRecord) {
		t.Fatalf("recordLogRetryPolicy = %+v, want %+v", *recordLogRetryPolicy, wantRecord)
	}
}

// TestTemplatePlanWorkflowPinsLogIdentityToTheRun proves the control plane does
// not take the executor's word for whose run a log belongs to.
//
// RunTerraform is a data-plane activity, so its result is attacker-controlled
// once an executor is compromised. RecordTemplateRunLog upserts on
// (tenant_id, run_id, phase) and guards only that the run exists, not that it
// is this run — so a returned TenantID/RunID naming another tenant's run would
// repoint that run's object_key at a key of the executor's choosing.
//
// The workflow knows the identity from its own input and overwrites it. On the
// honest path this changes nothing: PutTemplateRunLog derives both fields from
// the activity input the workflow supplied.
func TestTemplatePlanWorkflowPinsLogIdentityToTheRun(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)

	// A hostile executor claims every log belongs to another tenant's run,
	// under another log's name.
	var named []string
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
			named = append(named, input.LogName)
			return domain.TemplateRunLog{
				TenantID:    domain.TenantID("tenant_victim"),
				RunID:       domain.TemplateRunID("run_victim"),
				Name:        "apply-9",
				RunPhase:    domain.RunPhaseApply,
				Seq:         9,
				Command:     domain.TerraformCommandDestroy,
				ObjectKey:   "tenants/tenant_victim/runs/run_victim/logs/apply.log",
				ContentType: "text/plain; charset=utf-8",
				UploadedAt:  time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
			}, nil
		})

	var recorded []domain.TemplateRunLog
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log)
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(nil)

	input := templateRunWorkflowInput(domain.OperationPlan)
	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	assertWorkflowCompleted(t, env)
	if len(recorded) == 0 {
		t.Fatal("no log metadata recorded")
	}
	// Each command records its log twice, live and then complete, so the
	// i'th row is command i/2's.
	if len(recorded) != 2*len(named) {
		t.Fatalf("recorded %d log rows for %d commands, want two each", len(recorded), len(named))
	}
	for i, log := range recorded {
		wantState := domain.TemplateRunLogLive
		if i%2 == 1 {
			wantState = domain.TemplateRunLogComplete
		}
		if log.State != wantState {
			t.Fatalf("recorded log row %d as %s, want %s", i, log.State, wantState)
		}
		if log.TenantID != input.TenantID {
			t.Fatalf("recorded %s log tenant = %q, want %q", log.Name, log.TenantID, input.TenantID)
		}
		if log.RunID != input.RunID {
			t.Fatalf("recorded %s log run = %q, want %q", log.Name, log.RunID, input.RunID)
		}
		if log.Name != named[i/2] {
			t.Fatalf("recorded log row %d as %q, want the workflow's name %q", i, log.Name, named[i/2])
		}
		// What wrote the log is the workflow's to say too: a plan workflow
		// never runs a destroy.
		if log.RunPhase != domain.RunPhasePlan || log.Command == domain.TerraformCommandDestroy {
			t.Fatalf("recorded %s log as %s/%s, want the plan phase's own command", log.Name, log.RunPhase, log.Command)
		}
		if want, _ := domain.RunLogName(log.RunPhase, log.Seq); want != log.Name || log.Seq != i/2+1 {
			t.Fatalf("recorded log row %d as %q, number %d, want %q, number %d", i, log.Name, log.Seq, want, i/2+1)
		}
	}
}

// The same pin has to hold on the failure path: the failed command's log is
// uploaded too, and its metadata is the executor's to forge.
func TestTemplatePlanWorkflowPinsLogIdentityOfFailedCommand(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(domain.FetchSourceActivityOutput{TerraformPath: "run/workspace/source"}, nil)

	hostileLog := domain.TemplateRunLog{
		TenantID:    domain.TenantID("tenant_victim"),
		RunID:       domain.TemplateRunID("run_victim"),
		Name:        "plan",
		ObjectKey:   "logs/plan.log",
		ContentType: "text/plain; charset=utf-8",
		UploadedAt:  time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
	var recorded []domain.TemplateRunLog
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command != domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, nil
			}
			return domain.RunTerraformActivityOutput{}, errors.New("terraform plan failed")
		})
	env.OnActivity(domain.UploadLogActivityName, mock.Anything, mock.Anything).
		Return(hostileLog, nil)
	env.OnActivity(domain.RecordTemplateRunLogActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, log domain.TemplateRunLog) error {
			recorded = append(recorded, log)
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(nil)

	input := templateRunWorkflowInput(domain.OperationPlan)
	env.ExecuteWorkflow(TemplatePlanWorkflow, input)

	if len(recorded) == 0 {
		t.Fatal("no log metadata recorded for the failed command")
	}
	for _, log := range recorded {
		if log.TenantID != input.TenantID || log.RunID != input.RunID {
			t.Fatalf("recorded %s log identity = %q/%q, want %q/%q",
				log.Name, log.TenantID, log.RunID, input.TenantID, input.RunID)
		}
	}
}

// A job that fails still gives its executor back: the run's key is released
// and its workspace deleted, as after a job that succeeds. (Completing the
// session itself is not observable from the test environment.)
func TestTemplatePlanWorkflowClosesTheSessionOfAFailedJob(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	var teardown []string
	env.OnActivity(domain.PrepareWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(domain.PrepareWorkspaceActivityOutput{WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123", PublicKey: []byte("run-key")}, nil)
	mockFetchSource(t, env)
	env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
			if input.Command == domain.TerraformCommandPlan {
				return domain.RunTerraformActivityOutput{}, errors.New("plan exploded")
			}
			return domain.RunTerraformActivityOutput{}, nil
		})
	env.OnActivity(domain.ReleaseRunKeyActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.ReleaseRunKeyActivityInput) error {
			teardown = append(teardown, "release_key")
			return nil
		})
	env.OnActivity(domain.CleanupWorkspaceActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.CleanupWorkspaceActivityInput) error {
			teardown = append(teardown, "cleanup_workspace")
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatal("workflow succeeded, want the failed plan to fail it")
	}
	if want := []string{"release_key", "cleanup_workspace"}; !reflect.DeepEqual(teardown, want) {
		t.Fatalf("teardown = %#v, want %#v", teardown, want)
	}
}

// A step whose record fails never starts its work, and the run ends failed.
func TestTemplatePlanWorkflowDoesNotStartAStepItCouldNotRecord(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	fetched := false
	var statuses []domain.TemplateRunStatus
	mockPrepareWorkspace(t, env)
	env.OnActivity(domain.FetchSourceActivityName, mock.Anything, mock.Anything).
		Return(func(context.Context, domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
			fetched = true
			return domain.FetchSourceActivityOutput{}, nil
		})
	env.OnActivity(domain.RecordTemplateRunStepActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStepActivityInput) error {
			if input.Step == domain.TemplateRunStepFetchingSource {
				return errors.New("database unavailable")
			}
			return nil
		})
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			statuses = append(statuses, input.Status)
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationPlan))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatal("workflow succeeded, want the failed step write to fail it")
	}
	if fetched {
		t.Fatal("fetch source ran although its step was never recorded")
	}
	if want := []domain.TemplateRunStatus{domain.TemplateRunRunning, domain.TemplateRunFailed}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
}

// A run whose failure cannot be recorded returns both errors: the one that
// failed it, still matchable, and the one that kept its status from saying so.
func TestTemplatePlanWorkflowKeepsItsErrorWhenItsFailureCannotBeRecorded(t *testing.T) {
	t.Parallel()

	env := newTemplateRunWorkflowTestEnvironment(t)
	env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
		Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
			if input.Status == domain.TemplateRunFailed {
				return errors.New("database unavailable")
			}
			return nil
		})

	env.ExecuteWorkflow(TemplatePlanWorkflow, templateRunWorkflowInput(domain.OperationType("migrate")))

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("workflow error is nil, want the validation error")
	}
	for _, want := range []string{"unsupported template run operation", "also failed to persist failure status", "database unavailable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("workflow error = %q, want it to mention %q", err, want)
		}
	}
}

// Each workflow tells the control plane which execution of the run it is,
// and which Temporal workflow carries it, on every status write and on its
// claim.
func TestTemplateRunWorkflowsRecordTheirPhaseAndWorkflowID(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		workflow   any
		operation  domain.OperationType
		phase      domain.RunPhase
		workflowID string
	}{
		{name: "plan", workflow: TemplatePlanWorkflow, operation: domain.OperationPlan, phase: domain.RunPhasePlan, workflowID: "template-run/tenant_123/run_123"},
		{name: "apply", workflow: TemplateApplyWorkflow, operation: domain.OperationApply, phase: domain.RunPhaseApply, workflowID: "template-run/tenant_123/run_123/apply"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: testCase.workflowID})
			var statuses []domain.TemplateRunStatusActivityInput
			var claims []domain.BeginApplyActivityInput
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
					statuses = append(statuses, input)
					return nil
				})
			env.OnActivity(domain.BeginApplyActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
					claims = append(claims, input)
					return domain.BeginApplyActivityOutput{Claimed: true}, nil
				})

			env.ExecuteWorkflow(testCase.workflow, templateRunWorkflowInput(testCase.operation))

			assertWorkflowCompleted(t, env)
			if len(statuses) == 0 {
				t.Fatal("no status write")
			}
			for _, status := range statuses {
				if status.Phase != testCase.phase || status.WorkflowID != testCase.workflowID {
					t.Fatalf("status write = %#v, want phase %q and workflow %q", status, testCase.phase, testCase.workflowID)
				}
			}
			if testCase.phase == domain.RunPhaseApply && (len(claims) != 1 || claims[0].WorkflowID != testCase.workflowID) {
				t.Fatalf("claims = %#v, want one by %q", claims, testCase.workflowID)
			}
		})
	}
}

// An approved apply and a destroy record what their command did with the
// event that ends them, as an auto-approved apply always has.
func TestTemplateApplyWorkflowRecordsTheCountsItsCommandReports(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		operation domain.OperationType
		command   domain.TerraformCommandType
		event     domain.TemplateRunEvent
	}{
		{operation: domain.OperationApply, command: domain.TerraformCommandApply, event: domain.TemplateRunApplied},
		{operation: domain.OperationDestroy, command: domain.TerraformCommandDestroy, event: domain.TemplateRunDestroyed},
	} {
		t.Run(string(testCase.operation), func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			counts := domain.PlanSummary{Add: 1, Change: 2, Destroy: 3}
			summaries := map[domain.TemplateRunEvent]*domain.PlanSummary{}
			env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
					if input.Command == testCase.command {
						return domain.RunTerraformActivityOutput{HasChanges: true, Summary: counts}, nil
					}
					return domain.RunTerraformActivityOutput{}, nil
				})
			env.OnActivity(domain.RecordTemplateRunEventActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.TemplateRunEventActivityInput) error {
					summaries[input.Event] = input.Summary
					return nil
				})

			env.ExecuteWorkflow(TemplateApplyWorkflow, templateRunWorkflowInput(testCase.operation))

			assertWorkflowCompleted(t, env)
			if got := summaries[testCase.event]; got == nil || *got != counts {
				t.Fatalf("%s summary = %#v, want the command's counts", testCase.event, got)
			}
		})
	}
}
