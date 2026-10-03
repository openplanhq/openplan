package domain

// One Terraform operation against a stack template, and its lifecycle.

import (
	"encoding/json"
	"slices"
	"time"
)

// OperationType identifies what a run is for, as the Terraform CLI would name
// it. A plan run only plans: it shows what would change and ends. An apply run
// saves a plan of the desired state and applies it once someone approves it,
// or, started with auto-approve, applies without a saved plan at all. A
// destroy run saves a plan to destroy everything and applies it once
// approved; it has no auto-approve.
type OperationType string

const (
	OperationPlan    OperationType = "plan"
	OperationApply   OperationType = "apply"
	OperationDestroy OperationType = "destroy"
)

// TerraformCommandType identifies one Terraform subprocess command run by an executor.
type TerraformCommandType string

const (
	TerraformCommandInit            TerraformCommandType = "init"
	TerraformCommandSelectWorkspace TerraformCommandType = "select_workspace"
	TerraformCommandPlan            TerraformCommandType = "plan"
	// TerraformCommandPlanDestroy is a destroy run's plan: a plan to destroy
	// everything the template manages. It is the same step and records the
	// same log as TerraformCommandPlan.
	TerraformCommandPlanDestroy TerraformCommandType = "plan_destroy"
	// TerraformCommandApply and TerraformCommandDestroy both apply the run's
	// saved plan. They stay two commands because a destroy records the
	// destroying and destroyed events around it.
	TerraformCommandApply   TerraformCommandType = "apply"
	TerraformCommandDestroy TerraformCommandType = "destroy"
	// TerraformCommandApplyAutoApprove is an auto-approved apply run's only
	// Terraform step besides setup: it plans and applies in one go, with no
	// saved plan, so it is the one apply that takes the run's variables. It
	// is the same step and records the same log as TerraformCommandApply.
	TerraformCommandApplyAutoApprove TerraformCommandType = "apply_auto_approve"
)

// PlanSummary counts the resource changes in a saved plan. A replacement
// counts once as an add and once as a destroy, as Terraform's own summary line
// does.
type PlanSummary struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
}

// Valid reports whether the operation is one of the supported operation types.
func (operation OperationType) Valid() bool {
	switch operation {
	case OperationPlan, OperationApply, OperationDestroy:
		return true
	default:
		return false
	}
}

// TemplateRunStatus is a run's lifecycle state: the one fact about a run that
// decides what may happen to it next. It is not progress. What a running run
// is doing is its Step.
type TemplateRunStatus string

const (
	TemplateRunQueued TemplateRunStatus = "queued"
	// TemplateRunRunning is a run a workflow is working on: planning, or,
	// once claimed for its apply, applying. What it is doing right now is its
	// Step.
	TemplateRunRunning         TemplateRunStatus = "running"
	TemplateRunWaitingApproval TemplateRunStatus = "waiting_approval"
	TemplateRunApproved        TemplateRunStatus = "approved"
	TemplateRunCompleted       TemplateRunStatus = "completed"
	TemplateRunFailed          TemplateRunStatus = "failed"
	TemplateRunCanceled        TemplateRunStatus = "canceled"
)

// AllTemplateRunStatuses is every status a run may hold, in lifecycle order.
//
// It exists because two things have to reason about the whole vocabulary rather
// than one status at a time: Valid below, and the template_runs_in_flight_idx
// test, which asserts the index's terminal-status predicate agrees with
// Terminal for every status there is.
//
// A status added to the constants above but not to this slice would be rejected
// by Valid and never reach that index test, so TestTemplateRunStatusValid reads
// the constants back out of this file and fails if the two disagree.
var AllTemplateRunStatuses = []TemplateRunStatus{
	TemplateRunQueued,
	TemplateRunRunning,
	TemplateRunWaitingApproval,
	TemplateRunApproved,
	TemplateRunCompleted,
	TemplateRunFailed,
	TemplateRunCanceled,
}

// Valid reports whether the status is one of the supported run states.
func (status TemplateRunStatus) Valid() bool {
	return slices.Contains(AllTemplateRunStatuses, status)
}

