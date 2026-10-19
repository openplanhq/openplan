package workflows

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/temporalproto"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/durationpb"
)

// recordHistoriesEnv names the directory TestRecordHistories writes to, and
// turns it on: it needs a real Temporal server, so `go test ./...` skips it.
// scripts/workflow-replay.sh sets it when it runs this test from an older
// release's checkout, so the name is a contract between releases: keep it.
const recordHistoriesEnv = "OPENPLAN_RECORD_HISTORIES"

// historyNamespace keeps the recording away from the default namespace, where
// a running api and executor poll the same task queues and would take the
// recording's activities for real ones.
const historyNamespace = "openplan-histories"

// runScenario is one path through a template run: what each stubbed activity
// answers, keyed by the run it is called for.
type runScenario struct {
	name     string
	workflow string
	input    domain.TemplateRunWorkflowInput
	// hasChanges is what every plan reports; failCommand, if set, is the
	// Terraform command that fails.
	hasChanges  bool
	failCommand domain.TerraformCommandType
	// uploadFails loses every log the run uploads.
	uploadFails bool
	outcome     domain.PlanOutcome
	claimLost   bool
	wantErr     bool
}

// syncScenario is one path through a template sync.
type syncScenario struct {
	name    string
	output  domain.TemplateSyncActivityOutput
	err     error
	wantErr bool
}

// historyRunScenarios cover every branch the run workflows take on what their
// activities answer. Each records one history, so between them a change to any
// branch's commands fails a replay.
func historyRunScenarios() []runScenario {
	input := func(name string, operation domain.OperationType, autoApprove bool) domain.TemplateRunWorkflowInput {
		return domain.TemplateRunWorkflowInput{
			RunID:             domain.TemplateRunID("run-" + name),
			TenantID:          "tenant-history",
			StackTemplateID:   "stack-template-history",
			Operation:         operation,
			AutoApprove:       autoApprove,
			SelectedRef:       "main",
			ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			WorkspaceName:     "history",
			RepoOwner:         "openplanhq",
			RepoName:          "demo-templates",
			RootPath:          "random-pet",
			ConfigJSON:        []byte(`{}`),
		}
	}
	plan, apply := domain.TemplatePlanWorkflowName, domain.TemplateApplyWorkflowName
	return []runScenario{
		{name: "plan-planned", workflow: plan, input: input("plan-planned", domain.OperationPlan, false), hasChanges: true, outcome: domain.PlanOutcomePlanned},
		{name: "plan-apply-waiting", workflow: plan, input: input("plan-apply-waiting", domain.OperationApply, false), hasChanges: true, outcome: domain.PlanOutcomeWaiting},
		{name: "plan-destroy-no-changes", workflow: plan, input: input("plan-destroy-no-changes", domain.OperationDestroy, false), outcome: domain.PlanOutcomeNoChanges},
		{name: "plan-terraform-failed", workflow: plan, input: input("plan-terraform-failed", domain.OperationPlan, false), failCommand: domain.TerraformCommandPlan, wantErr: true},
		{name: "plan-log-lost", workflow: plan, input: input("plan-log-lost", domain.OperationPlan, false), uploadFails: true, outcome: domain.PlanOutcomePlanned},
		{name: "plan-rejected", workflow: plan, input: input("plan-rejected", domain.OperationApply, true), wantErr: true},
		{name: "apply-approved", workflow: apply, input: input("apply-approved", domain.OperationApply, false), hasChanges: true},
		{name: "apply-auto-approved", workflow: apply, input: input("apply-auto-approved", domain.OperationApply, true), hasChanges: true},
		{name: "apply-destroy", workflow: apply, input: input("apply-destroy", domain.OperationDestroy, false), hasChanges: true},
		{name: "apply-claim-lost", workflow: apply, input: input("apply-claim-lost", domain.OperationApply, false), claimLost: true},
		{name: "apply-terraform-failed", workflow: apply, input: input("apply-terraform-failed", domain.OperationApply, false), failCommand: domain.TerraformCommandApply, wantErr: true},
	}
}

