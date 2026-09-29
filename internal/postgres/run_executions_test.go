package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vishu42/openplan/internal/domain"
)

// violates reports whether err is Postgres refusing a write with code on
// constraint.
func violates(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code && pgErr.ConstraintName == constraint
}

// insertWorkflowExecutionRow writes a workflow execution of tenant_123's run
// directly, for tests of the table's own constraints, and returns its id.
func insertWorkflowExecutionRow(ctx context.Context, pool *pgxpool.Pool, runID domain.TemplateRunID, phase domain.RunPhase, status domain.TemplateRunExecutionStatus) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `
		insert into template_run_workflow_executions (
			tenant_id, run_id, phase, workflow_id, actor, status, started_at, finished_at
		) values (
			'tenant_123', $1::text, $2::text, 'template-run/tenant_123/' || $1::text, 'user_123', $3::text, now(),
			case when $3::text = 'running' then null else now() end
		)
		returning id
	`, runID, phase, status).Scan(&id)
	return id, err
}

// insertStepExecutionRow writes a step of a workflow execution directly, for
// tests of the table's own constraints.
func insertStepExecutionRow(ctx context.Context, pool *pgxpool.Pool, executionID int64, runID domain.TemplateRunID, step domain.TemplateRunStep, status domain.TemplateRunExecutionStatus) error {
	_, err := pool.Exec(ctx, `
		insert into template_run_step_executions (
			workflow_execution_id, run_id, tenant_id, step, status, started_at, finished_at
		) values (
			$1, $2::text, 'tenant_123', $3::text, $4::text, now(),
			case when $4::text = 'running' then null else now() end
		)
	`, executionID, runID, step, status)
	return err
}

type workflowExecutionRow struct {
	Phase                domain.RunPhase
	WorkflowID           string
	Actor                domain.UserID
	Status               domain.TemplateRunExecutionStatus
	StartedAt            time.Time
	FinishedAt           *time.Time
	ErrorSummary         string
	Add, Change, Destroy *int
}

// workflowExecutionRows reads tenant_123's run's workflow executions in the
// order they were written.
func workflowExecutionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID domain.TemplateRunID) []workflowExecutionRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
		select phase, workflow_id, actor, status, started_at, finished_at, error_summary,
			resource_add, resource_change, resource_destroy
		from template_run_workflow_executions
		where tenant_id = 'tenant_123' and run_id = $1
		order by id
	`, runID)
	if err != nil {
		t.Fatalf("read workflow executions: %v", err)
	}
	defer rows.Close()
	var got []workflowExecutionRow
	for rows.Next() {
		var row workflowExecutionRow
		if err := rows.Scan(&row.Phase, &row.WorkflowID, &row.Actor, &row.Status, &row.StartedAt, &row.FinishedAt,
			&row.ErrorSummary, &row.Add, &row.Change, &row.Destroy); err != nil {
			t.Fatalf("scan workflow execution: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read workflow executions: %v", err)
	}
	return got
}

type stepExecutionRow struct {
	Step       domain.TemplateRunStep
	Status     domain.TemplateRunExecutionStatus
	StartedAt  time.Time
	FinishedAt *time.Time
}

// stepExecutionRows reads tenant_123's run's steps, across its executions, in
// the order they were written.
func stepExecutionRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID domain.TemplateRunID) []stepExecutionRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
		select step, status, started_at, finished_at
		from template_run_step_executions
		where tenant_id = 'tenant_123' and run_id = $1
		order by id
	`, runID)
	if err != nil {
		t.Fatalf("read step executions: %v", err)
	}
	defer rows.Close()
	var got []stepExecutionRow
	for rows.Next() {
		var row stepExecutionRow
		if err := rows.Scan(&row.Step, &row.Status, &row.StartedAt, &row.FinishedAt); err != nil {
			t.Fatalf("scan step execution: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read step executions: %v", err)
	}
	return got
}

// A run is carried by one workflow at a time: a second running execution of
// the same run is refused, whatever its phase.
func TestWorkflowExecutionsAllowOneRunningPerRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunQueued))

	if _, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionRunning); err != nil {
		t.Fatal(err)
	}
	_, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhaseApply, domain.TemplateRunExecutionRunning)
	if !violates(err, "23505", "template_run_workflow_executions_one_running_idx") {
		t.Fatalf("second running execution: error = %v, want the one-running index", err)
	}
}

// A run has at most one execution per phase.
func TestWorkflowExecutionsAllowOnePerPhase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunQueued))

	if _, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	_, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionRunning)
	if !violates(err, "23505", "template_run_workflow_executions_phase_key") {
		t.Fatalf("second plan execution: error = %v, want the phase key", err)
	}
}

// A step carries its run's ID as well as its execution's, and the two cannot
// disagree.
func TestStepExecutionsBelongToTheirExecutionsRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunQueued))
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_456", "run_456", domain.TemplateRunQueued))

	executionID, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionRunning)
	if err != nil {
		t.Fatal(err)
	}
	err = insertStepExecutionRow(ctx, pool, executionID, "run_456", domain.TemplateRunStepPlanning, domain.TemplateRunExecutionRunning)
	if !violates(err, "23503", "template_run_step_executions_execution_fkey") {
		t.Fatalf("step under another run: error = %v, want the composite foreign key", err)
	}
}