// Terminal reports whether a run status represents no further workflow work.
func (status TemplateRunStatus) Terminal() bool {
	switch status {
	case TemplateRunCompleted, TemplateRunFailed, TemplateRunCanceled:
		return true
	case TemplateRunQueued, TemplateRunRunning, TemplateRunWaitingApproval, TemplateRunApproved:
		return false
	default:
		return false
	}
}

// TemplateRunStep is what a running run is doing right now, for the people
// watching it. It is not a status: nothing branches on it. It is recorded when
// a step starts, never when one finishes, and kept when the run ends, so a
// failed run still says where it failed. The zero value is a run that has not
// started a step.
type TemplateRunStep string

const (
	TemplateRunStepWaitingForExecutor TemplateRunStep = "waiting_for_executor"
	TemplateRunStepPreparingWorkspace TemplateRunStep = "preparing_workspace"
	TemplateRunStepFetchingSource     TemplateRunStep = "fetching_source"
	TemplateRunStepRestoringPlan      TemplateRunStep = "restoring_plan"
	TemplateRunStepInitializing       TemplateRunStep = "initializing"
	TemplateRunStepSelectingWorkspace TemplateRunStep = "selecting_workspace"
	TemplateRunStepPlanning           TemplateRunStep = "planning"
	TemplateRunStepSavingPlan         TemplateRunStep = "saving_plan"
	TemplateRunStepApplying           TemplateRunStep = "applying"
)

// AllTemplateRunSteps is every step a run may record, in the order a run
// takes them. The step check constraint in the migrations lists the same
// values.
var AllTemplateRunSteps = []TemplateRunStep{
	TemplateRunStepWaitingForExecutor,
	TemplateRunStepPreparingWorkspace,
	TemplateRunStepFetchingSource,
	TemplateRunStepRestoringPlan,
	TemplateRunStepInitializing,
	TemplateRunStepSelectingWorkspace,
	TemplateRunStepPlanning,
	TemplateRunStepSavingPlan,
	TemplateRunStepApplying,
}

// Valid reports whether the step is one a run may record. The zero value is
// not: it is what a run has before it records one.
func (step TemplateRunStep) Valid() bool {
	return slices.Contains(AllTemplateRunSteps, step)
}

// TemplateRunEvent is something a run did to its stack template. The workflow
// records it as it happens, rather than the store inferring it from a status.
type TemplateRunEvent string

const (
	// TemplateRunApplied is an apply run's apply succeeding: what it applied
	// is now what is live.
	TemplateRunApplied TemplateRunEvent = "applied"
	// TemplateRunDestroying is a destroy run about to destroy. From here, a
	// failure leaves its stack template failed.
	TemplateRunDestroying TemplateRunEvent = "destroying"
	// TemplateRunDestroyed is a destroy run's destroy succeeding, or its plan
	// finding nothing left to destroy.
	TemplateRunDestroyed TemplateRunEvent = "destroyed"
)

// TemplateRunExecutionStatus is where one workflow execution of a run, or one
// step of it, stands: running, or finished one way or the other.
type TemplateRunExecutionStatus string

const (
	TemplateRunExecutionRunning   TemplateRunExecutionStatus = "running"
	TemplateRunExecutionSucceeded TemplateRunExecutionStatus = "succeeded"
	TemplateRunExecutionFailed    TemplateRunExecutionStatus = "failed"
)

// TemplateRunWorkflowExecution is one workflow that carried a run: its plan,
// or its apply. An approved apply run has both; a plan run and an
// auto-approved apply run have one.
type TemplateRunWorkflowExecution struct {
	Phase      RunPhase `json:"phase"`
	WorkflowID string   `json:"workflow_id"`
	// Actor is who started this execution: the run's requester for a plan or
	// an auto-approved apply, and its approver for an approved apply.
	Actor  UserID                     `json:"actor"`
	Status TemplateRunExecutionStatus `json:"status"`
	// FinishedAt reads as the zero time while the execution runs.
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	ErrorSummary string    `json:"error_summary"`
	// Summary is what a plan would change, or what an apply did. Nil for a
	// plan with no changes, and until it is known.
	Summary *PlanSummary               `json:"summary"`
	Steps   []TemplateRunStepExecution `json:"steps"`
}

