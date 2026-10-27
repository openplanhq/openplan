package workflows

import (
	"errors"
	"fmt"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// A template run is two workflows: TemplatePlanWorkflow and
// TemplateApplyWorkflow. Each is carried by a type of its own, planWorkflow and
// applyWorkflow, whose lifecycle alone moves the run along its status, and
// which does its work in an executor session, as the run's steps.
//
// Both embed run, which holds the control plane and everything the two share,
// and hands the executor session to the methods that work in it.

const (
	// sessionCreationTimeout is how long a plan or an apply waits for a free
	// executor before it fails. An executor runs few sessions (one, in the Helm
	// chart's defaults), so finding them all busy is the normal case, not a
	// fault: the run waits its turn instead of failing a teammate's plan, or
	// sending an approved apply back through a whole plan.
	sessionCreationTimeout = 10 * time.Minute
)

// defaultRunRetryPolicy is the retry policy applied to activities in the
// template-run workflow when no activity-specific override is set.
// MaximumAttempts is temporarily pinned to 1 (no automatic retries) — in
// Temporal, 0 means unlimited attempts, not zero retries, so 1 is the value
// that disables retries.
var defaultRunRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    30 * time.Second,
	BackoffCoefficient: 2.0,
	MaximumInterval:    5 * time.Minute,
	MaximumAttempts:    1,
	NonRetryableErrorTypes: []string{
		"InvalidConfig",
		"UnsupportedCommand",
	},
}

// terraformRetryPolicy is applied to long-running Terraform commands (plan,
// apply). MaximumAttempts is temporarily pinned to 1 (no automatic retries) —
// in Temporal, 0 means unlimited attempts, not zero retries, so 1 is the
// value that disables retries.
var terraformRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    time.Minute,
	BackoffCoefficient: 2.0,
	MaximumInterval:    10 * time.Minute,
	MaximumAttempts:    1,
	NonRetryableErrorTypes: []string{
		"InvalidConfig",
		"UnsupportedCommand",
	},
}

// uploadLogRetryPolicy retries UploadLog where defaultRunRetryPolicy would
// not: an upload is idempotent (the same spool to the same key), so retrying
// it is safe, and without retries one blip in the object store loses a log
// whose command already ran. Five attempts span about half a minute, which
// rides out a brief outage without holding the run back for long; past that
// the log is recorded lost.
var uploadLogRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    2 * time.Second,
	BackoffCoefficient: 2.0,
	MaximumInterval:    30 * time.Second,
	MaximumAttempts:    5,
}

// recordLogRetryPolicy retries every RecordTemplateRunLog the workflow makes,
// where defaultRunRetryPolicy would not: the record is an upsert on the log's
// run, phase and number, so a retry rewrites the same row. It is short because
// every command waits on its live row before starting, and a row that still
// cannot be written is logged and the run goes on without it.
var recordLogRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    time.Second,
	BackoffCoefficient: 2.0,
	MaximumAttempts:    3,
}

// planWorkflow is TemplatePlanWorkflow as it carries a run, and applyWorkflow
// is TemplateApplyWorkflow. Each execution builds its own: registering a bound
// method as the workflow would share one across every execution.
type (
	planWorkflow  struct{ *run }
	applyWorkflow struct{ *run }
)

// TemplatePlanWorkflow plans a template run. A plan run ends with its plan.
// An apply or destroy run saves a plan with changes, which then waits for
// approval as a database row, not as a workflow: this workflow ends there,
// releasing its executor session, and approving the plan starts
// TemplateApplyWorkflow. Nothing holds an executor while a person decides, and
// no approval can arrive after a session has timed out.
//
// A plan with no changes completes the run. An auto-approved apply run never
// comes here: it starts TemplateApplyWorkflow directly.
//
// Its status is lifecycle's.
func TemplatePlanWorkflow(ctx workflow.Context, input domain.TemplateRunWorkflowInput) error {
	w := &planWorkflow{newRun(ctx, input, domain.RunPhasePlan)}
	return w.lifecycle()
}

