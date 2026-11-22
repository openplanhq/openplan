package temporal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

var _ app.WorkflowDispatcher = (*Dispatcher)(nil)

func TestStartTemplateRunExecutesWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := NewDispatcher(workflowClient, DispatcherOptions{})
	input := domain.TemplateRunWorkflowInput{
		RunID:           domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		SourceRef:       "main",
		WorkspaceName:   "mtp_acme_prod_vpc_a13f9c",
	}

	if err := dispatcher.StartTemplatePlan(context.Background(), input); err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}

	if workflowClient.executeOptions.ID != "template-run/tenant_123/run_123" {
		t.Fatalf("workflow ID = %q", workflowClient.executeOptions.ID)
	}
	if workflowClient.executeOptions.TaskQueue != domain.ControlTaskQueue {
		t.Fatalf("task queue = %q", workflowClient.executeOptions.TaskQueue)
	}
	if workflowClient.executeOptions.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE {
		t.Fatalf("workflow ID reuse policy = %v, want reject duplicate", workflowClient.executeOptions.WorkflowIDReusePolicy)
	}
	if workflowClient.executeOptions.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING {
		t.Fatalf("workflow ID conflict policy = %v, want use existing", workflowClient.executeOptions.WorkflowIDConflictPolicy)
	}
	if workflowClient.executeWorkflow != domain.TemplatePlanWorkflowName {
		t.Fatalf("workflow name = %#v", workflowClient.executeWorkflow)
	}
	if len(workflowClient.executeArgs) != 1 {
		t.Fatalf("workflow arg count = %d, want 1", len(workflowClient.executeArgs))
	}
	if !reflect.DeepEqual(workflowClient.executeArgs[0], input) {
		t.Fatalf("workflow input = %#v, want %#v", workflowClient.executeArgs[0], input)
	}
}

// TestStartTemplateRunStampsTheConfiguredTerraformTimeout pins where the
// Terraform ceiling enters a run. Starting is the last moment a queued run can
// pick up the deployment's current setting; after that the value travels in
// the workflow input, where the run keeps it and an operator can read it back
// out of history.
func TestStartTemplateRunStampsTheConfiguredTerraformTimeout(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := NewDispatcher(workflowClient, DispatcherOptions{TerraformTimeout: 90 * time.Minute})

	if err := dispatcher.StartTemplatePlan(context.Background(), domain.TemplateRunWorkflowInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}); err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}

	started, ok := workflowClient.executeArgs[0].(domain.TemplateRunWorkflowInput)
	if !ok {
		t.Fatalf("workflow input = %#v, want a TemplateRunWorkflowInput", workflowClient.executeArgs[0])
	}
	if started.TerraformTimeout != 90*time.Minute {
		t.Fatalf("TerraformTimeout = %v, want 90m", started.TerraformTimeout)
	}
}

// TestStartTemplateRunWithoutAConfiguredTimeoutLeavesItToTheWorkflow keeps an
// unconfigured dispatcher from stamping a zero that reads as a deliberate
// choice. The workflow, not the dispatcher, owns the default.
func TestStartTemplateRunWithoutAConfiguredTimeoutLeavesItToTheWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := NewDispatcher(workflowClient, DispatcherOptions{})

	if err := dispatcher.StartTemplatePlan(context.Background(), domain.TemplateRunWorkflowInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	}); err != nil {
		t.Fatalf("StartTemplateRun returned error: %v", err)
	}

	started := workflowClient.executeArgs[0].(domain.TemplateRunWorkflowInput)
	if started.TerraformTimeout != 0 {
		t.Fatalf("TerraformTimeout = %v, want zero so the workflow applies its default", started.TerraformTimeout)
	}
}

func TestStartTemplateSyncExecutesWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := NewDispatcher(workflowClient, DispatcherOptions{})
	input := domain.TemplateSyncWorkflowInput{
		SyncID:    domain.TemplateSyncID("template_sync_123"),
		TenantID:  domain.TenantID("tenant_123"),
		RepoOwner: "acme",
		RepoName:  "infra-templates",
		SourceRef: "v0.0.1",
		RootPath:  "modules/vpc",
	}

	if err := dispatcher.StartTemplateSync(context.Background(), input); err != nil {
		t.Fatalf("StartTemplateSync returned error: %v", err)
	}

	if workflowClient.executeOptions.ID != "template-sync/tenant_123/template_sync_123" {
		t.Fatalf("workflow ID = %q", workflowClient.executeOptions.ID)
	}
	if workflowClient.executeOptions.TaskQueue != domain.ControlTaskQueue {
		t.Fatalf("task queue = %q", workflowClient.executeOptions.TaskQueue)
	}
	if workflowClient.executeWorkflow != domain.TemplateSyncWorkflowName {
		t.Fatalf("workflow name = %#v", workflowClient.executeWorkflow)
	}
	if len(workflowClient.executeArgs) != 1 {
		t.Fatalf("workflow arg count = %d, want 1", len(workflowClient.executeArgs))
	}
	if !reflect.DeepEqual(workflowClient.executeArgs[0], input) {
		t.Fatalf("workflow input = %#v, want %#v", workflowClient.executeArgs[0], input)
	}
	// The queue delivers at least once: a redelivered start must find the sync
	// already running, not start a second one under the same ID once the first
	// has finished.
	if workflowClient.executeOptions.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE {
		t.Fatalf("workflow ID reuse policy = %v, want reject duplicate", workflowClient.executeOptions.WorkflowIDReusePolicy)
	}
	if workflowClient.executeOptions.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING {
		t.Fatalf("workflow ID conflict policy = %v, want use existing", workflowClient.executeOptions.WorkflowIDConflictPolicy)
	}
}