// TemplateRunStepExecution is one step a workflow execution ran. A step runs
// until the next one starts, or until its execution ends.
type TemplateRunStepExecution struct {
	Step       TemplateRunStep            `json:"step"`
	Status     TemplateRunExecutionStatus `json:"status"`
	StartedAt  time.Time                  `json:"started_at"`
	FinishedAt time.Time                  `json:"finished_at"`
}

// TemplateRun is one Terraform operation against a StackTemplate.
type TemplateRun struct {
	ID                 TemplateRunID      `json:"id"`
	TenantID           TenantID           `json:"tenant_id"`
	StackTemplateID    StackTemplateID    `json:"stack_template_id"`
	TemplateRevisionID TemplateRevisionID `json:"template_revision_id"`
	SourceTemplateID   SourceTemplateID   `json:"source_template_id"`
	Operation          OperationType      `json:"operation"`
	SelectedRef        string             `json:"selected_ref"`
	ResolvedCommitSHA  string             `json:"resolved_commit_sha"`
	WorkspaceName      string             `json:"workspace_name"`
	ConfigJSON         json.RawMessage    `json:"config_json"`
	BackendType        string             `json:"backend_type"`
	BackendConfigHash  string             `json:"backend_config_hash"`
	Status             TemplateRunStatus  `json:"status"`
	// Step is what the run is doing, or, once it ended, what it was doing
	// last: the latest step of the workflow execution that started last.
	// Empty until that execution starts a step.
	Step         TemplateRunStep `json:"step"`
	TriggerActor UserID          `json:"trigger_actor"`
	// TriggerActorDisplayName labels TriggerActor for people: the name the
	// users projection holds for that subject, or the subject itself when it
	// has no row. The app fills it on every run it hands out; the store
	// neither reads nor writes it. TriggerActor stays the identity.
	TriggerActorDisplayName string `json:"trigger_actor_display_name"`
	// CreatedAt is when the run was requested. When work started is its
	// executions' StartedAt.
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at"`
	// ErrorSummary is why the run failed: its failed workflow execution's
	// error. Empty otherwise.
	ErrorSummary string `json:"error_summary"`
	// RunNumber counts runs within one stack template, from 1. It is what
	// people see and what URLs carry; ID stays the identity everywhere else.
	RunNumber int `json:"run_number"`
	// AutoApprove means the trigger actor asked an apply run to apply straight
	// away, with no saved plan and without waiting for anyone to approve it.
	AutoApprove bool `json:"auto_approve"`
	// PlanSummary is what the plan would change, from the run's plan
	// execution, or, for an auto-approved apply, which has no plan, what the
	// apply changed. Nil until known, and for a plan with no changes.
	PlanSummary *PlanSummary `json:"plan_summary"`
	// Executions are the workflows that carried the run, in the order they
	// started, each with its steps. Only a read of one run fills them, empty
	// for a run no workflow has carried yet; a list leaves them nil, which
	// omits the field.
	Executions []TemplateRunWorkflowExecution `json:"executions,omitzero"`
}

// TemplateRunLog records the object-store location for one run phase log.
type TemplateRunLog struct {
	TenantID    TenantID      `json:"tenant_id"`
	RunID       TemplateRunID `json:"run_id"`
	Phase       string        `json:"phase"`
	ObjectKey   string        `json:"object_key"`
	ContentType string        `json:"content_type"`
	SizeBytes   int64         `json:"size_bytes"`
	UploadedAt  time.Time     `json:"uploaded_at"`
}

// TemplateRunApproval records who approved a waiting run.
// TemplateRunApproval records who approved a waiting run.
type TemplateRunApproval struct {
	RunID      TemplateRunID
	TenantID   TenantID
	ApprovedBy UserID
	ApprovedAt time.Time
}

// TemplateRunDiscard records who threw away a plan waiting for approval, and
// why. A discarded run ends canceled.
type TemplateRunDiscard struct {
	RunID       TemplateRunID
	TenantID    TenantID
	RequestedBy UserID
	Reason      string
	RequestedAt time.Time
}