// TemplateApplyWorkflow applies the plan a run saved, once someone approved it.
// It usually lands on a different executor from the plan, so it fetches the
// same commit again, puts the saved plan and its lock file back, and runs init
// before applying exactly that plan. tofu refuses a saved plan whose state has
// moved on since, so an approval can never apply something other than what
// was reviewed.
//
// An auto-approved apply run starts here with no plan at all, and applies the
// way `tofu apply -auto-approve` does.
//
// Its status is lifecycle's.
func TemplateApplyWorkflow(ctx workflow.Context, input domain.TemplateRunWorkflowInput) error {
	w := &applyWorkflow{newRun(ctx, input, domain.RunPhaseApply)}
	return w.lifecycle()
}

// lifecycle carries a plan through the run's status. It and applyWorkflow's
// are the only code that writes it: every status write is in the two, and so
// is every activity that moves the status as part of its own work, BeginApply
// and FinishPlan.
//
// A run the plan workflow does not carry fails without ever running. The plan
// starts running here, and once finished is settled: a plan with nothing to
// change, or a plan run, completes, and an apply or destroy run with changes
// waits for approval, which FinishPlan records. A destroy with nothing left to
// destroy records that it is destroyed before it completes, which is what
// moves its stack template.
//
// Any error marks the run failed before lifecycle returns it, as failOnError
// says.
func (w *planWorkflow) lifecycle() (err error) {
	defer func() { err = w.failOnError(err) }()

	if err := w.carries(); err != nil {
		return err
	}
	if err := w.setStatus(domain.TemplateRunRunning, "", ""); err != nil {
		return err
	}
	var planned domain.RunTerraformActivityOutput
	if err := w.inSession(sessionCreationTimeout, domain.RunPhasePlan, func(s *session) (err error) {
		planned, err = w.plan(s)
		return err
	}); err != nil {
		return err
	}

	var outcome domain.PlanOutcome
	if err := workflow.ExecuteActivity(w.ctx, domain.FinishPlanActivityName, domain.FinishPlanActivityInput{
		TenantID:        w.input.TenantID,
		RunID:           w.input.RunID,
		StackTemplateID: w.input.StackTemplateID,
		Operation:       w.input.Operation,
		HasChanges:      planned.HasChanges,
		Summary:         planned.Summary,
	}).Get(w.ctx, &outcome); err != nil {
		return err
	}
	switch outcome {
	case domain.PlanOutcomeNoChanges:
		if w.input.Operation == domain.OperationDestroy {
			if err := w.event(domain.TemplateRunDestroyed, nil); err != nil {
				return err
			}
		}
		return w.setStatus(domain.TemplateRunCompleted, "", "")
	case domain.PlanOutcomePlanned:
		return w.setStatus(domain.TemplateRunCompleted, "", "")
	case domain.PlanOutcomeWaiting:
		return nil
	default:
		return fmt.Errorf("unknown plan outcome %q", outcome)
	}
}

// lifecycle carries an apply through the run's status, as planWorkflow's
// carries a plan.
//
// A run the apply workflow does not carry fails without ever running. The
// apply claims its run, moving it from approved (or, auto-approved, queued) to
// running; a lost claim ends quietly, since the discard that won it already
// recorded the run's end. A finished apply completes.
//
// Any error marks the run failed before lifecycle returns it, as failOnError
// says.
func (w *applyWorkflow) lifecycle() (err error) {
	defer func() { err = w.failOnError(err) }()

	if err := w.carries(); err != nil {
		return err
	}
	var claim domain.BeginApplyActivityOutput
	if err := workflow.ExecuteActivity(w.ctx, domain.BeginApplyActivityName, domain.BeginApplyActivityInput{
		TenantID:    w.input.TenantID,
		RunID:       w.input.RunID,
		AutoApprove: w.input.AutoApprove,
		WorkflowID:  w.workflowID,
	}).Get(w.ctx, &claim); err != nil {
		return err
	}
	if !claim.Claimed {
		return nil
	}
	if err := w.inSession(sessionCreationTimeout, domain.RunPhaseApply, w.apply); err != nil {
		return err
	}
	return w.setStatus(domain.TemplateRunCompleted, "", "")
}