func historySyncScenarios() []syncScenario {
	return []syncScenario{
		{name: "sync-completed", output: domain.TemplateSyncActivityOutput{TemplateRevisionID: "revision-history", ResolvedCommitSHA: "0123456789abcdef0123456789abcdef01234567"}},
		{name: "sync-invalid", output: domain.TemplateSyncActivityOutput{Status: domain.TemplateRegistrationInvalid, ErrorSummary: "no Terraform files"}},
		// RepositoryNotFound is one of syncRetryPolicy's non-retryable types,
		// so the recording does not wait out four attempts.
		{name: "sync-failed", err: temporal.NewApplicationError("repository not found", "RepositoryNotFound"), wantErr: true},
	}
}

// TestRecordHistories runs every scenario on a real Temporal server, with
// stubbed activities, and writes each execution's history as JSON to the
// directory recordHistoriesEnv names. scripts/workflow-replay.sh runs it from
// the last release's checkout and replays what it wrote against the code as it
// is now: those are the runs an install has in flight when it upgrades.
//
// Nothing it records is committed. A release's histories are its own code's
// to produce, from its own scenarios, whenever a later change needs them.
func TestRecordHistories(t *testing.T) {
	dir := os.Getenv(recordHistoriesEnv)
	if dir == "" {
		t.Skipf("set %s to a directory to record workflow histories against a real Temporal server", recordHistoriesEnv)
	}
	address := os.Getenv("TEMPORAL_ADDRESS")
	if address == "" {
		address = "localhost:7233"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	temporalClient := dialHistoryNamespace(ctx, t, address)
	defer temporalClient.Close()

	runs := historyRunScenarios()
	syncs := historySyncScenarios()
	// A workflow with no scenario would record nothing and replay nothing.
	covered := map[string]bool{}
	for _, scenario := range runs {
		covered[scenario.workflow] = true
	}
	if len(syncs) > 0 {
		covered[domain.TemplateSyncWorkflowName] = true
	}
	for _, registration := range Registered() {
		if !covered[registration.Name] {
			t.Fatalf("no scenario records %s: add one to TestRecordHistories", registration.Name)
		}
	}
	stubs := &historyStubs{runs: map[domain.TemplateRunID]runScenario{}, syncs: map[domain.TemplateRegistrationID]syncScenario{}}
	for _, scenario := range runs {
		stubs.runs[scenario.input.RunID] = scenario
	}
	for _, scenario := range syncs {
		stubs.syncs[syncRegistrationID(scenario.name)] = scenario
	}
	startHistoryWorkers(t, temporalClient, stubs)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range runs {
		recordHistory(ctx, t, temporalClient, dir, scenario.name, scenario.workflow, scenario.input, scenario.wantErr)
	}
	for _, scenario := range syncs {
		recordHistory(ctx, t, temporalClient, dir, scenario.name, domain.TemplateSyncWorkflowName, domain.TemplateSyncWorkflowInput{
			RegistrationID: syncRegistrationID(scenario.name),
			TenantID:       "tenant-history",
			RepoOwner:      "openplanhq",
			RepoName:       "demo-templates",
			SourceRef:      "main",
			RootPath:       "random-pet",
		}, scenario.wantErr)
	}
}

func syncRegistrationID(name string) domain.TemplateRegistrationID {
	return domain.TemplateRegistrationID("registration-" + name)
}

// dialHistoryNamespace registers the recording's namespace if it is missing,
// and dials it once the server serves it: a namespace just registered takes a
// few seconds to reach every service's cache.
func dialHistoryNamespace(ctx context.Context, t *testing.T, address string) client.Client {
	t.Helper()
	namespaces, err := client.NewNamespaceClient(client.Options{HostPort: address})
	if err != nil {
		t.Fatalf("dial Temporal at %s: %v", address, err)
	}
	defer namespaces.Close()
	err = namespaces.Register(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        historyNamespace,
		WorkflowExecutionRetentionPeriod: durationpb.New(24 * time.Hour),
	})
	var exists *serviceerror.NamespaceAlreadyExists
	if err != nil && !errors.As(err, &exists) {
		t.Fatalf("register namespace %s: %v", historyNamespace, err)
	}

	// A fixed identity, since the default one names the recording machine,
	// and the workers and session queues inherit it into every history.
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: historyNamespace, Identity: "openplan-history-recorder"})
	if err != nil {
		t.Fatalf("dial namespace %s: %v", historyNamespace, err)
	}
	for {
		_, err := temporalClient.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{Namespace: historyNamespace, PageSize: 1})
		if err == nil {
			return temporalClient
		}
		select {
		case <-ctx.Done():
			t.Fatalf("namespace %s never became ready: %v", historyNamespace, err)
		case <-time.After(time.Second):
		}
	}
}