// An execution is on one step at a time.
func TestStepExecutionsAllowOneRunningPerExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunQueued))

	executionID, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionRunning)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertStepExecutionRow(ctx, pool, executionID, "run_123", domain.TemplateRunStepFetchingSource, domain.TemplateRunExecutionRunning); err != nil {
		t.Fatal(err)
	}
	err = insertStepExecutionRow(ctx, pool, executionID, "run_123", domain.TemplateRunStepInitializing, domain.TemplateRunExecutionRunning)
	if !violates(err, "23505", "template_run_step_executions_one_running_idx") {
		t.Fatalf("second running step: error = %v, want the one-running index", err)
	}
}

// Every step in domain.AllTemplateRunSteps passes the step check: the
// constraint is a copy of that list, and this keeps the two from drifting.
func TestStepExecutionsAcceptEveryDomainStep(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunQueued))

	executionID, err := insertWorkflowExecutionRow(ctx, pool, "run_123", domain.RunPhasePlan, domain.TemplateRunExecutionRunning)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range domain.AllTemplateRunSteps {
		if err := insertStepExecutionRow(ctx, pool, executionID, "run_123", step, domain.TemplateRunExecutionSucceeded); err != nil {
			t.Fatalf("step %q: %v", step, err)
		}
	}
}

func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// statusWrite is the status write a run's workflow makes, carrying the phase
// and the workflow ID the dispatcher gives that phase.
func statusWrite(runID domain.TemplateRunID, operation domain.OperationType, status domain.TemplateRunStatus, phase domain.RunPhase) domain.TemplateRunStatusActivityInput {
	workflowID := "template-run/tenant_123/" + string(runID)
	if phase == domain.RunPhaseApply {
		workflowID += "/apply"
	}
	return domain.TemplateRunStatusActivityInput{
		RunID: runID, TenantID: "tenant_123", StackTemplateID: "stack_template_123",
		Operation: operation, Status: status, Phase: phase, WorkflowID: workflowID,
	}
}

// The plan workflow starting a run starts the run's plan execution, as the
// run's requester. Starting it again changes nothing.
func TestPlanStartRecordsAPlanExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)

	for attempt := 1; attempt <= 2; attempt++ {
		mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationApply, domain.TemplateRunRunning, domain.RunPhasePlan)))
	}

	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 {
		t.Fatalf("executions = %#v, want one", rows)
	}
	got := rows[0]
	if got.Phase != domain.RunPhasePlan || got.WorkflowID != "template-run/tenant_123/run_123" ||
		got.Actor != "user_123" || got.Status != domain.TemplateRunExecutionRunning || got.FinishedAt != nil {
		t.Fatalf("execution = %#v, want a running plan by user_123", got)
	}
}

// A status write that starts a run must say which phase it starts; without
// one, nothing is written.
func TestPlanStartNeedsAPhase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)

	write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunRunning, "")
	if err := store.RecordTemplateRunStatus(ctx, write); err == nil {
		t.Fatal("RecordTemplateRunStatus without a phase returned nil")
	}
	if got := runStatus(t, ctx, pool, "run_123"); got != domain.TemplateRunQueued {
		t.Fatalf("status = %q, want queued", got)
	}
	if rows := workflowExecutionRows(t, ctx, pool, "run_123"); len(rows) != 0 {
		t.Fatalf("executions = %#v, want none", rows)
	}
}

// An approved apply's claim starts its apply execution as whoever approved
// it. A retried claim changes nothing.
func TestApplyClaimRecordsTheApprover(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunWaitingApproval)
	mustSucceed(t, store.ApproveTemplateRun(ctx, domain.TemplateRunApproval{
		RunID: "run_123", TenantID: "tenant_123", ApprovedBy: "approver_456", ApprovedAt: time.Now(),
	}))

	for attempt := 1; attempt <= 2; attempt++ {
		claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply")
		if err != nil || !claimed {
			t.Fatalf("attempt %d: BeginTemplateApply = %v, %v; want claimed", attempt, claimed, err)
		}
	}

	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 {
		t.Fatalf("executions = %#v, want one", rows)
	}
	got := rows[0]
	if got.Phase != domain.RunPhaseApply || got.WorkflowID != "template-run/tenant_123/run_123/apply" ||
		got.Actor != "approver_456" || got.Status != domain.TemplateRunExecutionRunning {
		t.Fatalf("execution = %#v, want a running apply by approver_456", got)
	}
}