// run is what both of a template run's workflows share. It holds the
// control-queue context and the run's input, and it is the run's Recorder.
//
// Every method schedules control-plane work, the run's writes and the secrets
// sealed for its executor, on r.ctx, the control queue. Executor work is
// session's.
type run struct {
	ctx   workflow.Context
	input domain.TemplateRunWorkflowInput
	// phase is which of the run's workflow executions this workflow is, and
	// workflowID the Temporal workflow it runs as. Status writes and the
	// apply's claim record both on the execution.
	phase      domain.RunPhase
	workflowID string
	// logs counts the Terraform commands this workflow has run, which numbers
	// their logs. Workflow code replays in order, so a replay counts the same.
	logs int
}

// newRun sets the baseline options for every activity the run schedules
// through it: control-plane work, on the control queue. Executor work goes
// through the session inSession opens, on the execution queue.
func newRun(ctx workflow.Context, input domain.TemplateRunWorkflowInput, phase domain.RunPhase) *run {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:           domain.ControlTaskQueue,
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         defaultRunRetryPolicy,
	})
	return &run{ctx: ctx, input: input, phase: phase, workflowID: workflow.GetInfo(ctx).WorkflowExecution.ID}
}

// setStatus records the run's status. Only the two lifecycles call it, and
// failOnError for them.
func (r *run) setStatus(status domain.TemplateRunStatus, errorSummary, commandError string) error {
	return workflow.ExecuteActivity(
		r.ctx,
		domain.RecordTemplateRunStatusActivityName,
		domain.TemplateRunStatusActivityInput{
			RunID:           r.input.RunID,
			TenantID:        r.input.TenantID,
			StackTemplateID: r.input.StackTemplateID,
			Operation:       r.input.Operation,
			Status:          status,
			ErrorSummary:    errorSummary,
			CommandError:    commandError,
			Phase:           r.phase,
			WorkflowID:      r.workflowID,
		},
	).Get(r.ctx, nil)
}

// failOnError marks the run failed if err is set, and returns it; each
// lifecycle defers it on its result. If recording the failure also fails, the
// run's persisted status will not match reality, so both errors are surfaced:
// the original wrapped with %w to stay matchable by callers, the persistence
// error appended as context.
func (r *run) failOnError(err error) error {
	if err == nil {
		return nil
	}
	if failureErr := r.setStatus(domain.TemplateRunFailed, fmt.Sprintf("template run activity failed: %v", err), commandError(err)); failureErr != nil {
		return fmt.Errorf("%w (also failed to persist failure status: %w)", err, failureErr)
	}
	return err
}

// commandError is what the Terraform command behind err said, when err is a
// RunTerraform failure that carries it, and "" for any other failure.
func commandError(err error) string {
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != domain.TerraformCommandFailedErrorType || !appErr.HasDetails() {
		return ""
	}
	var failure domain.TerraformCommandFailure
	if appErr.Details(&failure) != nil {
		return ""
	}
	return failure.Message
}

// carries rejects a run the plan workflow does not plan, before any session
// or workspace exists: returning the error lets lifecycle record the single
// Failed status, with the reason attached as the run's error summary. It plans
// every operation's run, but never an auto-approved one, which has no plan.
func (w *planWorkflow) carries() error {
	switch w.input.Operation {
	case domain.OperationPlan, domain.OperationApply, domain.OperationDestroy:
		if w.input.AutoApprove {
			return fmt.Errorf("the plan workflow does not plan an auto-approved %s run: it has no plan", w.input.Operation)
		}
		return nil
	}
	return fmt.Errorf("unsupported template run operation %q", w.input.Operation)
}

// carries rejects a run the apply workflow does not apply, as planWorkflow's
// does: it applies apply and destroy runs, and only an apply run can be
// auto-approved.
func (w *applyWorkflow) carries() error {
	switch w.input.Operation {
	case domain.OperationPlan:
		return errors.New("the apply workflow does not apply a plan run: it has nothing to apply")
	case domain.OperationApply:
		return nil
	case domain.OperationDestroy:
		if w.input.AutoApprove {
			return errors.New("a destroy run cannot be auto-approved")
		}
		return nil
	}
	return fmt.Errorf("unsupported template run operation %q", w.input.Operation)
}

