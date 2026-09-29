# Template Run Executions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give each workflow that carries a template run, and each step it runs, a row of its own, so a run's plan and apply stop overwriting each other and every step has a start and a finish.

**Architecture:** Two new tables, `template_run_workflow_executions` and `template_run_step_executions`, are written in the same transaction as the run status change or step that starts or ends them. `template_runs` keeps the run's intent and lifecycle; its `step`, `error_summary` and `plan_*` columns go, and reads derive those fields from the executions with lateral subqueries. The single-run API returns the executions with their steps.

**Tech Stack:** Go, PostgreSQL via pgx v5, Temporal Go SDK (workflows and testsuite), OpenTofu runner, React/TypeScript (field rename only), OpenAPI 3.1.

**Spec:** `docs/superpowers/specs/2026-09-28-template-run-executions-design.md`

## Global Constraints

- Work on a branch `feat/run-executions` cut from `main`. Never commit to `main`.
- Commit only when the user has asked for commits in this session. Each task's last step is a checkpoint: run its checks, then commit if the user said to, otherwise leave the work in the tree and continue.
- Table, column, constraint and index names are exactly the spec's. Rows are ordered by their identity `id`, never by a timestamp.
- `0029_template_run_executions.sql` is the only new migration. Tasks 1, 4 and 6 each add to it: it is unreleased, and every store test builds a fresh schema.
- openplan is pre-production: no backfill, no close-out of old runs, no compatibility shims. A database with runs in flight is reset (`docker compose down -v`), never migrated in place.
- Store tests need the Compose Postgres and skip silently without it. Before any store test run: `docker compose up -d postgres` and `export OPENPLAN_POSTGRES_TEST_DSN='postgres://openplan:openplan@localhost:55432/openplan_test?sslmode=disable'`. Run them with `-v` and check that the output has no `--- SKIP`; a skipped test is not a passing one.
- Format with `gofmt -w $(rg --files cmd internal -g '*.go')` and lint with `make lint` before each checkpoint.
- The step check constraint must list exactly `domain.AllTemplateRunSteps`.

## Review Focus

- A retried step write, whose step is already the execution's running step, must change nothing. It must not close its own row. Pinned in Task 3.
- An apply that has claimed its run but not started a step reads `step: ""`, not the plan's last step, and an apply that fails before its claim reads its own error with `step: ""`. Pinned in Task 4.
- A plan with no changes leaves `plan_summary` null, or the UI says "Plan finished" instead of "No changes". Pinned in Task 4.
- The run list is polled every 1.5 s and must derive the same fields as a single-run read, for every run in the list, in one query. Pinned in Task 4 by `runAt`, which compares the two for every derived-field test.
- A discarded plan reads canceled, with the plan's last step, no error, and the plan's counts. Pinned in Task 4.

---

### Task 1: Execution tables and domain types

**Files:**
- Create: `internal/postgres/migrations/0029_template_run_executions.sql`
- Modify: `internal/domain/template_run.go` (add types after the `TemplateRunEvent` constants, before `// TemplateRun is one Terraform operation`)
- Modify: `internal/domain/workflow.go:224-231` (`RunPhase`)
- Modify: `internal/domain/domain_test.go`
- Create: `internal/postgres/run_executions_test.go`

**Interfaces:**
- Produces: `domain.TemplateRunExecutionStatus` with `TemplateRunExecutionRunning`, `TemplateRunExecutionSucceeded`, `TemplateRunExecutionFailed`.
- Produces: `domain.TemplateRunWorkflowExecution{Phase RunPhase; WorkflowID string; Actor UserID; Status TemplateRunExecutionStatus; StartedAt, FinishedAt time.Time; ErrorSummary string; Summary *PlanSummary; Steps []TemplateRunStepExecution}`.
- Produces: `domain.TemplateRunStepExecution{Step TemplateRunStep; Status TemplateRunExecutionStatus; StartedAt, FinishedAt time.Time}`.
- Produces: `func (phase domain.RunPhase) Valid() bool`.
- Produces (tests, package `postgres`): `violates(err error, code, constraint string) bool`, `insertWorkflowExecutionRow(ctx, pool, runID, phase, status) (int64, error)`, `insertStepExecutionRow(ctx, pool, executionID int64, runID, step, status) error`, `workflowExecutionRows(t, ctx, pool, runID) []workflowExecutionRow`, `stepExecutionRows(t, ctx, pool, runID) []stepExecutionRow`.

- [ ] **Step 1: Write the failing domain test**

Append to `internal/domain/domain_test.go`:

```go
func TestRunPhaseValid(t *testing.T) {
	for _, phase := range []RunPhase{RunPhasePlan, RunPhaseApply} {
		if !phase.Valid() {
			t.Fatalf("%q.Valid() = false, want true", phase)
		}
	}
	for _, phase := range []RunPhase{"", "destroy"} {
		if phase.Valid() {
			t.Fatalf("%q.Valid() = true, want false", phase)
		}
	}
}
```

- [ ] **Step 2: Write the failing constraint tests**

Create `internal/postgres/run_executions_test.go`:

```go
package postgres

import (
	"context"
	"errors"
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/domain -run TestRunPhaseValid -count=1` and `go test ./internal/postgres -run 'TestWorkflowExecutions|TestStepExecutions' -count=1 -v`
Expected: compile failure, `undefined: domain.TemplateRunExecutionStatus` (and `phase.Valid undefined` in `internal/domain`).

- [ ] **Step 4: Add the domain types**

In `internal/domain/workflow.go`, after the `RunPhase` constants:

```go
// Valid reports whether the phase is one a run has.
func (phase RunPhase) Valid() bool {
	return phase == RunPhasePlan || phase == RunPhaseApply
}
```

In `internal/domain/template_run.go`, before `// TemplateRun is one Terraform operation against a StackTemplate.`:

```go
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
```

- [ ] **Step 5: Write the migration**

Create `internal/postgres/migrations/0029_template_run_executions.sql`:

```sql
-- A run is carried by up to two workflows, its plan and its apply, and each
-- runs a series of steps. template_runs holds one value per field, so the
-- apply overwrote what the plan recorded and each step overwrote the one
-- before it. These two tables give each workflow execution and each step a
-- row of its own, written in the same transaction as the run change that
-- starts or ends it.
--
-- Rows are ordered by id, never by a timestamp: each is written in its own
-- transaction, so now() can tie or step backwards with the clock.

create table template_run_workflow_executions (
	id               bigint      generated always as identity primary key,
	tenant_id        text        not null,
	run_id           text        not null references template_runs (id) on delete cascade,
	phase            text        not null,
	workflow_id      text        not null,
	actor            text        not null,
	status           text        not null,
	started_at       timestamptz not null,
	finished_at      timestamptz,
	error_summary    text        not null default '',
	resource_add     integer,
	resource_change  integer,
	resource_destroy integer,
	constraint template_run_workflow_executions_phase_key unique (tenant_id, run_id, phase),
	constraint template_run_workflow_executions_id_run_key unique (id, run_id),
	constraint template_run_workflow_executions_phase_check check (phase in ('plan', 'apply')),
	constraint template_run_workflow_executions_status_check check (status in ('running', 'succeeded', 'failed')),
	constraint template_run_workflow_executions_finished_at_check check ((status = 'running') = (finished_at is null)),
	constraint template_run_workflow_executions_counts_check check (
		num_nonnulls(resource_add, resource_change, resource_destroy) in (0, 3)
	)
);

-- A run is carried by one workflow at a time.
create unique index template_run_workflow_executions_one_running_idx
	on template_run_workflow_executions (tenant_id, run_id)
	where status = 'running';

-- The step check must stay equal to domain.AllTemplateRunSteps.
create table template_run_step_executions (
	id                    bigint      generated always as identity primary key,
	workflow_execution_id bigint      not null,
	run_id                text        not null,
	tenant_id             text        not null,
	step                  text        not null,
	status                text        not null,
	started_at            timestamptz not null,
	finished_at           timestamptz,
	constraint template_run_step_executions_step_key unique (workflow_execution_id, step),
	constraint template_run_step_executions_execution_fkey
		foreign key (workflow_execution_id, run_id)
		references template_run_workflow_executions (id, run_id) on delete cascade,
	constraint template_run_step_executions_step_check check (step in (
		'waiting_for_executor', 'preparing_workspace', 'fetching_source',
		'restoring_plan', 'initializing', 'selecting_workspace', 'planning',
		'saving_plan', 'applying'
	)),
	constraint template_run_step_executions_status_check check (status in ('running', 'succeeded', 'failed')),
	constraint template_run_step_executions_finished_at_check check ((status = 'running') = (finished_at is null))
);

-- An execution is on one step at a time.
create unique index template_run_step_executions_one_running_idx
	on template_run_step_executions (workflow_execution_id)
	where status = 'running';

-- A run's whole timeline, in order, is one range read.
create index template_run_step_executions_run_idx
	on template_run_step_executions (tenant_id, run_id, id);
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/domain -count=1` and `go test ./internal/postgres -run 'TestWorkflowExecutions|TestStepExecutions' -count=1 -v`
Expected: PASS, no `--- SKIP`.