// An auto-approved apply has no approver: its apply is its requester's.
func TestAutoApprovedClaimRecordsTheRequester(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedAutoApprovedRun(t, ctx, pool, "run_auto", domain.TemplateRunQueued)

	claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_auto", true, "template-run/tenant_123/run_auto/apply")
	if err != nil || !claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
	}
	rows := workflowExecutionRows(t, ctx, pool, "run_auto")
	if len(rows) != 1 || rows[0].Phase != domain.RunPhaseApply || rows[0].Actor != "user_123" {
		t.Fatalf("executions = %#v, want one apply by user_123", rows)
	}
}

// An apply that loses its claim to a discard never carried the run and
// records nothing.
func TestLostClaimRecordsNoExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunApproved)
	if _, err := discardTemplateRun(ctx, pool, domain.TemplateRunDiscard{TenantID: "tenant_123", RunID: "run_123", RequestedBy: "user_123"}); err != nil {
		t.Fatal(err)
	}

	claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply")
	if err != nil || claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want not claimed", claimed, err)
	}
	if rows := workflowExecutionRows(t, ctx, pool, "run_123"); len(rows) != 0 {
		t.Fatalf("executions = %#v, want none", rows)
	}
}

// A plan waiting for approval is finished: its execution ends with its counts
// while the run waits.
func TestFinishTemplatePlanWaitingFinishesThePlanExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	outcome, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, HasChanges: true, Summary: domain.PlanSummary{Add: 3, Change: 1},
	})
	if err != nil || outcome != domain.PlanOutcomeWaiting {
		t.Fatalf("FinishTemplatePlan = %q, %v; want waiting", outcome, err)
	}
	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 || rows[0].Status != domain.TemplateRunExecutionSucceeded || rows[0].FinishedAt == nil {
		t.Fatalf("executions = %#v, want the plan succeeded", rows)
	}
	if rows[0].Add == nil || *rows[0].Add != 3 || *rows[0].Change != 1 || *rows[0].Destroy != 0 {
		t.Fatalf("plan counts = %v/%v/%v, want 3/1/0", rows[0].Add, rows[0].Change, rows[0].Destroy)
	}
}

// A plan with no changes has nothing to count: its execution keeps no counts,
// which is what makes the run read "No changes".
func TestFinishTemplatePlanWithoutChangesStoresNoCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	if _, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply,
	}); err != nil {
		t.Fatal(err)
	}
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationApply, domain.TemplateRunCompleted, domain.RunPhasePlan)))

	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 || rows[0].Status != domain.TemplateRunExecutionSucceeded || rows[0].Add != nil {
		t.Fatalf("executions = %#v, want the plan succeeded with no counts", rows)
	}
}

// A failed run's running execution fails with the run's error.
func TestFailingARunFailsItsRunningExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunFailed, domain.RunPhasePlan)
	write.ErrorSummary = "plan: exit status 1"
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))

	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 || rows[0].Status != domain.TemplateRunExecutionFailed ||
		rows[0].ErrorSummary != "plan: exit status 1" || rows[0].FinishedAt == nil {
		t.Fatalf("executions = %#v, want the plan failed with its error", rows)
	}
}

// A workflow that fails before it claims its run still leaves a row holding
// its error: started and finished at once, with no steps. A retry of the
// write changes nothing.
func TestFailingBeforeTheClaimRecordsAFailedExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunApproved)

	write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunFailed, domain.RunPhaseApply)
	write.ErrorSummary = "the apply workflow failed before its claim"
	for attempt := 1; attempt <= 2; attempt++ {
		mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))
	}

	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 {
		t.Fatalf("executions = %#v, want one", rows)
	}
	got := rows[0]
	if got.Phase != domain.RunPhaseApply || got.Status != domain.TemplateRunExecutionFailed ||
		got.ErrorSummary != write.ErrorSummary || got.FinishedAt == nil || !got.FinishedAt.Equal(got.StartedAt) {
		t.Fatalf("execution = %#v, want an apply failed at once with the error", got)
	}
}

// A run completes only from a running execution; one with none is refused.
func TestCompletingARunNeedsARunningExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	if _, err := pool.Exec(ctx, `update template_runs set status = 'running' where id = 'run_123'`); err != nil {
		t.Fatal(err)
	}

	if err := store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationApply, domain.TemplateRunCompleted, domain.RunPhasePlan)); err == nil {
		t.Fatal("completing a run with no running execution returned nil")
	}
	if got := runStatus(t, ctx, pool, "run_123"); got != domain.TemplateRunRunning {
		t.Fatalf("status = %q, want running", got)
	}
}

func recordSteps(t *testing.T, ctx context.Context, store *Store, runID domain.TemplateRunID, steps ...domain.TemplateRunStep) {
	t.Helper()
	for _, step := range steps {
		if err := store.RecordTemplateRunStep(ctx, domain.TemplateRunStepActivityInput{TenantID: "tenant_123", RunID: runID, Step: step}); err != nil {
			t.Fatalf("RecordTemplateRunStep(%q): %v", step, err)
		}
	}
}