// plan is the plan workflow's work. An apply or destroy run keeps a plan that
// has changes, for someone to approve; a plan run keeps nothing but its log.
func (w *planWorkflow) plan(s *session) (domain.RunTerraformActivityOutput, error) {
	if err := s.ready(false); err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}

	command := domain.TerraformCommandPlan
	if w.input.Operation == domain.OperationDestroy {
		command = domain.TerraformCommandPlanDestroy
	}
	output, err := s.terraform(command)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	if !output.HasChanges || w.input.Operation == domain.OperationPlan {
		return output, nil
	}
	return output, s.savePlan()
}

// apply is the apply workflow's work: it applies an approved run's saved
// plan, or, for an auto-approved apply run, applies without one. It records
// what the command does to the stack template around it: a destroy marks the
// template destroying before it starts and destroyed once it succeeds, and an
// apply records what it applied as live. Either way the event carries what the
// command did, which is the apply execution's counts.
func (w *applyWorkflow) apply(s *session) error {
	if err := s.ready(!w.input.AutoApprove); err != nil {
		return err
	}

	destroy := w.input.Operation == domain.OperationDestroy
	command := domain.TerraformCommandApply
	switch {
	case w.input.AutoApprove:
		command = domain.TerraformCommandApplyAutoApprove
	case destroy:
		command = domain.TerraformCommandDestroy
	}

	if destroy {
		if err := w.event(domain.TemplateRunDestroying, nil); err != nil {
			return err
		}
	}
	output, err := s.terraform(command)
	if err != nil {
		return err
	}
	if destroy {
		return w.event(domain.TemplateRunDestroyed, &output.Summary)
	}
	return w.event(domain.TemplateRunApplied, &output.Summary)
}

// workspace is one phase's working directory on its executor, and the key the
// executor holds for the run. Each phase opens its own, since the apply
// usually lands on a different executor from the plan.
type workspace struct {
	// phase is the half of the run the workspace serves, which names its
	// commands' logs.
	phase         domain.RunPhase
	path          string
	terraformPath string
	// publicKey is the executor's sealing key for this run; everything secret
	// the executor needs is sealed to it on the control plane.
	publicKey []byte
}

// session is one phase's executor: the session that pins the phase's
// activities to one executor host, and the workspace the phase opened there.
//
// Its methods are the run's executor work, scheduled on s.ctx. What they need
// from the control plane, a step recorded or a secret sealed to the
// workspace's key, they ask of s.run, whose methods schedule it on the control
// queue.
type session struct {
	run *run
	ctx workflow.Context
	ws  workspace
}

// newSession waits up to creationTimeout for an executor to take the run, and
// returns the session it opens there, with no workspace open yet.
//
// CreateSession takes its base queue from the context's activity options, so
// naming the execution queue here is what places the session, and every
// activity scheduled on its context, on an executor host.
func newSession(r *run, creationTimeout time.Duration) (*session, error) {
	ctx, err := workflow.CreateSession(workflow.WithTaskQueue(r.ctx, domain.ExecutionTaskQueue), &workflow.SessionOptions{
		CreationTimeout:  creationTimeout,
		ExecutionTimeout: 24 * time.Hour,
	})
	if err != nil {
		return nil, err
	}
	return &session{run: r, ctx: ctx}, nil
}

// open creates the phase's filesystem workspace on the executor, and a sealing
// key for the run. Workflows cannot create directories directly because
// Temporal workflows must stay deterministic, so the side effect lives in
// PrepareWorkspace.
func (s *session) open(phase domain.RunPhase) error {
	s.ws.phase = phase
	var output domain.PrepareWorkspaceActivityOutput
	if err := ExecuteStep(
		s.ctx, s.run, domain.TemplateRunStepPreparingWorkspace,
		domain.PrepareWorkspaceActivityName,
		domain.PrepareWorkspaceActivityInput{RunID: s.run.input.RunID, TenantID: s.run.input.TenantID},
	).Get(s.ctx, &output); err != nil {
		return err
	}
	s.ws.path = output.WorkspacePath
	s.ws.publicKey = output.PublicKey
	return nil
}

// ready readies the open workspace for Terraform: the source at the run's
// commit, then init and workspace selection. With restorePlan, the saved plan
// and its lock file are put back once the source is in place, so init
// installs the providers the plan was made with.
func (s *session) ready(restorePlan bool) error {
	if err := s.fetchSource(); err != nil {
		return err
	}
	if restorePlan {
		if err := s.restoreSavedPlan(); err != nil {
			return err
		}
	}
	if _, err := s.terraform(domain.TerraformCommandInit); err != nil {
		return err
	}
	_, err := s.terraform(domain.TerraformCommandSelectWorkspace)
	return err
}

