package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/vishu42/openplan/internal/artifacts"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
	"github.com/vishu42/openplan/internal/runseal"
)

// ControlStore is everything the control activities read and write.
type ControlStore interface {
	StatusRecorder
	LogMetadataRecorder
	CredentialReader
	CredentialDecryptor
	PlanKeyStore
	PlanRecorder
}

// PlanKeyStore holds the key each run's saved plan is encrypted with.
type PlanKeyStore interface {
	// CreatePlanKey returns the run's plan key, making one if it has none.
	CreatePlanKey(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error)
	// PlanKey returns the run's plan key, failing if it has none: once a run
	// is terminal its key is gone, and nothing can open its saved plan.
	PlanKey(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error)
}

// PlanRecorder records the two steps between a run's plan and its apply.
type PlanRecorder interface {
	// FinishTemplatePlan records a finished plan and decides what follows it.
	// changes is the plan's resource changes; nil for a plan with changes
	// means they could not be listed.
	FinishTemplatePlan(ctx context.Context, input domain.FinishPlanActivityInput, changes *domain.PlanChanges) (domain.PlanOutcome, error)
	// BeginTemplateApply claims a run for its apply phase by moving it to
	// running, and starts its apply execution as workflowID: an approved run,
	// or with autoApprove a queued one. It reports true for a run it already
	// claimed, so a retried claim is idempotent, and false when the run is no
	// longer in a state this claim can take.
	BeginTemplateApply(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, autoApprove bool, workflowID string) (bool, error)
}

// LogMetadataRecorder persists the metadata row for an uploaded phase log.
type LogMetadataRecorder interface {
	RecordTemplateRunLog(ctx context.Context, log domain.TemplateRunLog) error
}

// PlanChangesReader reads the resource changes a run's executor uploaded.
type PlanChangesReader interface {
	// GetPlanChanges fails wrapping os.ErrNotExist when none were uploaded.
	GetPlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error)
}

// ControlActivities are the template-run activities that belong to the control
// plane. They write product state or handle secrets, so they run in the API
// process on the control queue, never next to tenant Terraform.
type ControlActivities struct {
	store   ControlStore
	tokens  GitHubTokenSource
	live    logsink.Stream
	changes PlanChangesReader
}

// WithPlanChanges reads each finished plan's resource changes from reader,
// to record them with the plan. Without it, a plan's changes are recorded as
// unavailable.
func (activities *ControlActivities) WithPlanChanges(reader PlanChangesReader) *ControlActivities {
	activities.changes = reader
	return activities
}

// NewControlActivities wires the control activities. tokens may be nil, in
// which case sources are fetched unauthenticated. live is where a finished
// log's end entry goes; nil skips it, which only tests that don't watch the
// stream should do.
func NewControlActivities(store ControlStore, tokens GitHubTokenSource, live logsink.Stream) *ControlActivities {
	return &ControlActivities{store: store, tokens: tokens, live: live}
}

// RecordTemplateRunStatus records one workflow status transition.
//
// Workflows call this as an activity because database writes are side effects
// and cannot run inside deterministic workflow code.
func (activities *ControlActivities) RecordTemplateRunStatus(ctx context.Context, input domain.TemplateRunStatusActivityInput) error {
	if err := activities.store.RecordTemplateRunStatus(ctx, input); err != nil {
		return fmt.Errorf("record template run status: %w", err)
	}
	return nil
}

// RecordTemplateRunStep records the step a running run has started.
func (activities *ControlActivities) RecordTemplateRunStep(ctx context.Context, input domain.TemplateRunStepActivityInput) error {
	if err := activities.store.RecordTemplateRunStep(ctx, input); err != nil {
		return fmt.Errorf("record template run step: %w", err)
	}
	return nil
}

// RecordTemplateRunEvent records something a running run did to its stack
// template: an apply that is now live, or a destroy starting or done.
func (activities *ControlActivities) RecordTemplateRunEvent(ctx context.Context, input domain.TemplateRunEventActivityInput) error {
	if err := activities.store.RecordTemplateRunEvent(ctx, input); err != nil {
		return fmt.Errorf("record template run event: %w", err)
	}
	return nil
}