- [ ] **Step 7: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git add internal/domain internal/postgres && git commit -m "feat: add template run execution tables"`.

---

### Task 2: Workflow executions are written with the run

**Files:**
- Modify: `internal/domain/workflow.go:116-123` (`TemplateRunStatusActivityInput`), `:322-326` (`BeginApplyActivityInput`)
- Create: `internal/postgres/run_executions.go`
- Modify: `internal/postgres/repositories.go:1372-1435` (`RecordTemplateRunStatus`)
- Modify: `internal/postgres/saved_plans.go:96-208` (`FinishTemplatePlan`, `BeginTemplateApply`)
- Modify: `internal/postgres/run_progress.go:78-86` (`RecordTemplateRunEvent` counts)
- Modify: `internal/activities/control.go:30-39,168-175` (`PlanRecorder`, `BeginApply`)
- Modify: `internal/activities/template_run_test.go:1021`, `cmd/api/main_test.go:833` (stub signatures)
- Modify: `internal/workflows/template_run.go` (`run`, `newRun`, both workflows, `setStatus`, apply claim)
- Modify: `internal/postgres/store_test.go:2849` (`seedTemplateRun`)
- Modify: `internal/postgres/saved_plans_test.go` (five `BeginTemplateApply` calls)
- Test: `internal/postgres/run_executions_test.go`, `internal/postgres/run_progress_test.go`, `internal/workflows/template_run_workflow_test.go`

**Interfaces:**
- Consumes: Task 1's types, `RunPhase.Valid`, test helpers.
- Produces: `TemplateRunStatusActivityInput.Phase domain.RunPhase`, `.WorkflowID string`; `BeginApplyActivityInput.WorkflowID string`.
- Produces: `func (store *Store) BeginTemplateApply(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, autoApprove bool, workflowID string) (bool, error)`.
- Produces (package `postgres`): `insertWorkflowExecution(ctx, exec pgxExecutor, tenantID, runID, phase domain.RunPhase, workflowID string, status domain.TemplateRunExecutionStatus, errorSummary string) (bool, error)`, `finishRunningExecution(ctx, exec, tenantID, runID, status, errorSummary) (bool, error)`, `endWorkflowExecution(ctx, exec, input domain.TemplateRunStatusActivityInput) error`, `recordPlanCounts(ctx, exec, tenantID, runID, summary domain.PlanSummary) error`, `recordRunningCounts(ctx, exec, tenantID, runID, summary domain.PlanSummary) error`.
- Produces (tests): `statusWrite(runID, operation, status, phase) domain.TemplateRunStatusActivityInput`, `mustSucceed(t, err)`.

- [ ] **Step 1: Write the failing store tests**

Append to `internal/postgres/run_executions_test.go` (its imports already cover these):

```go
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
```

In `internal/postgres/run_progress_test.go`, at the end of `TestRecordTemplateRunEventRecordsTheCountsItCarries`, append:

```go
	rows := workflowExecutionRows(t, ctx, pool, "run_123")
	if len(rows) != 1 || rows[0].Add == nil || *rows[0].Add != 2 || *rows[0].Change != 1 || *rows[0].Destroy != 0 {
		t.Fatalf("executions = %#v, want the apply execution holding 2/1/0", rows)
	}
```

- [ ] **Step 2: Write the failing workflow test**

In `internal/workflows/template_run_workflow_test.go`, add `"go.temporal.io/sdk/client"` to the imports, and add:

```go
// Each workflow tells the control plane which execution of the run it is,
// and which Temporal workflow carries it, on every status write and on its
// claim.
func TestTemplateRunWorkflowsRecordTheirPhaseAndWorkflowID(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		workflow   any
		operation  domain.OperationType
		phase      domain.RunPhase
		workflowID string
	}{
		{name: "plan", workflow: TemplatePlanWorkflow, operation: domain.OperationPlan, phase: domain.RunPhasePlan, workflowID: "template-run/tenant_123/run_123"},
		{name: "apply", workflow: TemplateApplyWorkflow, operation: domain.OperationApply, phase: domain.RunPhaseApply, workflowID: "template-run/tenant_123/run_123/apply"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: testCase.workflowID})
			var statuses []domain.TemplateRunStatusActivityInput
			var claims []domain.BeginApplyActivityInput
			env.OnActivity(domain.RecordTemplateRunStatusActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.TemplateRunStatusActivityInput) error {
					statuses = append(statuses, input)
					return nil
				})
			env.OnActivity(domain.BeginApplyActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.BeginApplyActivityInput) (domain.BeginApplyActivityOutput, error) {
					claims = append(claims, input)
					return domain.BeginApplyActivityOutput{Claimed: true}, nil
				})

			env.ExecuteWorkflow(testCase.workflow, templateRunWorkflowInput(testCase.operation))

			assertWorkflowCompleted(t, env)
			if len(statuses) == 0 {
				t.Fatal("no status write")
			}
			for _, status := range statuses {
				if status.Phase != testCase.phase || status.WorkflowID != testCase.workflowID {
					t.Fatalf("status write = %#v, want phase %q and workflow %q", status, testCase.phase, testCase.workflowID)
				}
			}
			if testCase.phase == domain.RunPhaseApply && (len(claims) != 1 || claims[0].WorkflowID != testCase.workflowID) {
				t.Fatalf("claims = %#v, want one by %q", claims, testCase.workflowID)
			}
		})
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/postgres ./internal/workflows -count=1`
Expected: compile failure, `unknown field Phase in struct literal of type domain.TemplateRunStatusActivityInput`, and `too many arguments in call to store.BeginTemplateApply`.

- [ ] **Step 4: Add the input fields**

In `internal/domain/workflow.go`, replace `TemplateRunStatusActivityInput` and `BeginApplyActivityInput` with:

```go
type TemplateRunStatusActivityInput struct {
	RunID           TemplateRunID
	TenantID        TenantID
	StackTemplateID StackTemplateID
	Operation       OperationType
	Status          TemplateRunStatus
	ErrorSummary    string
	// Phase is the workflow execution the write belongs to: the one a running
	// write starts, and the one a failed write records when its workflow
	// failed before it could claim the run.
	Phase RunPhase
	// WorkflowID is the Temporal workflow making the write, recorded on the
	// execution the write starts or records.
	WorkflowID string
}
```

```go
type BeginApplyActivityInput struct {
	TenantID    TenantID
	RunID       TemplateRunID
	AutoApprove bool
	// WorkflowID is the apply workflow claiming the run, recorded on the
	// apply execution the claim starts.
	WorkflowID string
}
```

- [ ] **Step 5: Write the execution store helpers**

Create `internal/postgres/run_executions.go`:

```go
package postgres