// close releases the run's key and deletes the workspace. Closing an apply's
// workspace deletes its saved plan too: the apply is the plan's last reader.
// It is best effort: a session that already failed has no executor left to
// clean up, and what it leaves behind is a directory and an unreadable plan
// file. The key ring evicts abandoned keys on its own.
func (s *session) close() {
	if s.ws.publicKey != nil {
		_ = workflow.ExecuteActivity(
			s.ctx,
			domain.ReleaseRunKeyActivityName,
			domain.ReleaseRunKeyActivityInput{TenantID: s.run.input.TenantID, RunID: s.run.input.RunID},
		).Get(s.ctx, nil)
	}
	if s.ws.path != "" {
		_ = workflow.ExecuteActivity(
			s.ctx,
			domain.CleanupWorkspaceActivityName,
			domain.CleanupWorkspaceActivityInput{
				TenantID:      s.run.input.TenantID,
				RunID:         s.run.input.RunID,
				WorkspacePath: s.ws.path,
				DeletePlan:    s.ws.phase == domain.RunPhaseApply,
			},
		).Get(s.ctx, nil)
	}
}

// fetchSource checks the run's commit out into the workspace, with a
// repository token sealed to the workspace's key, and notes the path of the
// Terraform root within it.
func (s *session) fetchSource() error {
	token, err := s.run.sealSourceToken(s.ws.publicKey)
	if err != nil {
		return err
	}

	// A clone of a large repository can outlast the default one-minute budget,
	// so FetchSource gets a longer one of its own rather than raising the
	// default for every other activity in the run.
	ctx := workflow.WithStartToCloseTimeout(s.ctx, 3*time.Minute)
	var output domain.FetchSourceActivityOutput
	if err := ExecuteStep(ctx, s.run, domain.TemplateRunStepFetchingSource, domain.FetchSourceActivityName, domain.FetchSourceActivityInput{
		RunID:             s.run.input.RunID,
		TenantID:          s.run.input.TenantID,
		WorkspacePath:     s.ws.path,
		RepoOwner:         s.run.input.RepoOwner,
		RepoName:          s.run.input.RepoName,
		SourceRef:         s.run.input.SelectedRef,
		ResolvedCommitSHA: s.run.input.ResolvedCommitSHA,
		RootPath:          s.run.input.RootPath,
		SealedToken:       token.SealedToken,
		FetchHint:         token.FetchHint,
	}).Get(ctx, &output); err != nil {
		return err
	}
	s.ws.terraformPath = output.TerraformPath
	return nil
}

// savePlan uploads the plan the workspace just made, under a plan key created
// for it.
func (s *session) savePlan() error {
	return s.transferPlan(domain.TemplateRunStepSavingPlan, domain.UploadPlanActivityName, true)
}

// restoreSavedPlan puts the saved plan back into the workspace, with the plan
// key it was saved with.
func (s *session) restoreSavedPlan() error {
	return s.transferPlan(domain.TemplateRunStepRestoringPlan, domain.DownloadPlanActivityName, false)
}

// transferPlan uploads or downloads the run's saved plan on the executor, as
// step, with the run's plan key sealed to the workspace's key. The plan phase
// creates the plan key; the apply phase reads the one the plan was saved with.
func (s *session) transferPlan(step domain.TemplateRunStep, activityName string, createKey bool) error {
	planKey, err := s.run.sealPlanKey(s.ws.publicKey, createKey)
	if err != nil {
		return err
	}

	// A saved plan can run to tens of megabytes, more than the default budget
	// is sized for.
	ctx := workflow.WithStartToCloseTimeout(s.ctx, 5*time.Minute)
	return ExecuteStep(ctx, s.run, step, activityName, domain.PlanArtifactActivityInput{
		TenantID:      s.run.input.TenantID,
		RunID:         s.run.input.RunID,
		TerraformPath: s.ws.terraformPath,
		SealedPlanKey: planKey.SealedPlanKey,
	}).Get(ctx, nil)
}

