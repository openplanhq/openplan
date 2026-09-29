package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vishu42/openplan/internal/domain"
)

// RecordTemplateRunStep records the step a running run has started, as a step
// of its running workflow execution, and ends the step before it: a step runs
// until the next one starts. Only a running run has a step to start, so any
// other run is not found.
//
// Writing the step the execution is already on changes nothing: it is this
// write retried after its acknowledgement was lost. That check comes first,
// because ending the running step would otherwise end the retried step's own
// row. A step the execution already finished is an error, not a retry: each
// step runs once per execution, and a second row would corrupt its timeline.
func (store *Store) RecordTemplateRunStep(ctx context.Context, input domain.TemplateRunStepActivityInput) error {
	if !input.Step.Valid() {
		return fmt.Errorf("record template run step: unknown step %q", input.Step)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin record template run step: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var executionID int64
	err = tx.QueryRow(ctx, `
		select e.id
		from template_run_workflow_executions e
		join template_runs r on r.tenant_id = e.tenant_id and r.id = e.run_id
		where e.tenant_id = $1
			and e.run_id = $2
			and e.status = 'running'
			and r.status = $3
		for update of e
	`, input.TenantID, input.RunID, domain.TemplateRunRunning).Scan(&executionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read running workflow execution: %w", err)
	}

	var existing domain.TemplateRunExecutionStatus
	err = tx.QueryRow(ctx, `
		select status from template_run_step_executions
		where workflow_execution_id = $1 and step = $2
	`, executionID, input.Step).Scan(&existing)
	switch {
	case err == nil && existing == domain.TemplateRunExecutionRunning:
		return nil
	case err == nil:
		return fmt.Errorf("record template run step: %q already ran in this workflow execution", input.Step)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read template run step: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update template_run_step_executions
		set status = $2, finished_at = now()
		where workflow_execution_id = $1 and status = 'running'
	`, executionID, domain.TemplateRunExecutionSucceeded); err != nil {
		return fmt.Errorf("end previous template run step: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into template_run_step_executions (workflow_execution_id, run_id, tenant_id, step, status, started_at)
		values ($1, $2, $3, $4, $5, now())
	`, executionID, input.RunID, input.TenantID, input.Step, domain.TemplateRunExecutionRunning); err != nil {
		return fmt.Errorf("record template run step: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit template run step: %w", err)
	}
	return nil
}

// templateRunEventOperations is the one kind of run each event belongs to.
var templateRunEventOperations = map[domain.TemplateRunEvent]domain.OperationType{
	domain.TemplateRunApplied:    domain.OperationApply,
	domain.TemplateRunDestroying: domain.OperationDestroy,
	domain.TemplateRunDestroyed:  domain.OperationDestroy,
}

// RecordTemplateRunEvent records something a running run did to its stack
// template, together with any counts the event carries, in one transaction
// with the run row locked. A run that is not running, or is not this stack
// template's run of this operation, is not found.
func (store *Store) RecordTemplateRunEvent(ctx context.Context, input domain.TemplateRunEventActivityInput) error {
	operation, ok := templateRunEventOperations[input.Event]
	if !ok {
		return fmt.Errorf("record template run event: unknown event %q", input.Event)
	}
	if input.Operation != operation {
		return fmt.Errorf("record template run event: %q is not an event of a %s run", input.Event, input.Operation)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin record template run event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var runID domain.TemplateRunID
	err = tx.QueryRow(ctx, `
		select id
		from template_runs
		where tenant_id = $1
			and id = $2
			and stack_template_id = $3
			and operation = $4
			and status = $5
		for update
	`, input.TenantID, input.RunID, input.StackTemplateID, input.Operation, domain.TemplateRunRunning).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read template run for event: %w", err)
	}

	if input.Summary != nil {
		if err := recordRunningCounts(ctx, tx, input.TenantID, input.RunID, *input.Summary); err != nil {
			return err
		}
	}

	switch input.Event {
	case domain.TemplateRunApplied:
		err = recordStackTemplateLastApplied(ctx, tx, input.TenantID, input.StackTemplateID, input.RunID, domain.TemplateRunRunning)
	case domain.TemplateRunDestroying:
		err = recordStackTemplateLifecycle(ctx, tx, input.TenantID, input.StackTemplateID, domain.StackTemplateDestroying)
	case domain.TemplateRunDestroyed:
		err = recordStackTemplateLifecycle(ctx, tx, input.TenantID, input.StackTemplateID, domain.StackTemplateDestroyed)
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record template run event: %w", err)
	}
	return nil
}
