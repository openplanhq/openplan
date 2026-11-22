package temporal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// WorkflowClient is the part of a Temporal client the dispatcher uses;
// client.Client satisfies it, and tests pass a recording fake.
type WorkflowClient interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, any, ...any) (client.WorkflowRun, error)
}

type Dispatcher struct {
	client  WorkflowClient
	options DispatcherOptions
}

// DispatcherOptions carries the deployment-wide policy applied when a workflow
// is started, as opposed to what the caller asked to run.
type DispatcherOptions struct {
	// TerraformTimeout bounds each Terraform command the run issues. Zero
	// leaves the workflow's own default in place.
	TerraformTimeout time.Duration
}

func NewDispatcher(temporalClient WorkflowClient, options DispatcherOptions) *Dispatcher {
	return &Dispatcher{client: temporalClient, options: options}
}

// StartTemplatePlan dispatches one TemplatePlanWorkflow execution to Temporal.
// The workflow ID is derived from tenant ID and run ID so repeated callers target
// the same logical run. Workflows run on the control queue; the workflow itself
// routes execution activities to the executor.
func (dispatcher *Dispatcher) StartTemplatePlan(ctx context.Context, input domain.TemplateRunWorkflowInput) error {
	if err := dispatcher.startRunWorkflow(ctx, templateRunWorkflowID(input.TenantID, input.RunID), domain.TemplatePlanWorkflowName, input); err != nil {
		return fmt.Errorf("start template run workflow: %w", err)
	}
	return nil
}

// StartTemplateSync dispatches one TemplateSyncWorkflow execution to Temporal.
func (dispatcher *Dispatcher) StartTemplateSync(ctx context.Context, input domain.TemplateSyncWorkflowInput) error {
	if err := dispatcher.start(ctx, templateSyncWorkflowID(input.TenantID, input.SyncID), domain.TemplateSyncWorkflowName, input); err != nil {
		return fmt.Errorf("start template sync workflow: %w", err)
	}
	return nil
}

// StartTemplateApply starts the workflow that applies an approved run's saved
// plan. Its ID extends the run's, so a redelivered start intent finds the
// workflow already running instead of starting a second apply.
func (dispatcher *Dispatcher) StartTemplateApply(ctx context.Context, input domain.TemplateRunWorkflowInput) error {
	if err := dispatcher.startRunWorkflow(ctx, templateApplyWorkflowID(input.TenantID, input.RunID), domain.TemplateApplyWorkflowName, input); err != nil {
		return fmt.Errorf("start template apply workflow: %w", err)
	}
	return nil
}

// startRunWorkflow starts one of a run's workflows under workflowID, or finds
// it already started.
func (dispatcher *Dispatcher) startRunWorkflow(ctx context.Context, workflowID string, workflowName string, input domain.TemplateRunWorkflowInput) error {
	// The timeout is stamped here rather than carried from the request: it is
	// deployment configuration, not something a caller chose, and stamping it
	// at dispatch means a run that sat in the queue across a reconfiguration
	// starts with the timeout in force now. Once started, the run keeps this
	// value for its whole life, because it is in the workflow's input.
	if input.TerraformTimeout <= 0 {
		input.TerraformTimeout = dispatcher.options.TerraformTimeout
	}
	return dispatcher.start(ctx, workflowID, workflowName, input)
}

// start starts workflowName under workflowID at most once. The queue delivers
// a start at least once, so a redelivery finds the workflow running, and one
// after it closed is refused by the reuse policy rather than starting it again.
// Either way the workflow asked for did start, so both succeed: an error would
// reschedule the queued intent forever.
func (dispatcher *Dispatcher) start(ctx context.Context, workflowID string, workflowName string, input any) error {
	_, err := dispatcher.client.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{
			ID:                       workflowID,
			TaskQueue:                domain.ControlTaskQueue,
			WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		},
		workflowName,
		input)
	if _, ok := errors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil
	}
	return err
}

func templateRunWorkflowID(tenantID domain.TenantID, runID domain.TemplateRunID) string {
	return fmt.Sprintf("template-run/%s/%s", tenantID, runID)
}

func templateApplyWorkflowID(tenantID domain.TenantID, runID domain.TemplateRunID) string {
	return templateRunWorkflowID(tenantID, runID) + "/apply"
}

func templateSyncWorkflowID(tenantID domain.TenantID, syncID domain.TemplateSyncID) string {
	return fmt.Sprintf("template-sync/%s/%s", tenantID, syncID)
}