// terraform runs one Terraform command on the executor as the step it is,
// with the run's credentials sealed to the workspace's key, then has the
// executor upload the log it spooled, and records where it ended up.
func (s *session) terraform(command domain.TerraformCommandType) (domain.RunTerraformActivityOutput, error) {
	// Sealed for every command rather than once per run: credentials are read
	// fresh each time, and an apply can start a day after its plan.
	credentials, err := s.run.sealCredentials(s.ws.publicKey)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	// The log is named here, once, and the name travels with every step that
	// touches it. An activity retry reuses its input, so it keeps the name.
	s.run.logs++
	seq := s.run.logs
	logName, err := domain.RunLogName(s.ws.phase, seq)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	// The live row goes in before ExecuteStep records the step: the run page
	// refetches its logs when the step changes, so the row has to be there
	// by then. A row that cannot be written costs the page its live view of
	// this command, which is no reason to skip the command.
	if err := s.run.log(logName, domain.TemplateRunLog{
		State:    domain.TemplateRunLogLive,
		RunPhase: s.ws.phase,
		Seq:      seq,
		Command:  command,
	}); err != nil {
		workflow.GetLogger(s.run.ctx).Warn("record live run log", "log", logName, "error", err)
	}

	// Every Terraform command gets the configured budget and the more generous
	// retry policy, including init and workspace selection: init downloads
	// providers and modules over the network, which is no more predictable
	// than the plan that follows it.
	//
	// The heartbeat timeout is what distinguishes a command that is working
	// from one whose executor is gone: without it, a dead executor is
	// indistinguishable from a slow apply until the whole Terraform timeout
	// expires.
	ctx := workflow.WithStartToCloseTimeout(s.ctx, s.run.terraformTimeout())
	ctx = workflow.WithHeartbeatTimeout(ctx, domain.TerraformHeartbeatTimeout)
	ctx = workflow.WithRetryPolicy(ctx, *terraformRetryPolicy)
	var output domain.RunTerraformActivityOutput
	runErr := ExecuteStep(ctx, s.run, terraformCommandSteps[command], domain.RunTerraformActivityName, domain.RunTerraformActivityInput{
		RunID:             s.run.input.RunID,
		TenantID:          s.run.input.TenantID,
		StackTemplateID:   s.run.input.StackTemplateID,
		WorkspacePath:     s.ws.path,
		TerraformPath:     s.ws.terraformPath,
		WorkspaceName:     s.run.input.WorkspaceName,
		Command:           command,
		ConfigJSON:        s.run.input.ConfigJSON,
		LogName:           logName,
		SealedEnvironment: credentials.SealedEnvironment,
	}).Get(ctx, &output)
	// A failed command's log is what explains the failure, so it is saved
	// either way. Saving it never fails the step: the command's own result is
	// what the run is about.
	s.saveLog(logName, seq, command, runErr)
	if runErr != nil {
		return domain.RunTerraformActivityOutput{}, runErr
	}
	return output, nil
}

// saveLog uploads the log the executor spooled under name, from a sandbox of
// its own (UploadLog), then records it on the control plane as complete, or
// as lost if the upload never succeeded. Its number and the command and phase
// that wrote it are the workflow's, which the executor does not get to say.
//
// runErr is the command's own error, which, with the session's state, says
// why an upload failed: see executorGone.
//
// Nothing here fails the step. A log that is lost, or a row that cannot be
// written, is logged; the run's outcome stays the command's.
func (s *session) saveLog(name string, seq int, command domain.TerraformCommandType, runErr error) {
	logger := workflow.GetLogger(s.ctx)
	log := domain.TemplateRunLog{RunPhase: s.ws.phase, Seq: seq, Command: command}

	ctx := workflow.WithStartToCloseTimeout(s.ctx, 5*time.Minute)
	ctx = workflow.WithRetryPolicy(ctx, *uploadLogRetryPolicy)
	var uploaded domain.TemplateRunLog
	if err := workflow.ExecuteActivity(ctx, domain.UploadLogActivityName, domain.UploadLogActivityInput{
		TenantID: s.run.input.TenantID,
		RunID:    s.run.input.RunID,
		LogName:  name,
	}).Get(ctx, &uploaded); err != nil {
		logger.Warn("upload run log", "log", name, "error", err)
		log.State = domain.TemplateRunLogLost
		log.LostReason = domain.LogLostUploadFailed
		if s.executorGone(runErr) {
			log.LostReason = domain.LogLostExecutorGone
		}
	} else if uploaded.ContentType == "" || uploaded.UploadedAt.IsZero() {
		// The control plane refuses a complete row that does not describe
		// its object, which would leave the row live for good; an object
		// the row cannot describe is as good as lost.
		logger.Warn("upload run log returned an incomplete result", "log", name,
			"content_type", uploaded.ContentType, "uploaded_at", uploaded.UploadedAt)
		log.State = domain.TemplateRunLogLost
		log.LostReason = domain.LogLostUploadFailed
	} else {
		// Only what describes the object is taken from the executor; the
		// row's identity and key are the workflow's and the control plane's.
		log.State = domain.TemplateRunLogComplete
		log.ContentType = uploaded.ContentType
		log.SizeBytes = uploaded.SizeBytes
		log.UploadedAt = uploaded.UploadedAt
	}

	if err := s.run.log(name, log); err != nil {
		logger.Warn("record run log", "log", name, "state", log.State, "error", err)
	}
}