// A step runs until the next one starts: starting one ends the one before it,
// at the same instant.
func TestRecordTemplateRunStepEndsTheStepBeforeIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunRunning))

	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepFetchingSource, domain.TemplateRunStepInitializing)

	rows := stepExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 2 {
		t.Fatalf("steps = %#v, want two", rows)
	}
	if rows[0].Step != domain.TemplateRunStepFetchingSource || rows[0].Status != domain.TemplateRunExecutionSucceeded ||
		rows[0].FinishedAt == nil || !rows[0].FinishedAt.Equal(rows[1].StartedAt) {
		t.Fatalf("first step = %#v, want fetching_source ended as initializing started", rows[0])
	}
	if rows[1].Step != domain.TemplateRunStepInitializing || rows[1].Status != domain.TemplateRunExecutionRunning || rows[1].FinishedAt != nil {
		t.Fatalf("second step = %#v, want initializing running", rows[1])
	}
}

// Writing the step the execution is already on is that write retried: it
// changes nothing, and in particular does not end its own row.
func TestRecordTemplateRunStepRetryChangesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunRunning))

	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepPlanning, domain.TemplateRunStepPlanning)

	rows := stepExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 || rows[0].Status != domain.TemplateRunExecutionRunning || rows[0].FinishedAt != nil {
		t.Fatalf("steps = %#v, want planning, once, still running", rows)
	}
}

// A step the execution already finished is not a retry but a second run of
// it, which would corrupt the timeline: it is refused and changes nothing.
func TestRecordTemplateRunStepRejectsASecondRunOfAStep(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)
	seedTemplateRun(t, ctx, pool, templateRunAt("stack_template_123", "run_123", domain.TemplateRunRunning))
	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepPlanning, domain.TemplateRunStepSavingPlan)

	err := store.RecordTemplateRunStep(ctx, domain.TemplateRunStepActivityInput{TenantID: "tenant_123", RunID: "run_123", Step: domain.TemplateRunStepPlanning})
	if err == nil {
		t.Fatal("a second run of planning returned nil")
	}
	rows := stepExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 2 || rows[1].Step != domain.TemplateRunStepSavingPlan || rows[1].Status != domain.TemplateRunExecutionRunning {
		t.Fatalf("steps = %#v, want saving_plan still running", rows)
	}
}

// When its execution ends, the step it was on ends with it, the same way.
func TestEndingARunEndsItsLastStep(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		status domain.TemplateRunStatus
		want   domain.TemplateRunExecutionStatus
	}{
		{status: domain.TemplateRunCompleted, want: domain.TemplateRunExecutionSucceeded},
		{status: domain.TemplateRunFailed, want: domain.TemplateRunExecutionFailed},
	} {
		t.Run(string(testCase.status), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			pool := openMigratedTestPool(t, ctx)
			store := savedPlanStore(t, pool)
			seedStackWithTemplate(t, ctx, store)
			seedPlanRun(t, ctx, pool, "run_123", domain.OperationPlan, domain.TemplateRunRunning)
			recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepPlanning)

			mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationPlan, testCase.status, domain.RunPhasePlan)))

			rows := stepExecutionRows(t, ctx, pool, "run_123")
			if len(rows) != 1 || rows[0].Status != testCase.want || rows[0].FinishedAt == nil {
				t.Fatalf("steps = %#v, want planning %s", rows, testCase.want)
			}
		})
	}
}

// A plan that waits for approval ends its last step as its execution ends.
func TestFinishTemplatePlanWaitingEndsTheLastStep(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)
	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepPlanning, domain.TemplateRunStepSavingPlan)

	if _, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, HasChanges: true, Summary: domain.PlanSummary{Add: 1},
	}); err != nil {
		t.Fatal(err)
	}
	rows := stepExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 2 || rows[1].Status != domain.TemplateRunExecutionSucceeded || rows[1].FinishedAt == nil {
		t.Fatalf("steps = %#v, want saving_plan succeeded", rows)
	}
}

// runAt reads a run through GetTemplateRun, and fails unless ListTemplateRuns
// derives the same step, error and counts for it: the list is what the UI
// polls, and it must read every run the way a single read does.
func runAt(t *testing.T, ctx context.Context, store *Store, runID domain.TemplateRunID) domain.TemplateRun {
	t.Helper()
	run, err := store.GetTemplateRun(ctx, "tenant_123", runID)
	if err != nil {
		t.Fatalf("GetTemplateRun(%s): %v", runID, err)
	}
	runs, err := store.ListTemplateRuns(ctx, "tenant_123", run.StackTemplateID)
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range runs {
		if listed.ID != runID {
			continue
		}
		if listed.Step != run.Step || listed.ErrorSummary != run.ErrorSummary || !reflect.DeepEqual(listed.PlanSummary, run.PlanSummary) {
			t.Fatalf("listed %s reads step %q, error %q, summary %#v; GetTemplateRun reads %q, %q, %#v",
				runID, listed.Step, listed.ErrorSummary, listed.PlanSummary, run.Step, run.ErrorSummary, run.PlanSummary)
		}
		return run
	}
	t.Fatalf("ListTemplateRuns has no %s", runID)
	return domain.TemplateRun{}
}