// RecordTemplateRunLog records a log's row in its current state. The row is
// keyed by run, phase and number, so a retry, or the same log moving from live
// to complete or lost, upserts. A complete or lost log then gets the end entry
// that tells its live viewers to stop.
//
// The object key is derived here from the run and the name rather than taken
// from the executor's report: the key is what the API later reads, and an
// executor that could choose it could point this run's log at any object in
// the store, another tenant's included.
func (activities *ControlActivities) RecordTemplateRunLog(ctx context.Context, log domain.TemplateRunLog) error {
	if !log.Command.Valid() {
		return fmt.Errorf("log has unknown command %q", log.Command)
	}
	if err := validateLogState(log); err != nil {
		return err
	}
	// The name is derived from the phase and number, as the database derives
	// its own copy, so the key below is the one the row will be read by.
	name, err := domain.RunLogName(log.RunPhase, log.Seq)
	if err != nil {
		return err
	}
	log.Phase = name
	key, err := artifacts.LogKey(log.TenantID, log.RunID, name)
	if err != nil {
		return err
	}
	log.ObjectKey = key
	if err := activities.store.RecordTemplateRunLog(ctx, log); err != nil {
		return fmt.Errorf("record template run log metadata: %w", err)
	}
	// Only after the row: a viewer told the log ended reads the row next, so
	// the row must already say so. A live log's stream is still the
	// executor's to write.
	if log.State == domain.TemplateRunLogLive || activities.live == nil {
		return nil
	}
	if err := logsink.AppendEnd(ctx, activities.live, logsink.StreamKey(log.RunID, name), log.State); err != nil {
		return fmt.Errorf("end live log: %w", err)
	}
	return nil
}

// validateLogState refuses a row that would contradict its own state. The
// store persists what it is given, so a complete log with no upload would
// read back as one anyone can open, at an empty content type from year 1.
func validateLogState(log domain.TemplateRunLog) error {
	if !log.State.Valid() {
		return fmt.Errorf("log has unknown state %q", log.State)
	}
	switch log.State {
	case domain.TemplateRunLogComplete:
		if log.UploadedAt.IsZero() || log.ContentType == "" {
			return fmt.Errorf("complete log %s/%d has no upload time or content type", log.RunPhase, log.Seq)
		}
	case domain.TemplateRunLogLost:
		if log.LostReason == "" {
			return fmt.Errorf("lost log %s/%d has no reason", log.RunPhase, log.Seq)
		}
	case domain.TemplateRunLogLive:
		// A live log has no upload yet and no reason to be lost.
	}
	return nil
}

// SealRunCredentials decrypts a StackTemplate's credentials and seals them to
// the run's key, so they cross Temporal as ciphertext only the executor
// holding that run can open.
func (activities *ControlActivities) SealRunCredentials(ctx context.Context, input domain.SealRunCredentialsActivityInput) (domain.SealRunCredentialsActivityOutput, error) {
	environment, err := resolveCredentialEnvironment(ctx, activities.store, activities.store, input.TenantID, input.StackTemplateID)
	if err != nil {
		return domain.SealRunCredentialsActivityOutput{}, fmt.Errorf("resolve terraform credentials: %w", err)
	}
	sealed, err := runseal.Seal(input.PublicKey, environment)
	if err != nil {
		return domain.SealRunCredentialsActivityOutput{}, fmt.Errorf("seal terraform credentials: %w", err)
	}
	return domain.SealRunCredentialsActivityOutput{SealedEnvironment: sealed}, nil
}

