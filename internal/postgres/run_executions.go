package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vishu42/openplan/internal/domain"
)

// A run's workflow executions are written in the same transaction as the run
// change that starts or ends them, so a run is running exactly when one of
// its executions is.

// insertWorkflowExecution records a workflow execution of the run as phase,
// in status: running for a workflow that has claimed the run, failed for one
// that failed before it could. Its actor is whoever approved the run for an
// approved apply, and whoever started the run otherwise.
//
// It reports whether it inserted. The run already having an execution of the
// phase is not an error here: a retried start finds the row it wrote the
// first time.
func insertWorkflowExecution(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, phase domain.RunPhase, workflowID string, status domain.TemplateRunExecutionStatus, errorSummary string) (bool, error) {
	if !phase.Valid() {
		return false, fmt.Errorf("record workflow execution: unknown phase %q", phase)
	}
	if workflowID == "" {
		return false, fmt.Errorf("record workflow execution: workflow ID is required")
	}
	commandTag, err := exec.Exec(ctx, `
		insert into template_run_workflow_executions (
			tenant_id, run_id, phase, workflow_id, actor, status, started_at, finished_at, error_summary
		)
		select
			r.tenant_id, r.id, $3::text, $4::text,
			coalesce(a.approved_by, r.trigger_actor),
			$5::text, now(),
			case when $5::text = 'running' then null else now() end,
			$6::text
		from template_runs r
		left join template_run_approvals a
			on a.tenant_id = r.tenant_id and a.run_id = r.id and $3::text = 'apply'
		where r.tenant_id = $1 and r.id = $2
		on conflict (tenant_id, run_id, phase) do nothing
	`, tenantID, runID, phase, workflowID, status, errorSummary)
	if err != nil {
		return false, fmt.Errorf("record workflow execution: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

// finishRunningExecution ends the run's running workflow execution of phase
// as status, with errorSummary, and the step it was on the same way. It
// reports whether the run had one: another phase's running execution is not
// this write's to end.
func finishRunningExecution(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, phase domain.RunPhase, status domain.TemplateRunExecutionStatus, errorSummary string) (bool, error) {
	if _, err := exec.Exec(ctx, `
		update template_run_step_executions s
		set status = $4, finished_at = now()
		from template_run_workflow_executions e
		where e.tenant_id = $1 and e.run_id = $2 and e.phase = $3 and e.status = 'running'
			and s.workflow_execution_id = e.id and s.status = 'running'
	`, tenantID, runID, phase, status); err != nil {
		return false, fmt.Errorf("end template run step: %w", err)
	}
	commandTag, err := exec.Exec(ctx, `
		update template_run_workflow_executions
		set status = $4, finished_at = now(), error_summary = $5
		where tenant_id = $1 and run_id = $2 and phase = $3 and status = 'running'
	`, tenantID, runID, phase, status, errorSummary)
	if err != nil {
		return false, fmt.Errorf("finish workflow execution: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

// endWorkflowExecution ends the input phase's workflow execution as the run
// becomes terminal: succeeded with a completed run, failed with a failed one,
// taking the run's error. It reports whether the write is this workflow's to
// make; when it is not, it has written nothing, and the run must not move.
//
// A write belongs to its own phase only. A plan workflow whose FinishPlan
// acknowledgement was lost fails after its plan already ended: by then the
// run waits for approval, or its apply has claimed it, and neither is the
// plan's to end. That late write changes nothing, as an apply that lost its
// claim does.
//
// A run that fails with no execution running, and none of the phase yet,
// failed before its workflow could claim it. It gets a failed execution of
// the phase, so its error has a row. A run that completes must have its
// phase's execution running.
func endWorkflowExecution(ctx context.Context, exec pgxExecutor, input domain.TemplateRunStatusActivityInput) (bool, error) {
	status := domain.TemplateRunExecutionSucceeded
	if input.Status == domain.TemplateRunFailed {
		status = domain.TemplateRunExecutionFailed
	}

	var runningPhase domain.RunPhase
	err := exec.QueryRow(ctx, `
		select phase from template_run_workflow_executions
		where tenant_id = $1 and run_id = $2 and status = 'running'
	`, input.TenantID, input.RunID).Scan(&runningPhase)
	switch {
	case err == nil && runningPhase != input.Phase:
		return false, nil
	case err == nil:
		_, err := finishRunningExecution(ctx, exec, input.TenantID, input.RunID, input.Phase, status, input.ErrorSummary)
		return err == nil, err
	case !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("read running workflow execution: %w", err)
	}

	if input.Status != domain.TemplateRunFailed {
		return false, fmt.Errorf("record template run status: run %s has no running %s workflow execution to complete", input.RunID, input.Phase)
	}
	inserted, err := insertWorkflowExecution(ctx, exec, input.TenantID, input.RunID, input.Phase, input.WorkflowID, domain.TemplateRunExecutionFailed, input.ErrorSummary)
	if err != nil {
		return false, err
	}
	// Not inserted: the phase's execution already ended, so this is that
	// workflow failing late, after its work was done.
	return inserted, nil
}

// recordPlanCounts records what the run's plan would change on its plan
// execution. It targets the phase, not the running execution, so a retried
// FinishPlan still finds the row after the plan has finished.
func recordPlanCounts(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, summary domain.PlanSummary) error {
	commandTag, err := exec.Exec(ctx, `
		update template_run_workflow_executions
		set resource_add = $3, resource_change = $4, resource_destroy = $5
		where tenant_id = $1 and run_id = $2 and phase = 'plan'
	`, tenantID, runID, summary.Add, summary.Change, summary.Destroy)
	if err != nil {
		return fmt.Errorf("record plan counts: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("record plan counts: run %s has no plan execution", runID)
	}
	return nil
}

// recordRunningCounts records counts on the run's running workflow execution:
// what its apply did.
func recordRunningCounts(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, summary domain.PlanSummary) error {
	commandTag, err := exec.Exec(ctx, `
		update template_run_workflow_executions
		set resource_add = $3, resource_change = $4, resource_destroy = $5
		where tenant_id = $1 and run_id = $2 and status = 'running'
	`, tenantID, runID, summary.Add, summary.Change, summary.Destroy)
	if err != nil {
		return fmt.Errorf("record execution counts: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("record execution counts: run %s has no running workflow execution", runID)
	}
	return nil
}

// ListTemplateRunExecutions returns the workflow executions that carried the
// run, in the order they started, each with its steps in the order they
// started.
func (store *Store) ListTemplateRunExecutions(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	// Both reads see one snapshot. Otherwise a claim and its first step
	// committing between them would return a step of an execution not read,
	// and a step ending with its execution would read ahead of it.
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin list workflow executions: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		select id, phase, workflow_id, actor, status, started_at, finished_at, error_summary,
			resource_add, resource_change, resource_destroy
		from template_run_workflow_executions
		where tenant_id = $1 and run_id = $2
		order by id
	`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("list workflow executions: %w", err)
	}
	executions := []domain.TemplateRunWorkflowExecution{}
	positions := map[int64]int{}
	for rows.Next() {
		var id int64
		var execution domain.TemplateRunWorkflowExecution
		var finishedAt sql.NullTime
		var add, change, destroy *int
		if err := rows.Scan(&id, &execution.Phase, &execution.WorkflowID, &execution.Actor, &execution.Status,
			&execution.StartedAt, &finishedAt, &execution.ErrorSummary, &add, &change, &destroy); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan workflow execution: %w", err)
		}
		if finishedAt.Valid {
			execution.FinishedAt = finishedAt.Time
		}
		execution.Summary = planSummary(add, change, destroy)
		execution.Steps = []domain.TemplateRunStepExecution{}
		positions[id] = len(executions)
		executions = append(executions, execution)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list workflow executions: %w", err)
	}

	stepRows, err := tx.Query(ctx, `
		select workflow_execution_id, step, status, started_at, finished_at
		from template_run_step_executions
		where tenant_id = $1 and run_id = $2
		order by id
	`, tenantID, runID)
	if err != nil {
		return nil, fmt.Errorf("list step executions: %w", err)
	}
	defer stepRows.Close()
	for stepRows.Next() {
		var executionID int64
		var step domain.TemplateRunStepExecution
		var finishedAt sql.NullTime
		if err := stepRows.Scan(&executionID, &step.Step, &step.Status, &step.StartedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("scan step execution: %w", err)
		}
		if finishedAt.Valid {
			step.FinishedAt = finishedAt.Time
		}
		position, ok := positions[executionID]
		if !ok {
			return nil, fmt.Errorf("list step executions: step of unknown execution %d", executionID)
		}
		executions[position].Steps = append(executions[position].Steps, step)
	}
	if err := stepRows.Err(); err != nil {
		return nil, fmt.Errorf("list step executions: %w", err)
	}
	return executions, nil
}