func approve(t *testing.T, ctx context.Context, store *Store, runID domain.TemplateRunID) {
	t.Helper()
	mustSucceed(t, store.ApproveTemplateRun(ctx, domain.TemplateRunApproval{
		RunID: runID, TenantID: "tenant_123", ApprovedBy: "approver_456", ApprovedAt: time.Now(),
	}))
}

// planToApproval takes an apply run from queued through a plan with changes
// to waiting for approval.
func planToApproval(t *testing.T, ctx context.Context, store *Store, runID domain.TemplateRunID) {
	t.Helper()
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite(runID, domain.OperationApply, domain.TemplateRunRunning, domain.RunPhasePlan)))
	recordSteps(t, ctx, store, runID, domain.TemplateRunStepPlanning, domain.TemplateRunStepSavingPlan)
	outcome, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: runID, StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, HasChanges: true, Summary: domain.PlanSummary{Add: 3, Change: 1},
	})
	if err != nil || outcome != domain.PlanOutcomeWaiting {
		t.Fatalf("FinishTemplatePlan = %q, %v; want waiting", outcome, err)
	}
}

// An approved apply reads the step of whichever execution started last, and
// the plan's counts throughout. Between its claim and its first step, the
// apply has no step, and the run reads none rather than the plan's last.
func TestRunReadsAnApprovedApplyFromItsExecutions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	planned := domain.PlanSummary{Add: 3, Change: 1}

	planToApproval(t, ctx, store, "run_123")
	if run := runAt(t, ctx, store, "run_123"); run.Step != domain.TemplateRunStepSavingPlan || run.PlanSummary == nil || *run.PlanSummary != planned {
		t.Fatalf("waiting run = step %q, summary %#v; want saving_plan and the plan's counts", run.Step, run.PlanSummary)
	}

	approve(t, ctx, store, "run_123")
	if claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply"); err != nil || !claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
	}
	if run := runAt(t, ctx, store, "run_123"); run.Status != domain.TemplateRunRunning || run.Step != "" {
		t.Fatalf("claimed run = %q, step %q; want running with no step", run.Status, run.Step)
	}

	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepWaitingForExecutor)
	if run := runAt(t, ctx, store, "run_123"); run.Step != domain.TemplateRunStepWaitingForExecutor {
		t.Fatalf("step = %q, want waiting_for_executor", run.Step)
	}

	// The apply's own counts do not replace the plan's in plan_summary.
	mustSucceed(t, store.RecordTemplateRunEvent(ctx, domain.TemplateRunEventActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, Event: domain.TemplateRunApplied, Summary: &domain.PlanSummary{Add: 2},
	}))
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationApply, domain.TemplateRunCompleted, domain.RunPhaseApply)))
	run := runAt(t, ctx, store, "run_123")
	if run.Status != domain.TemplateRunCompleted || run.Step != domain.TemplateRunStepWaitingForExecutor ||
		run.ErrorSummary != "" || run.PlanSummary == nil || *run.PlanSummary != planned {
		t.Fatalf("completed run = %#v, want completed at waiting_for_executor with the plan's counts", run)
	}
}

// An apply that fails before its claim reads its own error, and no step: the
// plan's last step is not where the apply failed.
func TestRunReadsAnApplyThatFailedBeforeItsClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	planToApproval(t, ctx, store, "run_123")
	approve(t, ctx, store, "run_123")

	write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunFailed, domain.RunPhaseApply)
	write.ErrorSummary = "template run activity failed: claim failed"
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))

	run := runAt(t, ctx, store, "run_123")
	if run.Status != domain.TemplateRunFailed || run.Step != "" || run.ErrorSummary != write.ErrorSummary {
		t.Fatalf("failed run = %q, step %q, error %q; want failed with no step and the apply's error", run.Status, run.Step, run.ErrorSummary)
	}
}

// An apply that fails after its claim reads the step it failed on and its own
// error, with the plan's counts: what the apply was approved to change.
func TestRunReadsAnApplyThatFailedAfterItsClaim(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	failAfterClaim(t, ctx, store, "run_123", "apply: exit status 1")

	run := runAt(t, ctx, store, "run_123")
	if run.Status != domain.TemplateRunFailed || run.Step != domain.TemplateRunStepApplying || run.ErrorSummary != "apply: exit status 1" ||
		run.PlanSummary == nil || *run.PlanSummary != (domain.PlanSummary{Add: 3, Change: 1}) {
		t.Fatalf("failed run = %#v, want failed while applying with the apply's error and the plan's counts", run)
	}
}