import (
	"context"
	"fmt"

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

// finishRunningExecution ends the run's running workflow execution as status,
// with errorSummary, and reports whether the run had one.
func finishRunningExecution(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, status domain.TemplateRunExecutionStatus, errorSummary string) (bool, error) {
	commandTag, err := exec.Exec(ctx, `
		update template_run_workflow_executions
		set status = $3, finished_at = now(), error_summary = $4
		where tenant_id = $1 and run_id = $2 and status = 'running'
	`, tenantID, runID, status, errorSummary)
	if err != nil {
		return false, fmt.Errorf("finish workflow execution: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

// endWorkflowExecution ends the run's running workflow execution as the run
// becomes terminal: succeeded with a completed run, failed with a failed one,
// taking the run's error.
//
// A run that fails with no execution running failed before its workflow could
// claim it. It gets a failed execution of the input's phase, so its error has
// a row. A run that completes must have one running.
func endWorkflowExecution(ctx context.Context, exec pgxExecutor, input domain.TemplateRunStatusActivityInput) error {
	status := domain.TemplateRunExecutionSucceeded
	if input.Status == domain.TemplateRunFailed {
		status = domain.TemplateRunExecutionFailed
	}
	ended, err := finishRunningExecution(ctx, exec, input.TenantID, input.RunID, status, input.ErrorSummary)
	if err != nil || ended {
		return err
	}
	if input.Status != domain.TemplateRunFailed {
		return fmt.Errorf("record template run status: run %s has no running workflow execution to complete", input.RunID)
	}
	inserted, err := insertWorkflowExecution(ctx, exec, input.TenantID, input.RunID, input.Phase, input.WorkflowID, domain.TemplateRunExecutionFailed, input.ErrorSummary)
	if err != nil {
		return err
	}
	if !inserted {
		return fmt.Errorf("record template run status: run %s already has a %s workflow execution", input.RunID, input.Phase)
	}
	return nil
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
```

- [ ] **Step 6: Write executions from the status write, the claim, the plan and the event**

In `RecordTemplateRunStatus` (`internal/postgres/repositories.go`), replace the non-terminal branch with:

```go
	if !input.Status.Terminal() {
		if _, err := tx.Exec(ctx, `
			update template_runs set status = $1 where tenant_id = $2 and id = $3
		`, input.Status, input.TenantID, input.RunID); err != nil {
			return fmt.Errorf("record template run status: %w", err)
		}
		// Running is the only status recorded here, and only the plan
		// workflow records it: an apply claims its run with
		// BeginTemplateApply. The plan's execution starts with it.
		if _, err := insertWorkflowExecution(ctx, tx, input.TenantID, input.RunID, input.Phase, input.WorkflowID, domain.TemplateRunExecutionRunning, ""); err != nil {
			return err
		}
		return commitTemplateRunStatus(ctx, tx)
	}
```

and, in the terminal path, directly after the `update template_runs set status = $1, error_summary = …, completed_at = …` statement and its error check, add:

```go
	if err := endWorkflowExecution(ctx, tx, input); err != nil {
		return err
	}
```

Add to the function's doc comment, after "Becoming terminal sets completed_at, …":

```go
// The run's workflow execution moves with it: running starts the plan's,
// and becoming terminal ends the running one (see endWorkflowExecution).
```

In `internal/postgres/saved_plans.go`, replace `BeginTemplateApply` with:

```go
// BeginTemplateApply claims a run for its apply phase by moving it to running:
// from approved, or, for an auto-approved apply run that never had a plan to
// approve, from queued. The claim starts the run's apply execution as
// workflowID. Losing the claim means the plan was discarded first; the
// conditional update is what makes that race safe, because
// discardTemplateRun makes the same kind of update from the other side.
//
// A run already running is this claim retried after its acknowledgement was
// lost. Nothing else moves an approved or auto-approved run to running, and
// the plan workflow of an approved run has already ended, so it reports the
// claim rather than a lost race, and finds its apply execution already there.
func (store *Store) BeginTemplateApply(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, autoApprove bool, workflowID string) (bool, error) {
	from := domain.TemplateRunApproved
	if autoApprove {
		from = domain.TemplateRunQueued
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin claim run for apply: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	commandTag, err := tx.Exec(ctx, `
		update template_runs
		set status = $1
		where tenant_id = $2 and id = $3 and status in ($4, $1) and auto_approve = $5
	`, domain.TemplateRunRunning, tenantID, runID, from, autoApprove)
	if err != nil {
		return false, fmt.Errorf("claim run for apply: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return false, nil
	}
	if _, err := insertWorkflowExecution(ctx, tx, tenantID, runID, domain.RunPhaseApply, workflowID, domain.TemplateRunExecutionRunning, ""); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit claim run for apply: %w", err)
	}
	return true, nil
}
```

In `FinishTemplatePlan`, in the `case input.Operation == domain.OperationPlan:` branch, after the `update template_runs set plan_add …` statement and its error check, add:

```go
		if err := recordPlanCounts(ctx, tx, input.TenantID, input.RunID, input.Summary); err != nil {
			return "", err
		}
```

In the `default:` (waiting) branch, after the `update stack_templates set pending_plan_run_id …` statement and its error check, add:

```go
		if err := recordPlanCounts(ctx, tx, input.TenantID, input.RunID, input.Summary); err != nil {
			return "", err
		}
		// The plan is done, and nothing runs while the run waits for a
		// person, so its execution ends here. A run already waiting is this
		// write retried, and its plan already ended.
		ended, err := finishRunningExecution(ctx, tx, input.TenantID, input.RunID, domain.TemplateRunExecutionSucceeded, "")
		if err != nil {
			return "", err
		}
		if !ended && status != domain.TemplateRunWaitingApproval {
			return "", fmt.Errorf("finish plan: run %s has no running plan execution", input.RunID)
		}
```

In `RecordTemplateRunEvent` (`internal/postgres/run_progress.go`), inside `if input.Summary != nil {`, after the `update template_runs set plan_add …` statement and its error check, add:

```go
		if err := recordRunningCounts(ctx, tx, input.TenantID, input.RunID, *input.Summary); err != nil {
			return err
		}
```

- [ ] **Step 7: Pass the workflow ID through the activity**

In `internal/activities/control.go`, change the `PlanRecorder` method to:

```go
	// BeginTemplateApply claims a run for its apply phase by moving it to
	// running, and starts its apply execution as workflowID: an approved run,
	// or with autoApprove a queued one. It reports true for a run it already
	// claimed, so a retried claim is idempotent, and false when the run is no
	// longer in a state this claim can take.
	BeginTemplateApply(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, autoApprove bool, workflowID string) (bool, error)
```

and in `BeginApply`:

```go
	claimed, err := activities.store.BeginTemplateApply(ctx, input.TenantID, input.RunID, input.AutoApprove, input.WorkflowID)
```

Change the two stubs to the new signature:
- `internal/activities/template_run_test.go:1021`: `func (store *controlStoreStub) BeginTemplateApply(context.Context, domain.TenantID, domain.TemplateRunID, bool, string) (bool, error) {`
- `cmd/api/main_test.go:833`: `func (recordingStore) BeginTemplateApply(context.Context, domain.TenantID, domain.TemplateRunID, bool, string) (bool, error) {`

- [ ] **Step 8: Carry the phase and workflow ID in the workflows**

In `internal/workflows/template_run.go`:

```go
func TemplatePlanWorkflow(ctx workflow.Context, input domain.TemplateRunWorkflowInput) error {
	w := &planWorkflow{newRun(ctx, input, domain.RunPhasePlan)}
	return w.lifecycle()
}
```

```go
func TemplateApplyWorkflow(ctx workflow.Context, input domain.TemplateRunWorkflowInput) error {
	w := &applyWorkflow{newRun(ctx, input, domain.RunPhaseApply)}
	return w.lifecycle()
}
```

Replace the `run` struct and `newRun` with:

```go
type run struct {
	ctx   workflow.Context
	input domain.TemplateRunWorkflowInput
	// phase is which of the run's workflow executions this workflow is, and
	// workflowID the Temporal workflow it runs as. Status writes and the
	// apply's claim record both on the execution.
	phase      domain.RunPhase
	workflowID string
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
```

In `setStatus`, add to the `TemplateRunStatusActivityInput` literal:

```go
			Phase:           r.phase,
			WorkflowID:      r.workflowID,
```

In `applyWorkflow.lifecycle`, add to the `BeginApplyActivityInput` literal:

```go
		WorkflowID:  w.workflowID,
```

- [ ] **Step 9: Give seeded running runs their execution, and update callers**

In `internal/postgres/store_test.go`, at the end of `seedTemplateRun` (after its error check), add:

```go
	// A running run is always carried by a running workflow execution: its
	// apply for an auto-approved run, which has no plan, and its plan
	// otherwise.
	if run.Status == domain.TemplateRunRunning {
		phase := domain.RunPhasePlan
		if run.AutoApprove {
			phase = domain.RunPhaseApply
		}
		if _, err := pool.Exec(ctx, `
			insert into template_run_workflow_executions (tenant_id, run_id, phase, workflow_id, actor, status, started_at)
			values ($1, $2, $3, $4, $5, 'running', now())
		`, run.TenantID, run.ID, phase, "template-run/"+string(run.TenantID)+"/"+string(run.ID), run.TriggerActor); err != nil {
			t.Fatalf("seed running workflow execution: %v", err)
		}
	}
```

In `internal/postgres/saved_plans_test.go`, add a workflow ID argument to each `store.BeginTemplateApply(ctx, "tenant_123", "<run>", <bool>)` call: `store.BeginTemplateApply(ctx, "tenant_123", "<run>", <bool>, "template-run/tenant_123/<run>/apply")`. There are five, in `TestApplyClaimAndDiscardExcludeEachOther`, `TestAutoApprovedApplyIsClaimedFromQueued` and `TestBeginTemplateApplyClaimIsIdempotent`.

A status write that starts a queued run, or fails a run with no running execution, now needs a phase. Add `Phase: domain.RunPhasePlan, WorkflowID: "template-run/tenant_123/<run id>",` to the `TemplateRunStatusActivityInput` literals in:
- `TestRecordTemplateRunStatusUpdatesTenantScopedRun` (`store_test.go`, the running write)
- `TestCreateTemplateRunScopesTheInFlightGate` (`store_test.go`, the failed write of `run_c_4`)
- `TestRecordTemplateRunStatusReconcilesInterruptedDestroyLifecycle` (`store_test.go`, the terminal write; its "failed before destroy started" case fails a waiting run)

If any other store test then fails with `unknown phase ""`, add the same two fields to its status write.

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/postgres ./internal/activities ./internal/workflows ./cmd/api -count=1 -v 2>&1 | rg -- '--- (FAIL|SKIP)|^(ok|FAIL)'`
Expected: only `ok` lines.

- [ ] **Step 11: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git commit -am "feat: record each workflow execution of a template run"` (after `git add internal/postgres/run_executions.go`).

---

### Task 3: Step executions

**Files:**
- Modify: `internal/postgres/run_progress.go:12-32` (`RecordTemplateRunStep`)
- Modify: `internal/postgres/run_executions.go` (`finishRunningExecution`)
- Test: `internal/postgres/run_executions_test.go`

**Interfaces:**
- Consumes: Task 2's `finishRunningExecution`, test helpers `stepExecutionRows`, `statusWrite`, `mustSucceed`.
- Produces: `RecordTemplateRunStep` writing step rows; `finishRunningExecution` also ending the execution's running step with the same status.

- [ ] **Step 1: Write the failing tests**

Append to `internal/postgres/run_executions_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/postgres -run 'TestRecordTemplateRunStep|TestEndingARun|TestFinishTemplatePlanWaitingEnds' -count=1 -v`
Expected: FAIL. `stepExecutionRows` returns no rows, because no step rows are written yet.

- [ ] **Step 3: Write steps as rows**

Replace `RecordTemplateRunStep` in `internal/postgres/run_progress.go` with the following. The file already imports `errors` and `github.com/jackc/pgx/v5`.

```go
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
	if _, err := tx.Exec(ctx, `
		update template_runs set step = $1 where tenant_id = $2 and id = $3
	`, input.Step, input.TenantID, input.RunID); err != nil {
		return fmt.Errorf("record template run step: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit template run step: %w", err)
	}
	return nil
}
```

The last `update template_runs set step` keeps the column current until Task 4 removes it.

- [ ] **Step 4: End the running step with its execution**

In `internal/postgres/run_executions.go`, replace `finishRunningExecution` with:

```go
// finishRunningExecution ends the run's running workflow execution as status,
// with errorSummary, and the step it was on the same way. It reports whether
// the run had a running execution.
func finishRunningExecution(ctx context.Context, exec pgxExecutor, tenantID domain.TenantID, runID domain.TemplateRunID, status domain.TemplateRunExecutionStatus, errorSummary string) (bool, error) {
	if _, err := exec.Exec(ctx, `
		update template_run_step_executions s
		set status = $3, finished_at = now()
		from template_run_workflow_executions e
		where e.tenant_id = $1 and e.run_id = $2 and e.status = 'running'
			and s.workflow_execution_id = e.id and s.status = 'running'
	`, tenantID, runID, status); err != nil {
		return false, fmt.Errorf("end template run step: %w", err)
	}
	commandTag, err := exec.Exec(ctx, `
		update template_run_workflow_executions
		set status = $3, finished_at = now(), error_summary = $4
		where tenant_id = $1 and run_id = $2 and status = 'running'
	`, tenantID, runID, status, errorSummary)
	if err != nil {
		return false, fmt.Errorf("finish workflow execution: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/postgres -count=1 -v 2>&1 | rg -- '--- (FAIL|SKIP)|^(ok|FAIL)'`
Expected: `ok`, with `TestRecordTemplateRunStepNeedsARunningRunOfTheTenant` and `TestRecordTemplateRunStepAcceptsEveryDomainStep` still passing.

- [ ] **Step 6: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git commit -am "feat: record each step of a template run execution"`.

---

### Task 4: The run reads its step, error and counts from its executions

**Files:**
- Modify: `internal/postgres/migrations/0029_template_run_executions.sql` (append)
- Modify: `internal/postgres/repositories.go` (`createTemplateRun` 912-982, `GetTemplateRun` 984-1059, `ListTemplateRuns` 1070-1153, `RecordTemplateRunStatus` terminal update)
- Modify: `internal/postgres/saved_plans.go` (`FinishTemplatePlan`)
- Modify: `internal/postgres/run_progress.go` (`RecordTemplateRunStep`, `RecordTemplateRunEvent`)
- Modify: `internal/postgres/store_test.go` (`seedTemplateRun`, `TestCreateTemplateRunPersistsRunFields`, `TestGetTemplateRunReturnsTenantScopedRecord`, `TestRecordTemplateRunStatusPersistsFailureSummary`)
- Modify: `internal/domain/template_run.go` (`TemplateRun` field comments)
- Test: `internal/postgres/run_executions_test.go`

**Interfaces:**
- Consumes: Tasks 2-3's writes.
- Produces (package `postgres`): `const templateRunSelect string`, `func scanTemplateRun(row pgx.Row) (domain.TemplateRun, error)`.
- Produces (tests): `runAt(t, ctx, store, runID) domain.TemplateRun`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/postgres/run_executions_test.go` (add `"reflect"` to its imports):

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/postgres -run 'TestRunReads' -count=1 -v`
Expected: FAIL in `TestRunReadsAnApprovedApplyFromItsExecutions` at `claimed run = "running", step "saving_plan"; want running with no step`, and in `TestRunReadsAnApplyThatFailedBeforeItsClaim` with step `saving_plan`: the run row still holds the plan's step.

- [ ] **Step 3: Drop the columns the executions now hold**

Append to `internal/postgres/migrations/0029_template_run_executions.sql`:

```sql
-- A run's step, error and counts belong to its workflow executions from here
-- on, and reads derive them from there. Dropping step drops its check too.
alter table template_runs
	drop column step,
	drop column error_summary,
	drop column plan_add,
	drop column plan_change,
	drop column plan_destroy;
```

- [ ] **Step 4: Derive the fields in one shared select**

In `internal/postgres/repositories.go`, replace `GetTemplateRun` and `ListTemplateRuns` (keep `planSummary`) with:

```go
// templateRunSelect reads a run as domain.TemplateRun has it. A run's step,
// error and counts are its workflow executions', so they are derived here:
//
//   - step is the latest step of the execution that started last, or none if
//     that execution has not started one: an apply between its claim and its
//     first step reads no step, not the plan's last;
//   - the error is the latest failed execution's;
//   - the counts are the plan execution's if the run has one, and otherwise
//     the apply's, which an auto-approved run has instead.
//
// Each is a lateral lookup on an indexed key, so a list of runs is still one
// query. Callers append the where clause, and the order for a list.
const templateRunSelect = `
	select
		r.id,
		r.tenant_id,
		r.stack_template_id,
		r.template_revision_id,
		r.source_template_id,
		r.operation,
		r.selected_ref,
		r.resolved_commit_sha,
		r.workspace_name,
		r.config_json,
		r.backend_type,
		r.backend_config_hash,
		r.status,
		coalesce(latest_step.step, ''),
		r.trigger_actor,
		r.started_at,
		r.completed_at,
		coalesce(failed.error_summary, ''),
		r.run_number,
		r.auto_approve,
		counts.resource_add,
		counts.resource_change,
		counts.resource_destroy
	from template_runs r
	left join lateral (
		select s.step
		from template_run_workflow_executions e
		left join template_run_step_executions s on s.workflow_execution_id = e.id
		where e.tenant_id = r.tenant_id and e.run_id = r.id
		order by e.id desc, s.id desc nulls last
		limit 1
	) latest_step on true
	left join lateral (
		select e.error_summary
		from template_run_workflow_executions e
		where e.tenant_id = r.tenant_id and e.run_id = r.id and e.status = 'failed'
		order by e.id desc
		limit 1
	) failed on true
	left join lateral (
		select e.resource_add, e.resource_change, e.resource_destroy
		from template_run_workflow_executions e
		where e.tenant_id = r.tenant_id and e.run_id = r.id
		order by e.phase = 'plan' desc, e.id
		limit 1
	) counts on true
`

// scanTemplateRun scans one row of templateRunSelect.
func scanTemplateRun(row pgx.Row) (domain.TemplateRun, error) {
	var run domain.TemplateRun
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var add, change, destroy *int
	if err := row.Scan(
		&run.ID,
		&run.TenantID,
		&run.StackTemplateID,
		&run.TemplateRevisionID,
		&run.SourceTemplateID,
		&run.Operation,
		&run.SelectedRef,
		&run.ResolvedCommitSHA,
		&run.WorkspaceName,
		&run.ConfigJSON,
		&run.BackendType,
		&run.BackendConfigHash,
		&run.Status,
		&run.Step,
		&run.TriggerActor,
		&startedAt,
		&completedAt,
		&run.ErrorSummary,
		&run.RunNumber,
		&run.AutoApprove,
		&add,
		&change,
		&destroy,
	); err != nil {
		return domain.TemplateRun{}, err
	}
	if startedAt.Valid {
		run.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time
	}
	run.PlanSummary = planSummary(add, change, destroy)
	return run, nil
}

func (store *Store) GetTemplateRun(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (domain.TemplateRun, error) {
	run, err := scanTemplateRun(store.pool.QueryRow(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.id = $2
	`, tenantID, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TemplateRun{}, app.ErrNotFound
	}
	if err != nil {
		return domain.TemplateRun{}, fmt.Errorf("get template run: %w", err)
	}
	return run, nil
}

func (store *Store) ListTemplateRuns(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) ([]domain.TemplateRun, error) {
	rows, err := store.pool.Query(ctx, templateRunSelect+`
		where r.tenant_id = $1 and r.stack_template_id = $2
		order by r.started_at desc nulls last, r.id desc
	`, tenantID, stackTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list template runs: %w", err)
	}
	defer rows.Close()

	runs := []domain.TemplateRun{}
	for rows.Next() {
		run, err := scanTemplateRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list template runs: %w", err)
	}
	return runs, nil
}
```

Update the `planSummary` comment to: `// planSummary reassembles an execution's counts, which are written together: either all three are set or none is.`

- [ ] **Step 5: Stop writing the dropped columns**

- `createTemplateRun` (`repositories.go`): remove `error_summary` from the column list and `run.ErrorSummary` from the arguments. The values become `$1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, $14, $15, $16, $17,` followed by the run number subquery, with `$17` being `run.AutoApprove`.
- `RecordTemplateRunStatus` terminal update: replace it with
  ```go
	if _, err := tx.Exec(ctx, `
		update template_runs
		set status = $1, completed_at = coalesce(completed_at, now())
		where tenant_id = $2 and id = $3
	`, input.Status, input.TenantID, input.RunID); err != nil {
		return fmt.Errorf("record template run status: %w", err)
	}
  ```
- `FinishTemplatePlan`: in the plan-run branch, delete the `update template_runs set plan_add …` statement (keep `recordPlanCounts`). In the waiting branch, replace the run update with
  ```go
		if _, err := tx.Exec(ctx, `
			update template_runs set status = $1 where tenant_id = $2 and id = $3
		`, domain.TemplateRunWaitingApproval, input.TenantID, input.RunID); err != nil {
			return "", fmt.Errorf("record plan with changes: %w", err)
		}
  ```
- `RecordTemplateRunEvent`: delete the `update template_runs set plan_add …` statement inside `if input.Summary != nil`, keeping `recordRunningCounts`.
- `RecordTemplateRunStep`: delete the trailing `update template_runs set step …` statement.

Update the `TemplateRun` field comments in `internal/domain/template_run.go`:

```go
	// Step is what the run is doing, or, once it ended, what it was doing
	// last: the latest step of the workflow execution that started last.
	// Empty until that execution starts a step.
	Step         TemplateRunStep `json:"step"`
```

```go
	// ErrorSummary is why the run failed: its failed workflow execution's
	// error. Empty otherwise.
	ErrorSummary string          `json:"error_summary"`
```

```go
	// PlanSummary is what the plan would change, from the run's plan
	// execution, or, for an auto-approved apply, which has no plan, what the
	// apply changed. Nil until known, and for a plan with no changes.
	PlanSummary *PlanSummary `json:"plan_summary"`
```

- [ ] **Step 6: Update the tests that wrote the dropped columns**

- `seedTemplateRun` (`store_test.go`): remove `error_summary` from the column list and `run.ErrorSummary` from the arguments. Renumber the values to `$1 … $17`, with `$17` being `run.AutoApprove`.
- `TestCreateTemplateRunPersistsRunFields`: remove `ErrorSummary: "previous error summary",` from the fixture, `error_summary` from the verifying `select`, `&got.ErrorSummary` from its `Scan`, and `got.ErrorSummary != run.ErrorSummary ||` from the comparison.
- `TestGetTemplateRunReturnsTenantScopedRecord`: remove `ErrorSummary: "previous error summary",` from both the seeded run and `want`.
- `TestRecordTemplateRunStatusPersistsFailureSummary`: read the error through the store instead of the dropped column. Replace its `select status, error_summary, completed_at from template_runs …` with `select status, completed_at …` scanning `&status, &completedAt`, and read the error with
  ```go
	run, err := store.GetTemplateRun(ctx, "tenant_123", "run_failure_summary")
	if err != nil {
		t.Fatal(err)
	}
	errorSummary := run.ErrorSummary
  ```
  keeping its existing assertions on `errorSummary`.

Then run `rg -n "error_summary|plan_add|plan_change|plan_destroy|\bstep\b" internal/postgres/*.go` and confirm every remaining hit is on `template_registrations`, a `template_run_*_executions` table, or a comment.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/postgres -count=1 -v 2>&1 | rg -- '--- (FAIL|SKIP)|^(ok|FAIL)'`
Expected: `ok`.

- [ ] **Step 8: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git commit -am "refactor: a run reads its step, error and counts from its executions"`.

---

### Task 5: The single-run API returns the executions

**Files:**
- Modify: `internal/domain/template_run.go` (`TemplateRun.Executions`)
- Modify: `internal/postgres/run_executions.go` (`ListTemplateRunExecutions`)
- Modify: `internal/app/service.go:111-118` (`TemplateRunRepository`), `:1658-1669` (`GetTemplateRun`)
- Modify: `internal/app/service_test.go` (`recordingTemplateRunRepository`), `internal/api/server_test.go` (`recordingTemplateRunRepository`), `cmd/api/main_test.go` (`recordingStore`)
- Modify: `docs/openapi.yaml` (`TemplateRun`, new schemas)
- Test: `internal/postgres/run_executions_test.go`, `internal/app/service_test.go`, `internal/api/server_test.go`

**Interfaces:**
- Consumes: Task 1's domain types; Task 4's `planSummary`.
- Produces: `TemplateRun.Executions []TemplateRunWorkflowExecution` (JSON `executions,omitempty`).
- Produces: `func (store *Store) ListTemplateRunExecutions(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error)`, and the same method on `app.TemplateRunRepository`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/postgres/run_executions_test.go`:

```go
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
	if got := []domain.TemplateRunStep{plan.Steps[0].Step, plan.Steps[1].Step}; len(plan.Steps) != 2 ||
		got[0] != domain.TemplateRunStepPlanning || got[1] != domain.TemplateRunStepSavingPlan {
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
```

In `internal/app/service_test.go`, add a field `executions []domain.TemplateRunWorkflowExecution` to `recordingTemplateRunRepository` and the method:

```go
func (repository *recordingTemplateRunRepository) ListTemplateRunExecutions(context.Context, domain.TenantID, domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	return repository.executions, nil
}
```

In `TestGetTemplateRunReturnsTenantScopedRun`, add `executions: []domain.TemplateRunWorkflowExecution{{Phase: domain.RunPhasePlan, Status: domain.TemplateRunExecutionSucceeded}},` to the `recordingTemplateRunRepository` literal, and after the existing assertions add:

```go
	if len(run.Executions) != 1 || run.Executions[0].Phase != domain.RunPhasePlan {
		t.Fatalf("executions = %#v, want the run's plan", run.Executions)
	}
```

In `internal/api/server_test.go`, add the same field and method to its `recordingTemplateRunRepository`. In `TestGetTemplateRunReturnsRun`, before building the server, set:

```go
	deps.templateRuns.executions = []domain.TemplateRunWorkflowExecution{{
		Phase: domain.RunPhasePlan, WorkflowID: "template-run/tenant_123/run_123", Status: domain.TemplateRunExecutionSucceeded,
		Steps: []domain.TemplateRunStepExecution{{Step: domain.TemplateRunStepPlanning, Status: domain.TemplateRunExecutionSucceeded}},
	}}
```

and after decoding `body`, add:

```go
	if len(body.Executions) != 1 || len(body.Executions[0].Steps) != 1 || body.Executions[0].Steps[0].Step != domain.TemplateRunStepPlanning {
		t.Fatalf("executions = %#v, want the plan with its planning step", body.Executions)
	}
```

In `cmd/api/main_test.go`, add to `recordingStore`:

```go
func (recordingStore) ListTemplateRunExecutions(context.Context, domain.TenantID, domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	return nil, nil
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/postgres ./internal/app ./internal/api ./cmd/api -count=1`
Expected: compile failure, `store.ListTemplateRunExecutions undefined` and `run.Executions undefined`.

- [ ] **Step 3: Add the field, the repository method and the service call**

In `internal/domain/template_run.go`, add as the last field of `TemplateRun`:

```go
	// Executions are the workflows that carried the run, in the order they
	// started, each with its steps. Only a read of one run fills them.
	Executions []TemplateRunWorkflowExecution `json:"executions,omitempty"`
```

Append to `internal/postgres/run_executions.go` (add `"database/sql"` to its imports):

```go
// ListTemplateRunExecutions returns the workflow executions that carried the
// run, in the order they started, each with its steps in the order they
// started.
func (store *Store) ListTemplateRunExecutions(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error) {
	rows, err := store.pool.Query(ctx, `
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

	stepRows, err := store.pool.Query(ctx, `
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
```

In `internal/app/service.go`, add to `TemplateRunRepository`:

```go
	// ListTemplateRunExecutions returns the workflow executions that carried a
	// run, with their steps, in the order they started.
	ListTemplateRunExecutions(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]domain.TemplateRunWorkflowExecution, error)
```

and change `GetTemplateRun` to:

```go
// GetTemplateRun returns one tenant-owned run, with the workflow executions
// that carried it. Only this read loads them: every other run endpoint reads
// the run for authorization alone.
func (service *Service) GetTemplateRun(ctx context.Context, command GetTemplateRunCommand) (domain.TemplateRun, error) {
	if err := validateGetTemplateRunCommand(command); err != nil {
		return domain.TemplateRun{}, err
	}

	run, err := service.authorizedTemplateRun(ctx, command.TenantID, command.RunID, authorization.RelationCanView, ErrNotFound)
	if err != nil {
		return domain.TemplateRun{}, fmt.Errorf("get template run: %w", err)
	}
	executions, err := service.TemplateRuns.ListTemplateRunExecutions(ctx, command.TenantID, command.RunID)
	if err != nil {
		return domain.TemplateRun{}, fmt.Errorf("get template run executions: %w", err)
	}
	run.Executions = executions

	return run, nil
}
```

- [ ] **Step 4: Document the response**

In `docs/openapi.yaml`, under `TemplateRun.properties`, after `plan_summary`, add:

```yaml
        executions:
          type: array
          description: |
            The workflows that carried the run, in the order they started:
            its plan, then its apply, or only one of them. Present only on
            `GET /v1/tenants/{tenant_id}/template-runs/{run_id}`.
          items: { $ref: "#/components/schemas/TemplateRunWorkflowExecution" }
```

Replace the descriptions of `step`, `error_summary` and `plan_summary` in `TemplateRun` with:

```yaml
        step:
          allOf: [{ $ref: "#/components/schemas/TemplateRunStep" }]
          description: |
            The latest step of the workflow execution that started last.
            Empty when that execution has not started a step, as for an
            apply between claiming the run and its first step.
```

```yaml
        error_summary:
          type: string
          description: Why the run failed, from its failed workflow execution. Empty otherwise.
```

```yaml
          description: |
            What the plan would change, counted the way tofu's own summary
            line counts: a replacement is one add and one destroy. From the
            run's plan execution; null until a plan with changes has
            finished. For an auto-approved `apply`, which has no plan, what
            the apply changed, read from its "Apply complete!" line, and
            null until it finishes.
```

Add these schemas after `TemplateRunStep`:

```yaml
    TemplateRunExecutionStatus:
      type: string
      description: Where one workflow execution of a run, or one step of it, stands.
      enum: [running, succeeded, failed]

    TemplateRunWorkflowExecution:
      type: object
      description: One workflow that carried a run.
      properties:
        phase: { type: string, enum: [plan, apply] }
        workflow_id: { type: string, description: The Temporal workflow ID. }
        actor:
          type: string
          description: |
            Who started this execution: the run's requester for a plan or an
            auto-approved apply, and its approver for an approved apply.
        status: { $ref: "#/components/schemas/TemplateRunExecutionStatus" }
        started_at: { type: string, format: date-time }
        finished_at:
          type: string
          format: date-time
          description: Reads as the zero time (`0001-01-01T00:00:00Z`) while the execution runs.
        error_summary: { type: string, description: Why it failed. Empty otherwise. }
        summary:
          type: [object, "null"]
          description: |
            A plan's counts of what it would change, or an apply's of what it
            changed. Null for a plan with no changes, and until known.
          properties:
            add: { type: integer, minimum: 0 }
            change: { type: integer, minimum: 0 }
            destroy: { type: integer, minimum: 0 }
          required: [add, change, destroy]
        steps:
          type: array
          description: The steps it ran, in the order they started.
          items: { $ref: "#/components/schemas/TemplateRunStepExecution" }
      required: [phase, workflow_id, actor, status, started_at, finished_at, error_summary, summary, steps]

    TemplateRunStepExecution:
      type: object
      description: |
        One step a workflow execution ran. A step runs until the next one
        starts, or until its execution ends.
      properties:
        step: { $ref: "#/components/schemas/TemplateRunStep" }
        status: { $ref: "#/components/schemas/TemplateRunExecutionStatus" }
        started_at: { type: string, format: date-time }
        finished_at:
          type: string
          format: date-time
          description: Reads as the zero time while the step runs.
      required: [step, status, started_at, finished_at]
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/postgres ./internal/app ./internal/api ./cmd/api -count=1 -v 2>&1 | rg -- '--- (FAIL|SKIP)|^(ok|FAIL)'` and `make api-docs-lint`
Expected: only `ok` lines; the lint reports no errors.

- [ ] **Step 6: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git commit -am "feat: return a run's executions with its steps"`.

---

### Task 6: `started_at` becomes `created_at`

**Files:**
- Modify: `internal/postgres/migrations/0029_template_run_executions.sql` (append)
- Modify: `internal/domain/template_run.go` (`TemplateRun.StartedAt`)
- Modify: `internal/postgres/repositories.go` (`createTemplateRun`, `templateRunSelect`, `scanTemplateRun`, `ListTemplateRuns` order)
- Modify: `internal/app/service.go` (`StartTemplateRun`, `StartedAt: service.Clock.Now()`)
- Modify: `internal/postgres/store_test.go`, `internal/app/service_test.go`, `internal/api/server_test.go`
- Modify: `docs/openapi.yaml`
- Modify: `web/src/api/types.ts`, `web/src/features/runs/TemplateRunHistory.tsx`, `web/src/features/runs/RunDetailScreen.tsx`, and the fixtures in `web/src/features/runs/{RunDetailScreen,TemplateDestroyPanel,TemplateRunActions,TemplateRunHistory}.test.tsx`, `web/src/features/stacks/StackTemplatePages.test.tsx`

**Interfaces:**
- Produces: `TemplateRun.CreatedAt time.Time` (JSON `created_at`) in place of `StartedAt`.

- [ ] **Step 1: Write the failing test**

In `internal/api/server_test.go`'s `TestStartTemplateRunCallsService`, change the decoded-body assertion to:

```go
	if !body.CreatedAt.Equal(startedAt) {
		t.Fatalf("created_at = %v, want %v", body.CreatedAt, startedAt)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api -run TestStartTemplateRunCallsService -count=1`
Expected: compile failure, `body.CreatedAt undefined`.

- [ ] **Step 3: Rename the column and field**

Append to `internal/postgres/migrations/0029_template_run_executions.sql`:

```sql
-- started_at was always set when the run was queued, not when any work
-- started; when work starts is now its executions' started_at.
alter table template_runs rename column started_at to created_at;
```

In `internal/domain/template_run.go`, replace `StartedAt    time.Time       \`json:"started_at"\`` with:

```go
	// CreatedAt is when the run was requested. When work started is its
	// executions' StartedAt.
	CreatedAt   time.Time       `json:"created_at"`
```

Then:
- `repositories.go`: in `createTemplateRun` rename the column `started_at` to `created_at` and `run.StartedAt` to `run.CreatedAt`; in `templateRunSelect` replace `r.started_at,` with `r.created_at,`; in `scanTemplateRun` rename `startedAt` to `createdAt` and assign `run.CreatedAt`; in `ListTemplateRuns` order by `r.created_at desc nulls last, r.id desc`.
- `service.go`: in `StartTemplateRun` replace `StartedAt:         service.Clock.Now(),` with `CreatedAt:         service.Clock.Now(),`.
- `rg -l 'StartedAt' internal cmd -g '*_test.go'` and, in the files that build or read a `domain.TemplateRun` (`internal/postgres/store_test.go`, `internal/app/service_test.go`, `internal/api/server_test.go`), rename `StartedAt` to `CreatedAt` and the `started_at` column to `created_at`. Leave `StartedAt` on the workflow and step execution rows and on `TemplateRegistration` alone.

- [ ] **Step 4: Rename it in the API document and the UI**

- `docs/openapi.yaml`: in `TemplateRun`, replace `started_at: { type: string, format: date-time }` with
  ```yaml
        created_at:
          type: string
          format: date-time
          description: |
            When the run was requested. When its work started is its
            executions' `started_at`.
  ```
  and `started_at` with `created_at` in its `required` list.
- `web/`: `rg -l 'started_at' web/src | xargs sed -i '' 's/started_at/created_at/g'`. Every `started_at` under `web/src` is a `TemplateRun` field; confirm with `rg -n created_at web/src` that nothing else was touched. Keep the UI labels as they are.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -count=1` (with the DSN exported), `make api-docs-lint`, and in `web/`: `npm test && npm run build`
Expected: all pass.

- [ ] **Step 6: Checkpoint**

Run `gofmt`, `make lint`. Commit if the user asked: `git commit -am "refactor: a run's started_at is its created_at"`.

---

### Task 7: Count every apply

**Files:**
- Modify: `internal/runner/terraform.go` (`Result` comment, `Run` apply cases, `applyAutoApprove`, `applyCompleteLine`)
- Modify: `internal/workflows/template_run.go` (`applyWorkflow.apply`)
- Modify: `internal/domain/workflow.go` (`RunTerraformActivityOutput` and `TemplateRunEventActivityInput` comments)
- Test: `internal/runner/terraform_test.go`, `internal/workflows/template_run_workflow_test.go`

**Interfaces:**
- Produces: `runner.Run` returning `Result{HasChanges, Summary}` for `TerraformCommandApply` and `TerraformCommandDestroy`; `summarizeApplyOutput` also reading `Destroy complete! Resources: …`.
- Produces: the apply workflow's `applied` and `destroyed` events always carrying the command's counts.

- [ ] **Step 1: Write the failing tests**

In `internal/runner/terraform_test.go`, add:

```go
// An approved apply and a destroy apply a saved plan, and count what they did
// from the closing line, as an auto-approved apply does, whichever of tofu's
// two lines it prints.
func TestLocalProcessRunnerCountsASavedPlanApplyFromItsOutput(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		command domain.TerraformCommandType
		stdout  string
		want    domain.PlanSummary
	}{
		{name: "apply", command: domain.TerraformCommandApply, stdout: "Apply complete! Resources: 2 added, 1 changed, 0 destroyed.\n", want: domain.PlanSummary{Add: 2, Change: 1}},
		{name: "destroy", command: domain.TerraformCommandDestroy, stdout: "Destroy complete! Resources: 3 destroyed.\n", want: domain.PlanSummary{Destroy: 3}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingCommandExecutor{stdout: testCase.stdout}
			runner := NewLocalProcessRunnerWithExecutor(executor)
			var log bytes.Buffer

			result, err := runner.Run(context.Background(), TerraformCommand{
				WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
				WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
				Command:       testCase.command,
				Stdout:        &log,
			})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if want := (Result{HasChanges: true, Summary: testCase.want}); result != want {
				t.Fatalf("result = %#v, want %#v", result, want)
			}
			if log.String() != testCase.stdout {
				t.Fatalf("log = %q, want the apply output", log.String())
			}
		})
	}
}
```

In `TestSummarizeApplyOutput`, add the case:

```go
		{name: "destroy", output: "Destroy complete! Resources: 3 destroyed.\n", want: domain.PlanSummary{Destroy: 3}},
```

In `internal/workflows/template_run_workflow_test.go`, add:

```go
// An approved apply and a destroy record what their command did with the
// event that ends them, as an auto-approved apply always has.
func TestTemplateApplyWorkflowRecordsTheCountsItsCommandReports(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		operation domain.OperationType
		command   domain.TerraformCommandType
		event     domain.TemplateRunEvent
	}{
		{operation: domain.OperationApply, command: domain.TerraformCommandApply, event: domain.TemplateRunApplied},
		{operation: domain.OperationDestroy, command: domain.TerraformCommandDestroy, event: domain.TemplateRunDestroyed},
	} {
		t.Run(string(testCase.operation), func(t *testing.T) {
			t.Parallel()

			env := newTemplateRunWorkflowTestEnvironment(t)
			counts := domain.PlanSummary{Add: 1, Change: 2, Destroy: 3}
			summaries := map[domain.TemplateRunEvent]*domain.PlanSummary{}
			env.OnActivity(domain.RunTerraformActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
					if input.Command == testCase.command {
						return domain.RunTerraformActivityOutput{HasChanges: true, Summary: counts}, nil
					}
					return domain.RunTerraformActivityOutput{}, nil
				})
			env.OnActivity(domain.RecordTemplateRunEventActivityName, mock.Anything, mock.Anything).
				Return(func(_ context.Context, input domain.TemplateRunEventActivityInput) error {
					summaries[input.Event] = input.Summary
					return nil
				})

			env.ExecuteWorkflow(TemplateApplyWorkflow, templateRunWorkflowInput(testCase.operation))

			assertWorkflowCompleted(t, env)
			if got := summaries[testCase.event]; got == nil || *got != counts {
				t.Fatalf("%s summary = %#v, want the command's counts", testCase.event, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runner ./internal/workflows -run 'CountsASavedPlanApply|SummarizeApplyOutput|RecordsTheCountsItsCommandReports' -count=1`
Expected: FAIL. The runner returns `Result{}` for a saved-plan apply, the summarizer reads no `Destroy complete!` line, and the workflow sends no summary with `applied` or `destroyed`.

- [ ] **Step 3: Count every apply in the runner**

In `internal/runner/terraform.go`:

- Replace the `Result` doc comment's last sentences with: `// Result reports what a command found out. A plan and every apply fill it in.` and the `Summary` field comment with `// Summary counts the changes: a plan's from tofu show -json on the saved plan, an apply's from its closing "Apply complete!" or "Destroy complete!" line. Zero when there are none.` and the `HasChanges` comment's second sentence with `For an apply, whether it changed anything.`
- In `Run`, replace the two apply cases with:
  ```go
	case domain.TerraformCommandApply, domain.TerraformCommandDestroy:
		return runner.countApply(input, func(input TerraformCommand) error {
			return runner.run(ctx, input, sortedEnvironment(input.Environment), "apply", "-input=false", "-auto-approve", "-no-color", PlanFileName)
		})
	case domain.TerraformCommandApplyAutoApprove:
		return runner.countApply(input, func(input TerraformCommand) error {
			return runner.runWithTerraformVariables(ctx, input, "apply", "-input=false", "-auto-approve", "-no-color")
		})
  ```
- Replace `applyAutoApprove` with:
  ```go
// countApply runs an apply and counts what it did from its closing line. The
// output still goes to the command's writers; the counts are read from a copy.
func (runner *LocalProcessRunner) countApply(input TerraformCommand, apply func(TerraformCommand) error) (Result, error) {
	var output bytes.Buffer
	stdout, _ := outputWriters(input)
	input.Stdout = io.MultiWriter(stdout, &output)
	if err := apply(input); err != nil {
		return Result{}, err
	}
	summary := summarizeApplyOutput(output.String())
	return Result{HasChanges: summary != domain.PlanSummary{}, Summary: summary}, nil
}
  ```
- Replace `applyCompleteLine` and its comment with:
  ```go
// applyCompleteLine is tofu's closing line for a successful apply, such as
// "Apply complete! Resources: 1 imported, 2 added, 0 changed, 1 destroyed.",
// or "Destroy complete! Resources: 3 destroyed." when it prints that one for
// a destroy.
var applyCompleteLine = regexp.MustCompile(`(?:Apply|Destroy) complete! Resources: ([^\n]*)`)
  ```
- Update the `Run` doc comment's last paragraph so it no longer says an auto-approved apply is the only one counted.

- [ ] **Step 4: Send the counts with every apply's event**

In `internal/workflows/template_run.go`, replace the end of `applyWorkflow.apply`, from `output, err := s.terraform(command)`, with:

```go
	output, err := s.terraform(command)
	if err != nil {
		return err
	}
	if destroy {
		return w.event(domain.TemplateRunDestroyed, &output.Summary)
	}
	return w.event(domain.TemplateRunApplied, &output.Summary)
}
```

and replace the end of its doc comment ("… an apply records what it applied as live, with the counts an auto-approved apply reports, since it had no plan to count.") with: `… an apply records what it applied as live. Either way the event carries what the command did, which is the apply execution's counts.`

In `internal/domain/workflow.go`, update the comments: `RunTerraformActivityOutput.HasChanges and Summary` → `describe a plan or an apply: whether it would change, or changed, anything, and what. Zero for every other command.`; `TemplateRunEventActivityInput.Summary` → `// Summary, when set, records what the run's apply did, on its apply execution. Every apply and destroy carries it; a plan's destroyed event, for a destroy with nothing to destroy, does not.`

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runner ./internal/workflows ./internal/activities -count=1`
Expected: PASS, including the existing `TestLocalProcessRunnerRunsTerraformApply`, `…RunsTerraformDestroy`, `…SetsTerraformVariablesOnlyWhenPlanning` and `…CountsAnAutoApprovedApplyFromItsOutput`.

- [ ] **Step 6: Checkpoint**

Run `go test ./... -count=1`, `gofmt`, `make lint`. Commit if the user asked: `git commit -am "feat: count what every apply did"`.

---

### Task 8: Documents and end-to-end check

**Files:**
- Modify: `docs/apply-run-sequence.md:93,101,167,190`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Update the sequence document**

In `docs/apply-run-sequence.md`, replace these lines:
- `CW->>PG: FinishPlan: counts, waiting_approval, template's pending plan` → `CW->>PG: FinishPlan: plan execution and its last step succeeded with its counts, run waiting_approval, template's pending plan`
- `CW->>PG: BeginApply: approved to running, or stop if discarded meanwhile` → `CW->>PG: BeginApply: approved to running and the apply execution started, or stop if discarded meanwhile`
- `CW->>PG: stack template last applied, with the run row locked` → `CW->>PG: stack template last applied, and the apply's counts on its execution, with the run row locked`
- `CW->>PG: UPDATE template_runs status (completed also drops the plan key)` → `CW->>PG: run completed, its apply execution and last step succeeded (also drops the plan key)`

- [ ] **Step 2: Run the whole suite**

With the DSN exported:

```bash
go test ./... -count=1 -v 2>&1 | rg -- '--- (FAIL|SKIP)|^(ok|FAIL)'
gofmt -l $(rg --files cmd internal -g '*.go')
make lint
make api-docs-lint
(cd web && npm test && npm run build)
```

Expected: only `ok` lines, and no `--- SKIP` from `internal/postgres`; `gofmt -l` prints nothing; both lints pass; web tests and build pass.

- [ ] **Step 3: Run a real plan, approval and apply**

Reset the local database, which still holds the stash's `0029`: `docker compose down -v && docker compose up -d`. Provision per `README.md`, then run `go run ./cmd/api` and `go run ./cmd/executor` in two shells and `npm run dev` in `web/`. In the UI, start an **Apply** on a stack template, wait for it to wait for approval, approve it, and wait for it to complete.

Then read its timeline (`<run_id>` from the run's URL or `select id from template_runs order by created_at desc limit 1`):

```bash
docker exec -i openplan-compose-postgres-1 psql -U openplan -d openplan_test -v run=<run_id> <<'SQL'
select 'step' as kind, w.phase, s.step as name, s.status, s.started_at, s.finished_at
from template_run_step_executions s
join template_run_workflow_executions w on w.id = s.workflow_execution_id
where s.run_id = :'run'
union all
select 'approval', null, a.approved_by, null, a.approved_at, a.approved_at
from template_run_approvals a
where a.run_id = :'run'
order by started_at;
SQL
```

Expected: the plan's steps (`waiting_for_executor` … `saving_plan`), then the approval, then the apply's steps (`waiting_for_executor`, `preparing_workspace`, `fetching_source`, `restoring_plan`, `initializing`, `selecting_workspace`, `applying`), every one `succeeded` with a `finished_at`. Also check `select phase, actor, status, resource_add, resource_change, resource_destroy from template_run_workflow_executions where run_id = '<run_id>' order by id;` shows a plan by the requester and an apply by the approver, both `succeeded`, each with counts. Report the output. If anything differs, stop and report it rather than adjusting the check.

- [ ] **Step 4: Checkpoint**

Commit if the user asked: `git commit -am "docs: the run's writes land on its executions"`. Then use superpowers:finishing-a-development-branch.