// A start redelivered after its workflow closed is refused by the reuse
// policy. The workflow it asked for did start, so the start succeeds: an error
// would reschedule the queued intent forever.
func TestStartingAWorkflowThatAlreadyRanSucceeds(t *testing.T) {
	t.Parallel()

	alreadyStarted := serviceerror.NewWorkflowExecutionAlreadyStarted("workflow execution already started", "", "")
	dispatcher := NewDispatcher(&recordingWorkflowClient{executeErr: alreadyStarted}, DispatcherOptions{})
	ctx := context.Background()
	run := domain.TemplateRunWorkflowInput{RunID: "run_123", TenantID: "tenant_123"}

	for name, start := range map[string]func() error{
		"plan":  func() error { return dispatcher.StartTemplatePlan(ctx, run) },
		"apply": func() error { return dispatcher.StartTemplateApply(ctx, run) },
		"sync": func() error {
			return dispatcher.StartTemplateSync(ctx, domain.TemplateSyncWorkflowInput{SyncID: "template_sync_123", TenantID: "tenant_123"})
		},
	} {
		if err := start(); err != nil {
			t.Errorf("%s: error = %v, want nil for a workflow already started", name, err)
		}
	}
}

// The apply workflow starts on the control queue under an ID derived from the
// run's, and a redelivered start finds it running rather than starting twice.
func TestStartTemplateApplyStartsTheApplyWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := NewDispatcher(workflowClient, DispatcherOptions{TerraformTimeout: 20 * time.Minute})
	input := domain.TemplateRunWorkflowInput{
		RunID:     domain.TemplateRunID("run_123"),
		TenantID:  domain.TenantID("tenant_123"),
		Operation: domain.OperationPlan,
	}

	if err := dispatcher.StartTemplateApply(context.Background(), input); err != nil {
		t.Fatalf("StartTemplateApply returned error: %v", err)
	}

	if workflowClient.executeOptions.ID != "template-run/tenant_123/run_123/apply" {
		t.Fatalf("workflow ID = %q", workflowClient.executeOptions.ID)
	}
	if workflowClient.executeOptions.TaskQueue != domain.ControlTaskQueue {
		t.Fatalf("task queue = %q", workflowClient.executeOptions.TaskQueue)
	}
	if workflowClient.executeOptions.WorkflowIDConflictPolicy != enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING {
		t.Fatalf("conflict policy = %v, want use existing", workflowClient.executeOptions.WorkflowIDConflictPolicy)
	}
	if workflowClient.executeWorkflow != domain.TemplateApplyWorkflowName {
		t.Fatalf("workflow = %v", workflowClient.executeWorkflow)
	}
	started := workflowClient.executeArgs[0].(domain.TemplateRunWorkflowInput)
	if started.TerraformTimeout != 20*time.Minute {
		t.Fatalf("terraform timeout = %v, want the deployment's", started.TerraformTimeout)
	}
}

func TestStartTemplateRunWrapsClientError(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("temporal unavailable")
	dispatcher := NewDispatcher(&recordingWorkflowClient{executeErr: clientErr}, DispatcherOptions{})

	err := dispatcher.StartTemplatePlan(context.Background(), domain.TemplateRunWorkflowInput{
		RunID:    domain.TemplateRunID("run_123"),
		TenantID: domain.TenantID("tenant_123"),
	})
	if !errors.Is(err, clientErr) {
		t.Fatalf("error = %v, want wrapped client error", err)
	}
	if !strings.Contains(err.Error(), "start template run workflow") {
		t.Fatalf("error = %q, want start context", err.Error())
	}
}

func TestStartTemplateSyncWrapsClientError(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("temporal unavailable")
	dispatcher := NewDispatcher(&recordingWorkflowClient{executeErr: clientErr}, DispatcherOptions{})

	err := dispatcher.StartTemplateSync(context.Background(), domain.TemplateSyncWorkflowInput{
		SyncID:   domain.TemplateSyncID("template_sync_123"),
		TenantID: domain.TenantID("tenant_123"),
	})
	if !errors.Is(err, clientErr) {
		t.Fatalf("error = %v, want wrapped client error", err)
	}
	if !strings.Contains(err.Error(), "start template sync workflow") {
		t.Fatalf("error = %q, want start context", err.Error())
	}
}

func TestTemplatePlanWorkflowID(t *testing.T) {
	t.Parallel()

	got := templateRunWorkflowID(domain.TenantID("tenant_123"), domain.TemplateRunID("run_123"))
	if got != "template-run/tenant_123/run_123" {
		t.Fatalf("workflow ID = %q", got)
	}
}

func TestTemplateSyncWorkflowID(t *testing.T) {
	t.Parallel()

	got := templateSyncWorkflowID(domain.TenantID("tenant_123"), domain.TemplateSyncID("template_sync_123"))
	if got != "template-sync/tenant_123/template_sync_123" {
		t.Fatalf("workflow ID = %q", got)
	}
}

type recordingWorkflowClient struct {
	executeOptions  client.StartWorkflowOptions
	executeWorkflow any
	executeArgs     []any
	executeErr      error
}

func (workflowClient *recordingWorkflowClient) ExecuteWorkflow(
	_ context.Context,
	options client.StartWorkflowOptions,
	workflow any,
	args ...any,
) (client.WorkflowRun, error) {
	workflowClient.executeOptions = options
	workflowClient.executeWorkflow = workflow
	workflowClient.executeArgs = args
	return nil, workflowClient.executeErr
}