// failAfterClaim takes an apply run from queued through its approved plan and
// its apply's claim, then fails the apply while applying with errorSummary.
func failAfterClaim(t *testing.T, ctx context.Context, store *Store, runID domain.TemplateRunID, errorSummary string) {
	t.Helper()
	planToApproval(t, ctx, store, runID)
	approve(t, ctx, store, runID)
	if claimed, err := store.BeginTemplateApply(ctx, "tenant_123", runID, false, "template-run/tenant_123/"+string(runID)+"/apply"); err != nil || !claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
	}
	recordSteps(t, ctx, store, runID, domain.TemplateRunStepWaitingForExecutor, domain.TemplateRunStepApplying)
	write := statusWrite(runID, domain.OperationApply, domain.TemplateRunFailed, domain.RunPhaseApply)
	write.ErrorSummary = errorSummary
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))
}

// A plan with no changes leaves the run with no counts, which is what the UI
// reads as "No changes".
func TestRunReadsAPlanWithNoChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationPlan, domain.TemplateRunQueued)

	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationPlan, domain.TemplateRunRunning, domain.RunPhasePlan)))
	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepPlanning)
	if _, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123", Operation: domain.OperationPlan,
	}); err != nil {
		t.Fatal(err)
	}
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationPlan, domain.TemplateRunCompleted, domain.RunPhasePlan)))

	run := runAt(t, ctx, store, "run_123")
	if run.PlanSummary != nil || run.Step != domain.TemplateRunStepPlanning {
		t.Fatalf("run = summary %#v, step %q; want no summary, planning", run.PlanSummary, run.Step)
	}
}

// An auto-approved apply has no plan, so its counts are its apply's.
func TestRunReadsAnAutoApprovedApply(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedAutoApprovedRun(t, ctx, pool, "run_auto", domain.TemplateRunQueued)

	if claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_auto", true, "template-run/tenant_123/run_auto/apply"); err != nil || !claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
	}
	recordSteps(t, ctx, store, "run_auto", domain.TemplateRunStepApplying)
	mustSucceed(t, store.RecordTemplateRunEvent(ctx, domain.TemplateRunEventActivityInput{
		TenantID: "tenant_123", RunID: "run_auto", StackTemplateID: "stack_template_123",
		Operation: domain.OperationApply, Event: domain.TemplateRunApplied, Summary: &domain.PlanSummary{Add: 2, Destroy: 1},
	}))

	run := runAt(t, ctx, store, "run_auto")
	if run.PlanSummary == nil || *run.PlanSummary != (domain.PlanSummary{Add: 2, Destroy: 1}) || run.Step != domain.TemplateRunStepApplying {
		t.Fatalf("run = summary %#v, step %q; want the apply's counts, applying", run.PlanSummary, run.Step)
	}
}

// A discarded plan reads canceled, with the plan's last step and counts, and
// no error: nothing failed.
func TestRunReadsADiscardedPlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	planToApproval(t, ctx, store, "run_123")
	if _, err := discardTemplateRun(ctx, pool, domain.TemplateRunDiscard{TenantID: "tenant_123", RunID: "run_123", RequestedBy: "user_123"}); err != nil {
		t.Fatal(err)
	}

	run := runAt(t, ctx, store, "run_123")
	if run.Status != domain.TemplateRunCanceled || run.Step != domain.TemplateRunStepSavingPlan || run.ErrorSummary != "" ||
		run.PlanSummary == nil || *run.PlanSummary != (domain.PlanSummary{Add: 3, Change: 1}) {
		t.Fatalf("discarded run = %#v, want canceled at saving_plan with the plan's counts and no error", run)
	}
}

// The run list derives every run's step, error and counts from that run's own
// executions, in the one query the UI polls: runs of one template in three
// states each read their own.
func TestRunListDerivesEachRunsFieldsFromItsOwnExecutions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)

	// One run is in flight at a time, so each is taken as far as it goes
	// before the next is queued.
	seedPlanRun(t, ctx, pool, "run_plan", domain.OperationPlan, domain.TemplateRunQueued)
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_plan", domain.OperationPlan, domain.TemplateRunRunning, domain.RunPhasePlan)))
	recordSteps(t, ctx, store, "run_plan", domain.TemplateRunStepPlanning)
	if _, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
		TenantID: "tenant_123", RunID: "run_plan", StackTemplateID: "stack_template_123",
		Operation: domain.OperationPlan, HasChanges: true, Summary: domain.PlanSummary{Destroy: 2},
	}); err != nil {
		t.Fatal(err)
	}
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, statusWrite("run_plan", domain.OperationPlan, domain.TemplateRunCompleted, domain.RunPhasePlan)))
	seedPlanRun(t, ctx, pool, "run_failed", domain.OperationApply, domain.TemplateRunQueued)
	failAfterClaim(t, ctx, store, "run_failed", "apply: exit status 1")
	seedPlanRun(t, ctx, pool, "run_waiting", domain.OperationApply, domain.TemplateRunQueued)
	planToApproval(t, ctx, store, "run_waiting")

	type derived struct {
		step    domain.TemplateRunStep
		err     string
		summary domain.PlanSummary
	}
	want := map[domain.TemplateRunID]derived{
		"run_plan":    {domain.TemplateRunStepPlanning, "", domain.PlanSummary{Destroy: 2}},
		"run_failed":  {domain.TemplateRunStepApplying, "apply: exit status 1", domain.PlanSummary{Add: 3, Change: 1}},
		"run_waiting": {domain.TemplateRunStepSavingPlan, "", domain.PlanSummary{Add: 3, Change: 1}},
	}
	runs, err := store.ListTemplateRuns(ctx, "tenant_123", "stack_template_123")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != len(want) {
		t.Fatalf("listed %d runs, want %d", len(runs), len(want))
	}
	for _, run := range runs {
		w, ok := want[run.ID]
		if !ok {
			t.Fatalf("listed unexpected run %s", run.ID)
		}
		if run.Step != w.step || run.ErrorSummary != w.err || run.PlanSummary == nil || *run.PlanSummary != w.summary {
			t.Fatalf("listed %s reads step %q, error %q, summary %#v; want %q, %q, %#v",
				run.ID, run.Step, run.ErrorSummary, run.PlanSummary, w.step, w.err, w.summary)
		}
	}
}