// executorGone reports whether the session's executor died, given the
// command's error. Either of two signs says so, because which one arrives
// depends on how it died. A dead process stops heartbeating its session
// (20 s) long before its command's own heartbeat timeout (120 s) fires, so
// the session fails and cancels the command: runErr is then a cancellation,
// and only the session's state tells. An executor whose worker stays up but
// whose command hangs keeps the session alive, and the command's heartbeat
// timeout is what tells.
func (s *session) executorGone(runErr error) bool {
	if info := workflow.GetSessionInfo(s.ctx); info != nil && info.SessionState == workflow.SessionStateFailed {
		return true
	}
	var timeoutErr *temporal.TimeoutError
	return errors.As(runErr, &timeoutErr) && timeoutErr.TimeoutType() == enumspb.TIMEOUT_TYPE_HEARTBEAT
}

// sealSourceToken resolves a token for the run's repository on the control
// plane, sealed to publicKey.
func (r *run) sealSourceToken(publicKey []byte) (domain.SealSourceTokenActivityOutput, error) {
	var token domain.SealSourceTokenActivityOutput
	err := workflow.ExecuteActivity(r.ctx, domain.SealSourceTokenActivityName, domain.SealSourceTokenActivityInput{
		RepoOwner: r.input.RepoOwner,
		RepoName:  r.input.RepoName,
		PublicKey: publicKey,
	}).Get(r.ctx, &token)
	return token, err
}

// sealPlanKey seals the run's plan key to publicKey, creating it first with
// create.
func (r *run) sealPlanKey(publicKey []byte, create bool) (domain.SealPlanKeyActivityOutput, error) {
	var planKey domain.SealPlanKeyActivityOutput
	err := workflow.ExecuteActivity(r.ctx, domain.SealPlanKeyActivityName, domain.SealPlanKeyActivityInput{
		TenantID:  r.input.TenantID,
		RunID:     r.input.RunID,
		PublicKey: publicKey,
		Create:    create,
	}).Get(r.ctx, &planKey)
	return planKey, err
}

// sealCredentials seals the stack template's credentials, read fresh, to
// publicKey.
func (r *run) sealCredentials(publicKey []byte) (domain.SealRunCredentialsActivityOutput, error) {
	var credentials domain.SealRunCredentialsActivityOutput
	err := workflow.ExecuteActivity(r.ctx, domain.SealRunCredentialsActivityName, domain.SealRunCredentialsActivityInput{
		TenantID:        r.input.TenantID,
		StackTemplateID: r.input.StackTemplateID,
		PublicKey:       publicKey,
	}).Get(r.ctx, &credentials)
	return credentials, err
}

// terraformTimeout is how long one Terraform command may run before Temporal
// fails it. It comes from the deployment's configuration, stamped onto the
// input when the run was dispatched, so a run keeps the budget it started with
// even if the control plane is reconfigured while it is in flight. Zero means
// nothing configured it, which is the default.
func (r *run) terraformTimeout() time.Duration {
	if r.input.TerraformTimeout > 0 {
		return r.input.TerraformTimeout
	}
	return domain.DefaultTerraformTimeout
}