// startHistoryWorkers starts the two workers a run needs: the control worker,
// which runs the workflows and the control activities, and an executor worker
// with sessions on the execution queue.
func startHistoryWorkers(t *testing.T, temporalClient client.Client, stubs *historyStubs) {
	t.Helper()
	control := worker.New(temporalClient, domain.ControlTaskQueue, worker.Options{})
	for _, registration := range Registered() {
		control.RegisterWorkflowWithOptions(registration.Workflow, workflow.RegisterOptions{Name: registration.Name})
	}
	executor := worker.New(temporalClient, domain.ExecutionTaskQueue, worker.Options{EnableSessionWorker: true})
	for name, fn := range map[string]any{
		domain.RecordTemplateRunStatusActivityName:          stubs.noop,
		domain.RecordTemplateRunStepActivityName:            stubs.noop,
		domain.RecordTemplateRunEventActivityName:           stubs.noop,
		domain.RecordTemplateRunLogActivityName:             stubs.noop,
		domain.RecordTemplateRegistrationStatusActivityName: stubs.noop,
		domain.RecordTemplateRegistrationStepActivityName:   stubs.noop,
		domain.SealSourceTokenActivityName:                  stubs.sealSourceToken,
		domain.SealRunCredentialsActivityName:               stubs.sealRunCredentials,
		domain.SealPlanKeyActivityName:                      stubs.sealPlanKey,
		domain.FinishPlanActivityName:                       stubs.finishPlan,
		domain.BeginApplyActivityName:                       stubs.beginApply,
		domain.SyncTemplateActivityName:                     stubs.syncTemplate,
	} {
		control.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	for name, fn := range map[string]any{
		domain.PrepareWorkspaceActivityName: stubs.prepareWorkspace,
		domain.FetchSourceActivityName:      stubs.fetchSource,
		domain.RunTerraformActivityName:     stubs.runTerraform,
		domain.UploadLogActivityName:        stubs.uploadLog,
		domain.UploadPlanActivityName:       stubs.noop,
		domain.DownloadPlanActivityName:     stubs.noop,
		domain.ReleaseRunKeyActivityName:    stubs.noop,
		domain.CleanupWorkspaceActivityName: stubs.noop,
	} {
		executor.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	for _, w := range []worker.Worker{control, executor} {
		if err := w.Start(); err != nil {
			t.Fatalf("start worker: %v", err)
		}
		t.Cleanup(w.Stop)
	}
}

// recordHistory runs one workflow to its end and writes its history as
// <name>.json in dir, in the JSON the replayer and `temporal workflow show
// --output json` both read.
func recordHistory(ctx context.Context, t *testing.T, temporalClient client.Client, dir, name, workflowName string, input any, wantErr bool) {
	t.Helper()
	// A unique ID, since the namespace keeps a day of earlier recordings.
	id := fmt.Sprintf("history-%s-%d", name, time.Now().UnixNano())
	execution, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: domain.ControlTaskQueue}, workflowName, input)
	if err != nil {
		t.Fatalf("%s: start: %v", name, err)
	}
	if err := execution.Get(ctx, nil); (err != nil) != wantErr {
		t.Fatalf("%s: workflow error = %v, want error %v", name, err, wantErr)
	}

	history := &historypb.History{}
	events := temporalClient.GetWorkflowHistory(ctx, id, execution.GetRunID(), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for events.HasNext() {
		event, err := events.Next()
		if err != nil {
			t.Fatalf("%s: read history: %v", name, err)
		}
		history.Events = append(history.Events, event)
	}
	encoded, err := temporalproto.CustomJSONMarshalOptions{Indent: "  "}.Marshal(history)
	if err != nil {
		t.Fatalf("%s: encode history: %v", name, err)
	}
	// The SDK names sticky and session queues after the host, whatever the
	// client's identity. Replay never compares queue names, so the recording
	// machine's name can go.
	if host, err := os.Hostname(); err == nil && host != "" {
		encoded = bytes.ReplaceAll(encoded, []byte(host), []byte("history-recorder"))
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded %s: %d events", name, len(history.Events))
}

// historyStubs answers every activity the workflows schedule, as the scenario
// of the run or registration it is called for says. Nothing here touches a
// database, an executor or a repository: a history records only what the
// activities returned.
type historyStubs struct {
	runs  map[domain.TemplateRunID]runScenario
	syncs map[domain.TemplateRegistrationID]syncScenario
}

// noop stands in for every activity whose result the workflows ignore.
func (*historyStubs) noop(context.Context, any) error { return nil }

func (*historyStubs) sealSourceToken(context.Context, domain.SealSourceTokenActivityInput) (domain.SealSourceTokenActivityOutput, error) {
	return domain.SealSourceTokenActivityOutput{SealedToken: []byte("sealed-token")}, nil
}

func (*historyStubs) sealRunCredentials(context.Context, domain.SealRunCredentialsActivityInput) (domain.SealRunCredentialsActivityOutput, error) {
	return domain.SealRunCredentialsActivityOutput{SealedEnvironment: []byte("sealed-environment")}, nil
}

func (*historyStubs) sealPlanKey(context.Context, domain.SealPlanKeyActivityInput) (domain.SealPlanKeyActivityOutput, error) {
	return domain.SealPlanKeyActivityOutput{SealedPlanKey: []byte("sealed-plan-key")}, nil
}

func (stubs *historyStubs) finishPlan(_ context.Context, input domain.FinishPlanActivityInput) (domain.PlanOutcome, error) {
	return stubs.runs[input.RunID].outcome, nil
}

func (stubs *historyStubs) beginApply(_ context.Context, input domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
	return domain.BeginApplyActivityOutput{Claimed: !stubs.runs[input.RunID].claimLost}, nil
}

func (stubs *historyStubs) syncTemplate(_ context.Context, input domain.TemplateSyncActivityInput) (domain.TemplateSyncActivityOutput, error) {
	scenario := stubs.syncs[input.RegistrationID]
	return scenario.output, scenario.err
}

func (*historyStubs) prepareWorkspace(_ context.Context, input domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
	return domain.PrepareWorkspaceActivityOutput{WorkspacePath: "/runs/" + string(input.RunID), PublicKey: []byte("public-key")}, nil
}

func (*historyStubs) fetchSource(_ context.Context, input domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
	return domain.FetchSourceActivityOutput{TerraformPath: input.WorkspacePath + "/source/" + input.RootPath}, nil
}

func (stubs *historyStubs) runTerraform(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	scenario := stubs.runs[input.RunID]
	if input.Command == scenario.failCommand {
		return domain.RunTerraformActivityOutput{}, temporal.NewNonRetryableApplicationError("terraform exited 1", "TerraformFailed", nil)
	}
	return domain.RunTerraformActivityOutput{HasChanges: scenario.hasChanges, Summary: domain.PlanSummary{Add: 1}}, nil
}

func (stubs *historyStubs) uploadLog(_ context.Context, input domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
	if stubs.runs[input.RunID].uploadFails {
		// Non-retryable, so the recording does not wait out
		// uploadLogRetryPolicy; the history holds only the final failure.
		return domain.TemplateRunLog{}, temporal.NewNonRetryableApplicationError("object store unavailable", "UploadFailed", nil)
	}
	return domain.TemplateRunLog{ContentType: "text/plain", SizeBytes: 1, UploadedAt: time.Now()}, nil
}