// A run's executions come back in the order they started, each with its steps
// in the order they started, with who ran them and what they counted.
func TestListTemplateRunExecutionsReturnsExecutionsWithTheirSteps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	planToApproval(t, ctx, store, "run_123")
	approve(t, ctx, store, "run_123")
	if _, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply"); err != nil {
		t.Fatal(err)
	}
	recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepWaitingForExecutor)

	executions, err := store.ListTemplateRunExecutions(ctx, "tenant_123", "run_123")
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 2 {
		t.Fatalf("executions = %#v, want plan and apply", executions)
	}
	plan, apply := executions[0], executions[1]
	if plan.Phase != domain.RunPhasePlan || plan.Actor != "user_123" || plan.Status != domain.TemplateRunExecutionSucceeded ||
		plan.FinishedAt.IsZero() || plan.Summary == nil || *plan.Summary != (domain.PlanSummary{Add: 3, Change: 1}) {
		t.Fatalf("plan = %#v, want a succeeded plan by user_123 counting 3/1/0", plan)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Step != domain.TemplateRunStepPlanning || plan.Steps[1].Step != domain.TemplateRunStepSavingPlan {
		t.Fatalf("plan steps = %#v, want planning, saving_plan", plan.Steps)
	}
	if apply.Phase != domain.RunPhaseApply || apply.Actor != "approver_456" || apply.Status != domain.TemplateRunExecutionRunning ||
		!apply.FinishedAt.IsZero() || apply.Summary != nil {
		t.Fatalf("apply = %#v, want a running apply by approver_456", apply)
	}
	if len(apply.Steps) != 1 || apply.Steps[0].Step != domain.TemplateRunStepWaitingForExecutor || apply.Steps[0].Status != domain.TemplateRunExecutionRunning {
		t.Fatalf("apply steps = %#v, want waiting_for_executor running", apply.Steps)
	}
}

// A retried FinishPlan changes nothing. While the run waits, the retry finds
// the write it already made and succeeds. Once the run is approved, or its
// apply has claimed it, the plan is no longer the run's to finish: the retry
// is refused and rolls back, and a running apply keeps running.
func TestFinishTemplatePlanRetryChangesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		advance        func(t *testing.T, ctx context.Context, store *Store)
		wantErr        bool
		wantStatus     domain.TemplateRunStatus
		wantExecutions []domain.TemplateRunExecutionStatus
		wantLastStep   domain.TemplateRunExecutionStatus
	}{
		{
			name:           "waiting",
			advance:        func(*testing.T, context.Context, *Store) {},
			wantStatus:     domain.TemplateRunWaitingApproval,
			wantExecutions: []domain.TemplateRunExecutionStatus{domain.TemplateRunExecutionSucceeded},
			wantLastStep:   domain.TemplateRunExecutionSucceeded,
		},
		{
			name: "approved",
			advance: func(t *testing.T, ctx context.Context, store *Store) {
				approve(t, ctx, store, "run_123")
			},
			wantErr:        true,
			wantStatus:     domain.TemplateRunApproved,
			wantExecutions: []domain.TemplateRunExecutionStatus{domain.TemplateRunExecutionSucceeded},
			wantLastStep:   domain.TemplateRunExecutionSucceeded,
		},
		{
			name: "claimed",
			advance: func(t *testing.T, ctx context.Context, store *Store) {
				approve(t, ctx, store, "run_123")
				if claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply"); err != nil || !claimed {
					t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
				}
				recordSteps(t, ctx, store, "run_123", domain.TemplateRunStepWaitingForExecutor)
			},
			wantErr:        true,
			wantStatus:     domain.TemplateRunRunning,
			wantExecutions: []domain.TemplateRunExecutionStatus{domain.TemplateRunExecutionSucceeded, domain.TemplateRunExecutionRunning},
			wantLastStep:   domain.TemplateRunExecutionRunning,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			pool := openMigratedTestPool(t, ctx)
			store := savedPlanStore(t, pool)
			seedStackWithTemplate(t, ctx, store)
			seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
			planToApproval(t, ctx, store, "run_123")
			test.advance(t, ctx, store)

			outcome, err := store.FinishTemplatePlan(ctx, domain.FinishPlanActivityInput{
				TenantID: "tenant_123", RunID: "run_123", StackTemplateID: "stack_template_123",
				Operation: domain.OperationApply, HasChanges: true, Summary: domain.PlanSummary{Add: 3, Change: 1},
			})
			if test.wantErr && err == nil {
				t.Fatalf("FinishTemplatePlan = %q, nil; want refused", outcome)
			}
			if !test.wantErr && (err != nil || outcome != domain.PlanOutcomeWaiting) {
				t.Fatalf("FinishTemplatePlan = %q, %v; want waiting", outcome, err)
			}

			if got := runStatus(t, ctx, pool, "run_123"); got != test.wantStatus {
				t.Fatalf("status = %q, want it left %q", got, test.wantStatus)
			}
			rows := workflowExecutionRows(t, ctx, pool, "run_123")
			var statuses []domain.TemplateRunExecutionStatus
			for _, row := range rows {
				statuses = append(statuses, row.Status)
			}
			if !reflect.DeepEqual(statuses, test.wantExecutions) {
				t.Fatalf("executions = %v, want %v", statuses, test.wantExecutions)
			}
			steps := stepExecutionRows(t, ctx, pool, "run_123")
			if last := steps[len(steps)-1]; last.Status != test.wantLastStep {
				t.Fatalf("last step %q = %q, want %q", last.Step, last.Status, test.wantLastStep)
			}
		})
	}
}

