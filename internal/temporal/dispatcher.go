package temporal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

type workflowClient interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (client.WorkflowRun, error)
	// DescribeWorkflowExecution with an empty run ID describes the latest run
	// of the workflow ID.
	DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
}

type Dispatcher struct {
	client  workflowClient
	options DispatcherOptions
}

// DispatcherOptions carries the deployment-wide policy applied when a workflow
// is started, as opposed to what the caller asked to run.
type DispatcherOptions struct {
	// TerraformTimeout bounds each Terraform command the run issues. Zero
	// leaves the workflow's own default in place.
	TerraformTimeout time.Duration
}

func NewDispatcher(temporalClient client.Client, options DispatcherOptions) *Dispatcher {
	return newDispatcher(temporalClient, options)
}

func newDispatcher(temporalClient workflowClient, options ...DispatcherOptions) *Dispatcher {
	dispatcher := &Dispatcher{client: temporalClient}
	if len(options) > 0 {
		dispatcher.options = options[0]
	}
	return dispatcher
}

// StartTemplateRun dispatches one TemplatePlanWorkflow execution to Temporal.
// The workflow ID is derived from tenant ID and run ID so repeated callers target
// the same logical run. Workflows run on the control queue; the workflow itself
// routes execution activities to the executor.
func (dispatcher *Dispatcher) StartTemplateRun(ctx context.Context, input domain.TemplateRunWorkflowInput) error {
	// The timeout is stamped here rather than carried from the request: it is
	// deployment configuration, not something a caller chose, and stamping it
	// at dispatch means a run that sat in the queue across a reconfiguration
	// starts with the timeout in force now. Once started, the run keeps this
	// value for its whole life, because it is in the workflow's input.
	if input.TerraformTimeout <= 0 {
		input.TerraformTimeout = dispatcher.options.TerraformTimeout
	}
	_, err := dispatcher.client.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                       templateRunWorkflowID(input.TenantID, input.RunID),
			TaskQueue:                domain.ControlTaskQueue,
			WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		domain.TemplatePlanWorkflowName,
		input)
	if err != nil {
		return fmt.Errorf("start template run workflow: %w", err)
	}

	return nil
}

// StartTemplateSync dispatches one TemplateSyncWorkflow execution to Temporal.
func (dispatcher *Dispatcher) StartTemplateSync(ctx context.Context, input domain.TemplateSyncWorkflowInput) error {
	_, err := dispatcher.client.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:        templateSyncWorkflowID(input.TenantID, input.RegistrationID),
			TaskQueue: domain.ControlTaskQueue,
		},
		domain.TemplateSyncWorkflowName,
		input)
	if err != nil {
		return fmt.Errorf("start template sync workflow: %w", err)
	}

	return nil
}

// StartTemplateApply starts the workflow that applies an approved run's saved
// plan. Its ID extends the run's, so a redelivered start intent finds the
// workflow already running instead of starting a second apply.
func (dispatcher *Dispatcher) StartTemplateApply(ctx context.Context, input domain.TemplateRunWorkflowInput) error {
	if input.TerraformTimeout <= 0 {
		input.TerraformTimeout = dispatcher.options.TerraformTimeout
	}
	_, err := dispatcher.client.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                       templateApplyWorkflowID(input.TenantID, input.RunID),
			TaskQueue:                domain.ControlTaskQueue,
			WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		domain.TemplateApplyWorkflowName,
		input)
	if err != nil {
		return fmt.Errorf("start template apply workflow: %w", err)
	}

	return nil
}

// DescribeTemplateRunWorkflows reports what Temporal holds for both workflows
// that can carry a run, its plan and its apply, in that order. A workflow
// Temporal has never heard of, or has lost, reads as absent rather than as an
// error: that absence is the fact an operator ending a stuck run acts on. Any
// other failure to ask is returned, so a caller never mistakes an unreachable
// Temporal for a gone workflow.
func (dispatcher *Dispatcher) DescribeTemplateRunWorkflows(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflow, error) {
	workflows := []domain.TemplateRunWorkflow{
		{Phase: domain.RunPhasePlan, WorkflowID: templateRunWorkflowID(tenantID, runID)},
		{Phase: domain.RunPhaseApply, WorkflowID: templateApplyWorkflowID(tenantID, runID)},
	}
	for i := range workflows {
		described, err := dispatcher.client.DescribeWorkflowExecution(ctx, workflows[i].WorkflowID, "")
		var notFound *serviceerror.NotFound
		switch {
		case errors.As(err, &notFound):
			workflows[i].State = domain.TemplateRunWorkflowAbsent
			continue
		case err != nil:
			return nil, fmt.Errorf("describe workflow %s: %w", workflows[i].WorkflowID, err)
		}
		status := described.GetWorkflowExecutionInfo().GetStatus()
		if status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			workflows[i].State = domain.TemplateRunWorkflowRunning
			continue
		}
		workflows[i].State = domain.TemplateRunWorkflowClosed
		workflows[i].CloseStatus = workflowCloseStatus(status)
	}
	return workflows, nil
}

// workflowCloseStatus names how a closed workflow ended, in the words
// Temporal's own UI uses. Spelled out rather than derived from the enum's
// String, which is CamelCase ("TimedOut") and would lose the word break.
func workflowCloseStatus(status enumspb.WorkflowExecutionStatus) string {
	switch status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "continued_as_new"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "timed_out"
	default:
		return strings.ToLower(status.String())
	}
}

func templateRunWorkflowID(tenantID domain.TenantID, runID domain.TemplateRunID) string {
	return fmt.Sprintf("template-run/%s/%s", tenantID, runID)
}

func templateApplyWorkflowID(tenantID domain.TenantID, runID domain.TemplateRunID) string {
	return templateRunWorkflowID(tenantID, runID) + "/apply"
}

func templateSyncWorkflowID(tenantID domain.TenantID, registrationID domain.TemplateRegistrationID) string {
	return fmt.Sprintf("template-sync/%s/%s", tenantID, registrationID)
}
