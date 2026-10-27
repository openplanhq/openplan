package workflows

import (
	"fmt"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// syncRetryPolicy is the retry policy for template-sync activities. Git clones
// and HCL parsing can fail due to transient network errors or GitHub rate
// limits, but invalid configuration or missing repositories are permanent
// failures that should not be retried.
var syncRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    30 * time.Second,
	BackoffCoefficient: 2.0,
	MaximumInterval:    5 * time.Minute,
	MaximumAttempts:    4,
	NonRetryableErrorTypes: []string{
		"InvalidTemplate",
		"RepositoryNotFound",
	},
}

// TemplateSyncWorkflow syncs a template: it clones and parses the template as
// the sync's one step, and its status is lifecycle's.
func TemplateSyncWorkflow(ctx workflow.Context, input domain.TemplateSyncWorkflowInput) error {
	r := newTemplateSync(ctx, input)
	return r.lifecycle(r.sync)
}

// templateSync is one template sync, as the workflow carries it out. It
// holds the control-queue context and the sync's input, and it is the sync's
// Recorder. Its status is written only by lifecycle.
//
// A sync has no executor session: it clones and parses a repository but runs
// none of its code, so all of its work, the step included, schedules on the
// control plane, on r.ctx.
type templateSync struct {
	ctx   workflow.Context
	input domain.TemplateSyncWorkflowInput
}

// newTemplateSync sets the options for every activity the sync schedules.
func newTemplateSync(ctx workflow.Context, input domain.TemplateSyncWorkflowInput) *templateSync {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           domain.ControlTaskQueue,
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         syncRetryPolicy,
	})
	return &templateSync{ctx: ctx, input: input}
}

// sync clones the sync's repository at its ref and parses the
// template it holds, as the sync's one step.
func (r *templateSync) sync() (domain.TemplateSyncActivityOutput, error) {
	var synced domain.TemplateSyncActivityOutput
	err := ExecuteStep(r.ctx, r, domain.TemplateSyncStepSyncing, domain.SyncTemplateActivityName, domain.TemplateSyncActivityInput{
		SyncID:     r.input.SyncID,
		TenantID:   r.input.TenantID,
		TemplateID: r.input.TemplateID,
		RepoOwner:  r.input.RepoOwner,
		RepoName:   r.input.RepoName,
		SourceRef:  r.input.SourceRef,
		RootPath:   r.input.RootPath,
	}).Get(r.ctx, &synced)
	return synced, err
}

// Record records the step the sync is starting.
func (r *templateSync) Record(step domain.TemplateSyncStep) error {
	return workflow.ExecuteActivity(
		r.ctx,
		domain.RecordTemplateSyncStepActivityName,
		domain.TemplateSyncStepActivityInput{
			SyncID:   r.input.SyncID,
			TenantID: r.input.TenantID,
			Step:     step,
		},
	).Get(r.ctx, nil)
}

// lifecycle carries a sync through its status, and is the only code
// that writes it. It marks the sync running, runs work, and records
// what work found: the revision it produced, or why the template is invalid. A
// sync that reports no status of its own completed.
//
// Any error, including one writing that outcome, marks the sync failed
// before lifecycle returns it. If recording the failure also fails, both
// errors are surfaced: the original wrapped with %w to stay matchable by
// callers, the persistence error appended as context.
func (r *templateSync) lifecycle(work func() (domain.TemplateSyncActivityOutput, error)) (err error) {
	setStatus := func(status domain.TemplateSyncStatusActivityInput) error {
		status.SyncID = r.input.SyncID
		status.TenantID = r.input.TenantID
		return workflow.ExecuteActivity(
			r.ctx,
			domain.RecordTemplateSyncStatusActivityName,
			status,
		).Get(r.ctx, nil)
	}
	defer func() {
		if err == nil {
			return
		}
		if recordErr := setStatus(domain.TemplateSyncStatusActivityInput{
			Status:       domain.TemplateSyncStateFailed,
			ErrorSummary: err.Error(),
		}); recordErr != nil {
			err = fmt.Errorf("%w (also failed to persist failure status: %w)", err, recordErr)
		}
	}()

	if err := setStatus(domain.TemplateSyncStatusActivityInput{
		Status: domain.TemplateSyncStateRunning,
	}); err != nil {
		return err
	}
	synced, err := work()
	if err != nil {
		return err
	}
	status := synced.Status
	if status == "" {
		status = domain.TemplateSyncStateCompleted
	}
	return setStatus(domain.TemplateSyncStatusActivityInput{
		Status:             status,
		TemplateRevisionID: synced.TemplateRevisionID,
		ResolvedCommitSHA:  synced.ResolvedCommitSHA,
		ErrorSummary:       synced.ErrorSummary,
	})
}