// SealSourceToken resolves an installation token for a repository and seals
// it to the run's key.
//
// Resolving is best effort, as it was when the executor did it: a public
// repository needs no token, and a missing installation is indistinguishable
// from one. So a failed lookup seals the empty token and returns the hint the
// fetch appends if it then fails unauthenticated.
func (activities *ControlActivities) SealSourceToken(ctx context.Context, input domain.SealSourceTokenActivityInput) (domain.SealSourceTokenActivityOutput, error) {
	var token string
	var hint string
	if activities.tokens != nil {
		resolved, err := activities.tokens.Token(ctx, input.RepoOwner, input.RepoName)
		if err != nil {
			hint = unauthenticatedFetchHint(err, input.RepoOwner, input.RepoName)
		} else {
			token = resolved
		}
	}
	sealed, err := runseal.Seal(input.PublicKey, token)
	if err != nil {
		return domain.SealSourceTokenActivityOutput{}, fmt.Errorf("seal source token: %w", err)
	}
	return domain.SealSourceTokenActivityOutput{SealedToken: sealed, FetchHint: hint}, nil
}

// SealPlanKey seals the key a run's saved plan is encrypted with to the run's
// key on the executor now holding it. The plan phase creates the key; the
// apply phase, usually on another executor, gets the same key sealed to its
// own run key.
func (activities *ControlActivities) SealPlanKey(ctx context.Context, input domain.SealPlanKeyActivityInput) (domain.SealPlanKeyActivityOutput, error) {
	var key []byte
	var err error
	if input.Create {
		key, err = activities.store.CreatePlanKey(ctx, input.TenantID, input.RunID)
	} else {
		key, err = activities.store.PlanKey(ctx, input.TenantID, input.RunID)
	}
	if err != nil {
		return domain.SealPlanKeyActivityOutput{}, fmt.Errorf("plan key: %w", err)
	}
	sealed, err := runseal.Seal(input.PublicKey, key)
	if err != nil {
		return domain.SealPlanKeyActivityOutput{}, fmt.Errorf("seal plan key: %w", err)
	}
	return domain.SealPlanKeyActivityOutput{SealedPlanKey: sealed}, nil
}

// FinishPlan records a finished plan and reports what follows: completion, a
// wait for approval, or, for an auto-approved run, its apply.
func (activities *ControlActivities) FinishPlan(ctx context.Context, input domain.FinishPlanActivityInput) (domain.PlanOutcome, error) {
	changes, err := activities.planChanges(ctx, input)
	if err != nil {
		return "", fmt.Errorf("finish plan: %w", err)
	}
	outcome, err := activities.store.FinishTemplatePlan(ctx, input, changes)
	if err != nil {
		return "", fmt.Errorf("finish plan: %w", err)
	}
	return outcome, nil
}

// planChanges reads the resource changes the run's executor uploaded for its
// plan. They come from the sandbox that ran the template's code, so they are
// checked again here: anything missing, unreadable or over the limits is
// recorded as unavailable rather than failing the plan, since the plan log
// still has every change. Only a store that can't be reached is an error, so
// the activity retries it.
func (activities *ControlActivities) planChanges(ctx context.Context, input domain.FinishPlanActivityInput) (*domain.PlanChanges, error) {
	if !input.HasChanges || activities.changes == nil {
		return nil, nil
	}
	body, err := activities.changes.GetPlanChanges(ctx, input.TenantID, input.RunID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Twice the parser's own budget leaves room for JSON's escaping and
	// still refuses a file no real plan's executor would write.
	if len(body) > 2*domain.MaxPlanChangesBytes {
		return nil, nil
	}
	return checkedPlanChanges(body), nil
}

// checkedPlanChanges decodes uploaded changes and checks them against the
// limits, in reading order, or nil when they can't be trusted.
func checkedPlanChanges(body []byte) *domain.PlanChanges {
	var changes domain.PlanChanges
	if json.Unmarshal(body, &changes) != nil || changes.Validate() != nil {
		return nil
	}
	domain.SortPlanResourceChanges(changes.Changes)
	return &changes
}

// BeginApply claims a run for its apply phase.
func (activities *ControlActivities) BeginApply(ctx context.Context, input domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
	claimed, err := activities.store.BeginTemplateApply(ctx, input.TenantID, input.RunID, input.AutoApprove, input.WorkflowID)
	if err != nil {
		return domain.BeginApplyActivityOutput{}, fmt.Errorf("begin apply: %w", err)
	}
	return domain.BeginApplyActivityOutput{Claimed: claimed}, nil
}