// A plan workflow whose FinishPlan acknowledgement was lost fails after its
// plan already finished. The plan did its work: the run keeps waiting, or
// stays approved, and the late failure changes nothing.
func TestLateFailureOfAFinishedPlanChangesNothing(t *testing.T) {
	t.Parallel()

	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting", true: "approved"}[approved], func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			pool := openMigratedTestPool(t, ctx)
			store := savedPlanStore(t, pool)
			seedStackWithTemplate(t, ctx, store)
			seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
			planToApproval(t, ctx, store, "run_123")
			want := domain.TemplateRunWaitingApproval
			if approved {
				approve(t, ctx, store, "run_123")
				want = domain.TemplateRunApproved
			}

			write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunFailed, domain.RunPhasePlan)
			write.ErrorSummary = "template run activity failed: FinishPlan timed out"
			mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))

			if got := runStatus(t, ctx, pool, "run_123"); got != want {
				t.Fatalf("status = %q, want it left %q", got, want)
			}
			rows := workflowExecutionRows(t, ctx, pool, "run_123")
			if len(rows) != 1 || rows[0].Status != domain.TemplateRunExecutionSucceeded || rows[0].ErrorSummary != "" {
				t.Fatalf("executions = %#v, want the plan still succeeded", rows)
			}
		})
	}
}

// A plan workflow's late failure must not touch the apply that has since
// claimed the run: that execution is the apply workflow's to end.
func TestLateFailureOfAPlanLeavesTheApplyRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunQueued)
	planToApproval(t, ctx, store, "run_123")
	approve(t, ctx, store, "run_123")
	if _, err := store.CreatePlanKey(ctx, "tenant_123", "run_123"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.BeginTemplateApply(ctx, "tenant_123", "run_123", false, "template-run/tenant_123/run_123/apply"); err != nil || !claimed {
		t.Fatalf("BeginTemplateApply = %v, %v; want claimed", claimed, err)
	}

	write := statusWrite("run_123", domain.OperationApply, domain.TemplateRunFailed, domain.RunPhasePlan)
	write.ErrorSummary = "template run activity failed: FinishPlan timed out"
	mustSucceed(t, store.RecordTemplateRunStatus(ctx, write))

	if got := runStatus(t, ctx, pool, "run_123"); got != domain.TemplateRunRunning {
		t.Fatalf("status = %q, want the apply still running", got)
	}
	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 2 || rows[1].Phase != domain.RunPhaseApply || rows[1].Status != domain.TemplateRunExecutionRunning {
		t.Fatalf("executions = %#v, want the apply still running", rows)
	}
	if planKeyIsNull(t, ctx, pool, "run_123") {
		t.Fatal("the late plan failure dropped the apply's plan key")
	}
}

// A terminal status write must say which execution it belongs to: without a
// phase it cannot tell its own execution from another workflow's.
func TestTerminalStatusWriteNeedsAPhase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := savedPlanStore(t, pool)
	seedStackWithTemplate(t, ctx, store)
	seedPlanRun(t, ctx, pool, "run_123", domain.OperationApply, domain.TemplateRunRunning)

	if err := store.RecordTemplateRunStatus(ctx, statusWrite("run_123", domain.OperationApply, domain.TemplateRunCompleted, "")); err == nil {
		t.Fatal("a completed write without a phase returned nil")
	}
	if got := runStatus(t, ctx, pool, "run_123"); got != domain.TemplateRunRunning {
		t.Fatalf("status = %q, want running", got)
	}
}
