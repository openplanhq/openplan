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
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

var _ app.WorkflowDispatcher = (*Dispatcher)(nil)

func TestStartTemplateRunExecutesWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := newDispatcher(workflowClient)
	input := domain.TemplateRunWorkflowInput{
		RunID:           domain.TemplateRunID("run_123"),
		TenantID:        domain.TenantID("tenant_123"),
		StackTemplateID: domain.StackTemplateID("stack_template_123"),
		Operation:       domain.OperationPlan,
		SelectedRef:     "main",
		WorkspaceName:   "mtp_acme_prod_vpc_a13f9c",
	}

	if err := dispatcher.StartTemplateRun(context.Background(), input); err != nil {
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
	dispatcher := newDispatcher(workflowClient, DispatcherOptions{TerraformTimeout: 90 * time.Minute})

	if err := dispatcher.StartTemplateRun(context.Background(), domain.TemplateRunWorkflowInput{
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
	dispatcher := newDispatcher(workflowClient)

	if err := dispatcher.StartTemplateRun(context.Background(), domain.TemplateRunWorkflowInput{
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
	dispatcher := newDispatcher(workflowClient)
	input := domain.TemplateSyncWorkflowInput{
		RegistrationID: domain.TemplateRegistrationID("template_registration_123"),
		TenantID:       domain.TenantID("tenant_123"),
		RepoOwner:      "acme",
		RepoName:       "infra-templates",
		SourceRef:      "v0.0.1",
		RootPath:       "modules/vpc",
	}

	if err := dispatcher.StartTemplateSync(context.Background(), input); err != nil {
		t.Fatalf("StartTemplateSync returned error: %v", err)
	}

	if workflowClient.executeOptions.ID != "template-sync/tenant_123/template_registration_123" {
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
}

// The apply workflow starts on the control queue under an ID derived from the
// run's, and a redelivered start finds it running rather than starting twice.
func TestStartTemplateApplyStartsTheApplyWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{}
	dispatcher := newDispatcher(workflowClient, DispatcherOptions{TerraformTimeout: 20 * time.Minute})
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
	dispatcher := newDispatcher(&recordingWorkflowClient{executeErr: clientErr})

	err := dispatcher.StartTemplateRun(context.Background(), domain.TemplateRunWorkflowInput{
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
	dispatcher := newDispatcher(&recordingWorkflowClient{executeErr: clientErr})

	err := dispatcher.StartTemplateSync(context.Background(), domain.TemplateSyncWorkflowInput{
		RegistrationID: domain.TemplateRegistrationID("template_registration_123"),
		TenantID:       domain.TenantID("tenant_123"),
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

	got := templateSyncWorkflowID(domain.TenantID("tenant_123"), domain.TemplateRegistrationID("template_registration_123"))
	if got != "template-sync/tenant_123/template_registration_123" {
		t.Fatalf("workflow ID = %q", got)
	}
}

// A run's two workflow ids are both asked about, in plan then apply order,
// and each answer is read on its own: a plan that closed and an apply Temporal
// never had is the shape of a restore that lost the apply.
func TestDescribeTemplateRunWorkflowsReadsEachWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{described: map[string]describeAnswer{
		"template-run/tenant_123/run_123":       {status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED},
		"template-run/tenant_123/run_123/apply": {err: serviceerror.NewNotFound("no such workflow")},
	}}
	dispatcher := newDispatcher(workflowClient)

	got, err := dispatcher.DescribeTemplateRunWorkflows(context.Background(), "tenant_123", "run_123")
	if err != nil {
		t.Fatalf("DescribeTemplateRunWorkflows returned error: %v", err)
	}
	want := []domain.TemplateRunWorkflow{
		{Phase: domain.RunPhasePlan, WorkflowID: "template-run/tenant_123/run_123", State: domain.TemplateRunWorkflowClosed, CloseStatus: "completed"},
		{Phase: domain.RunPhaseApply, WorkflowID: "template-run/tenant_123/run_123/apply", State: domain.TemplateRunWorkflowAbsent},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workflows = %#v, want %#v", got, want)
	}
	if workflowClient.describedRunIDs["template-run/tenant_123/run_123"] != "" {
		t.Fatalf("run id = %q, want empty so the latest run is described", workflowClient.describedRunIDs["template-run/tenant_123/run_123"])
	}
}

func TestDescribeTemplateRunWorkflowsReportsARunningWorkflow(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{described: map[string]describeAnswer{
		"template-run/tenant_123/run_123":       {err: serviceerror.NewNotFound("gone")},
		"template-run/tenant_123/run_123/apply": {status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
	}}

	got, err := newDispatcher(workflowClient).DescribeTemplateRunWorkflows(context.Background(), "tenant_123", "run_123")
	if err != nil {
		t.Fatalf("DescribeTemplateRunWorkflows returned error: %v", err)
	}
	if got[1].State != domain.TemplateRunWorkflowRunning || got[1].CloseStatus != "" {
		t.Fatalf("apply workflow = %#v, want running with no close status", got[1])
	}
}

// Timed out is the enum name with its prefix gone and lowercased, so the
// word Temporal's own UI shows is the word the run's summary will carry.
func TestDescribeTemplateRunWorkflowsNamesTheCloseStatus(t *testing.T) {
	t.Parallel()

	workflowClient := &recordingWorkflowClient{described: map[string]describeAnswer{
		"template-run/tenant_123/run_123":       {status: enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT},
		"template-run/tenant_123/run_123/apply": {status: enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED},
	}}

	got, err := newDispatcher(workflowClient).DescribeTemplateRunWorkflows(context.Background(), "tenant_123", "run_123")
	if err != nil {
		t.Fatalf("DescribeTemplateRunWorkflows returned error: %v", err)
	}
	if got[0].CloseStatus != "timed_out" || got[1].CloseStatus != "terminated" {
		t.Fatalf("close statuses = %q, %q; want timed_out, terminated", got[0].CloseStatus, got[1].CloseStatus)
	}
}

// Only not-found reads as absent. Anything else is Temporal failing to
// answer, and an operator must not end a run on the strength of that.
func TestDescribeTemplateRunWorkflowsSurfacesOtherErrors(t *testing.T) {
	t.Parallel()

	clientErr := errors.New("temporal unavailable")
	workflowClient := &recordingWorkflowClient{described: map[string]describeAnswer{
		"template-run/tenant_123/run_123": {err: clientErr},
	}}

	got, err := newDispatcher(workflowClient).DescribeTemplateRunWorkflows(context.Background(), "tenant_123", "run_123")
	if !errors.Is(err, clientErr) {
		t.Fatalf("error = %v, want wrapped client error", err)
	}
	if got != nil {
		t.Fatalf("workflows = %#v, want none on error", got)
	}
}

type describeAnswer struct {
	status enumspb.WorkflowExecutionStatus
	err    error
}

type recordingWorkflowClient struct {
	executeOptions  client.StartWorkflowOptions
	executeWorkflow any
	executeArgs     []any
	executeErr      error
	// described answers DescribeWorkflowExecution by workflow id; an id with
	// no answer is not found. describedRunIDs records the run id asked with.
	described       map[string]describeAnswer
	describedRunIDs map[string]string
}

func (workflowClient *recordingWorkflowClient) DescribeWorkflowExecution(_ context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	if workflowClient.describedRunIDs == nil {
		workflowClient.describedRunIDs = map[string]string{}
	}
	workflowClient.describedRunIDs[workflowID] = runID
	answer, ok := workflowClient.described[workflowID]
	if !ok {
		return nil, serviceerror.NewNotFound("workflow not found")
	}
	if answer.err != nil {
		return nil, answer.err
	}
	return &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: answer.status},
	}, nil
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