// terraformCommandSteps is the step each Terraform command is. Planning a
// destroy is planning, and applying one is applying: the run's operation
// already says it is a destroy.
var terraformCommandSteps = map[domain.TerraformCommandType]domain.TemplateRunStep{
	domain.TerraformCommandInit:             domain.TemplateRunStepInitializing,
	domain.TerraformCommandSelectWorkspace:  domain.TemplateRunStepSelectingWorkspace,
	domain.TerraformCommandPlan:             domain.TemplateRunStepPlanning,
	domain.TerraformCommandPlanDestroy:      domain.TemplateRunStepPlanning,
	domain.TerraformCommandApply:            domain.TemplateRunStepApplying,
	domain.TerraformCommandDestroy:          domain.TemplateRunStepApplying,
	domain.TerraformCommandApplyAutoApprove: domain.TemplateRunStepApplying,
}

// Record records the step the run is starting, for the people watching it.
func (r *run) Record(step domain.TemplateRunStep) error {
	return workflow.ExecuteActivity(
		r.ctx,
		domain.RecordTemplateRunStepActivityName,
		domain.TemplateRunStepActivityInput{
			RunID:    r.input.RunID,
			TenantID: r.input.TenantID,
			Step:     step,
		},
	).Get(r.ctx, nil)
}

// inSession runs fn in an executor session, with the phase's workspace open
// in it, then closes the workspace and completes the session, whether fn
// succeeded or not. Waiting for the executor is the run's first step: an
// approved apply can wait minutes for a free one.
//
// fn's argument is the only way to reach the session, so nothing can use it
// once it is completed: a completed session's context is no longer pinned to
// its host, and would send an activity to any executor, which holds none of
// this one's keys.
func (r *run) inSession(creationTimeout time.Duration, phase domain.RunPhase, fn func(*session) error) error {
	if err := r.Record(domain.TemplateRunStepWaitingForExecutor); err != nil {
		return err
	}
	s, err := newSession(r, creationTimeout)
	if err != nil {
		return err
	}
	defer workflow.CompleteSession(s.ctx)

	defer s.close()
	if err := s.open(phase); err != nil {
		return err
	}
	return fn(s)
}

// event records something the run did to its stack template, with the counts
// it carries, if any.
func (r *run) event(event domain.TemplateRunEvent, summary *domain.PlanSummary) error {
	return workflow.ExecuteActivity(
		r.ctx,
		domain.RecordTemplateRunEventActivityName,
		domain.TemplateRunEventActivityInput{
			RunID:           r.input.RunID,
			TenantID:        r.input.TenantID,
			StackTemplateID: r.input.StackTemplateID,
			Operation:       r.input.Operation,
			Event:           event,
			Summary:         summary,
		},
	).Get(r.ctx, nil)
}

// log hands a log's row, in whatever state it is in, to the control plane,
// which owns the database. Every call retries under recordLogRetryPolicy,
// within a short budget of its own: a row write is quick, and the run waits on
// it before its command starts.
//
// The identity is taken from the workflow's own input, never from the returned
// metadata. UploadLog runs on the data plane, so everything it returns is
// attacker-controlled once an executor is compromised, and
// RecordTemplateRunLog upserts on (tenant_id, run_id, phase) while checking
// only that the run exists — not that it is this run. A log claiming another
// tenant's run would therefore repoint that run's object_key. For the same
// reason the name is the one the workflow chose, and RecordTemplateRunLog
// derives the object key from all three rather than storing the returned one.
//
// On the honest path this overwrites nothing: PutTemplateRunLog derives the
// same fields from the activity input the workflow supplied.
func (r *run) log(name string, log domain.TemplateRunLog) error {
	log.TenantID = r.input.TenantID
	log.RunID = r.input.RunID
	log.Name = name
	ctx := workflow.WithStartToCloseTimeout(r.ctx, 30*time.Second)
	ctx = workflow.WithRetryPolicy(ctx, *recordLogRetryPolicy)
	return workflow.ExecuteActivity(
		ctx,
		domain.RecordTemplateRunLogActivityName,
		log,
	).Get(ctx, nil)
}
