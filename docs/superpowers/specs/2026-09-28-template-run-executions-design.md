# Template run executions — design

Status: approved for planning, 2026-09-28.

## Problem

A template run is one row in `template_runs`, but up to two workflows carry
it: `TemplatePlanWorkflow`, then, once approved, `TemplateApplyWorkflow`. Both
write the same row, and the row holds one value per field. So:

- `step`, `error_summary` and `completed_at` are whatever the last writer left.
  The apply overwrites the plan's, and each step overwrites the one before it.
- `plan_add`, `plan_change` and `plan_destroy` hold the plan's counts, except
  on an auto-approved apply, which writes the apply's counts into them
  (`RecordTemplateRunEvent`, `internal/postgres/run_progress.go`). An
  approved apply's own counts are never read at all.
- `started_at` is set when the API enqueues the run, not when any work starts.
  Nothing records when the plan ended, when the apply began, or when any step
  started or finished.
- The only record of who started the apply is `template_run_approvals`,
  because the row has one `trigger_actor`.

The two workflows are already linked: both carry the run's ID, and the apply's
workflow ID extends the plan's (`internal/temporal/dispatcher.go`). What is
missing is a place for each workflow's facts and each step's.

## Model

Three levels:

- **Run** (`template_runs`): what the user asked for and where it stands. The
  intent (`operation`, `auto_approve`), the snapshot it runs against, its
  lifecycle `status`, `run_number`, who requested it, and when it was
  created and ended. The approval gate and the one-in-flight index stay here.
- **Workflow execution** (`template_run_workflow_executions`, new): one row
  per workflow that carried the run. Its phase, workflow ID, actor, status,
  start and finish, error, and counts: a plan row counts what the plan would
  change, an apply row what the apply did.
- **Step execution** (`template_run_step_executions`, new): one row per step
  a workflow execution ran, with its start, finish and outcome.

| Run | Workflow executions |
|---|---|
| plan | `plan` |
| apply, approved | `plan`, then `apply` |
| apply, auto-approved | `apply` |
| destroy | `plan`, then `apply` |

## Schema

```sql
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

create unique index template_run_workflow_executions_one_running_idx
	on template_run_workflow_executions (tenant_id, run_id)
	where status = 'running';

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

create unique index template_run_step_executions_one_running_idx
	on template_run_step_executions (workflow_execution_id)
	where status = 'running';

create index template_run_step_executions_run_idx
	on template_run_step_executions (tenant_id, run_id, id);
```

The step check lists `domain.AllTemplateRunSteps` and must stay equal to it,
as 0026's did.

Keys and order:

- A run has at most one workflow execution per phase: the apply's workflow ID
  rejects duplicates. Retrying a failed apply on the same run would add an
  attempt to that key; that is not planned.
- A workflow execution runs each step at most once, so `(workflow_execution_id,
  step)` is unique.
- Rows are ordered by their identity `id`, never by a timestamp. Each row is
  written in its own transaction, so `now()` values can tie or step backwards
  with the clock; insert order cannot.
- A step carries `run_id` as well as its execution's ID so a run's whole
  timeline is one indexed read with no join. The composite foreign key to
  `(id, run_id)` keeps the two from disagreeing.

`template_runs` changes:

- drops `step`, `error_summary`, `plan_add`, `plan_change`, `plan_destroy`;
- renames `started_at` to `created_at`, which is what it always was;
- keeps `trigger_actor` (who asked for the run), `completed_at` (when the run
  ended, including a discard, which has no execution), and the
  `cancellation_*` columns (the discard's record).

`plan_artifact_dek` stays on the run. The key is created by the plan
execution, read by the apply execution, and dropped when the run ends
(`releaseRunPlan`). It is the handoff between the two executions and lives as
long as the run, so it belongs to neither.

`template_run_approvals` and `template_run_logs` are unchanged.

## Write rules

The invariants:

- **A run is `running` exactly when one of its workflow executions is
  `running`.**
- **A step execution is `running` only while its workflow execution is.**

Every rule writes the rows it touches in one transaction.

| Moment | Where | Run | Workflow execution | Step execution |
|---|---|---|---|---|
| Plan starts | `RecordTemplateRunStatus(running)` | queued → running | insert `plan`, running, actor = `trigger_actor` | — |
| Apply claims the run | `BeginTemplateApply` | approved (or queued, auto-approve) → running | insert `apply`, running, actor = approver, or `trigger_actor` when auto-approved | — |
| A step starts | `RecordTemplateRunStep` | — | — | the running step succeeded; insert this step, running |
| Plan finishes with changes to approve | `FinishTemplatePlan` | running → waiting_approval | `plan` succeeded, counts | running step succeeded |
| Plan run finishes with changes | `FinishTemplatePlan`, then `RecordTemplateRunStatus(completed)` | running → completed | counts, then succeeded | running step succeeded |
| Plan finishes with no changes | `FinishTemplatePlan`, then `RecordTemplateRunStatus(completed)` | running → completed | no counts, then succeeded | running step succeeded |
| Apply or destroy finishes its command | `RecordTemplateRunEvent(applied / destroyed)` | — | the running execution's counts | — |
| Run completes | `RecordTemplateRunStatus(completed)` | running → completed | running execution succeeded | running step succeeded |
| Run fails | `RecordTemplateRunStatus(failed)` | → failed | the input phase's running execution failed with the error; if none is running and the phase has none yet, insert it already failed | running step failed |
| A workflow fails after its own execution ended | `RecordTemplateRunStatus(failed)` | unchanged | unchanged | unchanged |
| Discard | `discardTemplateRun` | → canceled | none (nothing is running) | none |

A plan with no changes stores no counts, as today. The UI reads a missing
`plan_summary` as "No changes" or "Nothing to destroy", and a present one as a
plan that changes something.

A step ends when the next one starts, or when its workflow execution ends.
There is no "step finished" write, so a step adds one control activity, as
today. The cost is that control-plane work between two steps, such as sealing
credentials for the next command, counts toward the earlier step. It takes
milliseconds.

A workflow that fails before it claims the run (the plan workflow's `carries`
check on a queued run, or the apply workflow's on an approved one) still gets
a workflow execution row, with `started_at = finished_at = now()`, so every
failure has a row that holds its error. It has no steps.

An apply that loses its claim to a discard ends without writing anything. It
never carried the run.

A terminal status write names its phase and ends only that phase's
execution. A plan workflow whose `FinishPlan` acknowledgement was lost fails
after its plan already ended: the run is waiting for approval, approved, or
already carried by its apply. That late write changes nothing, for the same
reason a lost claim does: the workflow no longer carries the run.

Retries stay idempotent:

- A status write that finds the run already in that status returns before
  touching anything.
- The claim's insert is `on conflict do nothing`.
- A step write whose step is the execution's running step does nothing. This
  check comes before the running step is closed, or a retried write would
  close its own row. A step write whose step already exists and has finished
  is an error: it is a second run of the step, not a retry, and swallowing it
  would corrupt the timeline.

Activity retries are off today (`MaximumAttempts: 1`), so these guard the
writes for when they are turned on.

## Counts

An approved apply or destroy applies its saved plan with `tofu apply tfplan`,
and today the runner reads nothing from its output (`runner.Run` returns
`Result{}` for both). It reads the closing line of every apply, the way it
already does for an auto-approved one:

- `summarizeApplyOutput` matches `Apply complete! Resources: …` and also
  `Destroy complete! Resources: …`, whichever tofu prints for a saved destroy
  plan.
- `runner.Run` counts `TerraformCommandApply` and `TerraformCommandDestroy`
  from their output, as `applyAutoApprove` does.
- The apply workflow sends the counts with every `applied` and `destroyed`
  event it records, not only an auto-approved one's.

A plan's `destroyed` event, for a destroy with nothing to destroy, still
carries none.

## Workflow changes

- `run` gains `phase domain.RunPhase` and `workflowID string`. `newRun` takes
  the phase and reads the ID from `workflow.GetInfo(ctx).WorkflowExecution.ID`.
  `TemplatePlanWorkflow` passes `RunPhasePlan`, `TemplateApplyWorkflow`
  `RunPhaseApply`.
- `TemplateRunStatusActivityInput` gains `Phase` and `WorkflowID`.
  `setStatus` fills them from `run`.
- `BeginApplyActivityInput` gains `WorkflowID`, which reaches
  `BeginTemplateApply`.
- `RecordTemplateRunStep` keeps its input. It finds the run's running
  workflow execution itself.
- `applyWorkflow.apply` sends the command's counts with its event, as above.
- Nothing else in the workflows changes. Which activity runs when is the same.

## Read model

The API keeps the run's current fields, derived, so the UI works unchanged
except for the rename:

- `step`: the latest step, by `id`, of the workflow execution that started
  last (highest `id`), or `''` if that execution has no steps.
  - Between an apply claiming the run and its first step, this is `''`.
    Today it is the plan's `saving_plan`, so an apply that fails there reads
    "Apply failed while saving plan". It will read "Apply failed".
  - The run's status changes at the same moment, so the run detail screen's
    progress tag (`status:step`), which its log queries key on, still changes
    whenever the run moves.
- `error_summary`: the latest failed workflow execution's error, or `''`.
- `plan_summary`: the `plan` execution's counts if the run has a `plan`
  execution, otherwise the `apply` execution's (auto-approve). A plan with no
  changes has none, so the run's `plan_summary` stays null.
- `started_at` becomes `created_at` in `domain.TemplateRun`, its JSON, the
  OpenAPI document and `web/src/api/types.ts`. Its uses in
  `TemplateRunHistory.tsx`, `RunDetailScreen.tsx` and the web tests' fixtures
  follow.

`GetTemplateRun` and `ListTemplateRuns` share one select that derives these
three fields with lateral subqueries on the executions' keys. The run list is
polled every 1.5 s and is not paginated (#270), so it stays one query, never
one per run.

`GET` for one run adds `executions`: its workflow executions in `id` order,
each with `phase`, `workflow_id`, `actor`, `status`, `started_at`,
`finished_at`, `error_summary`, `summary`, and its `steps` (`step`, `status`,
`started_at`, `finished_at`) in `id` order. The run list does not carry them.
They are read by their own repository method, which only `GetTemplateRun`
calls: every other run endpoint loads the run for authorization and has no use
for them.

A run's whole timeline, including the approval between its two executions:

```sql
select 'step' as kind, w.phase, s.step as name, s.status, s.started_at, s.finished_at
from template_run_step_executions s
join template_run_workflow_executions w on w.id = s.workflow_execution_id
where s.tenant_id = $1 and s.run_id = $2
union all
select 'approval', null, a.approved_by, null, a.approved_at, a.approved_at
from template_run_approvals a
where a.tenant_id = $1 and a.run_id = $2
order by started_at;
```

## Migration

`0029_template_run_executions.sql` creates both tables and makes the
`template_runs` changes. There is no backfill and no close-out: openplan is
pre-production, so a database with runs in flight is reset with
`docker compose down -v` rather than migrated. A `running` run left from
before 0029 has no workflow execution and would break the first invariant.
The local database also needs the reset for a second reason: it ran the
`feat/run-jobs-revive` stash's own `0029`, which dropped `template_run_logs`.

## Documents

- `docs/openapi.yaml`: `created_at`, the derived fields' descriptions, and the
  single-run response's `executions`.
- `docs/apply-run-sequence.md`: the writes `FinishPlan`, `BeginApply`, the
  `applied` event and the final status make. `docs/architecture.md` only
  mentions inserting the queued run, which does not change.

## Out of scope

- UI for executions and steps. The API exposes them; showing them is a
  separate change. So is reading a run's phase from its latest execution in
  place of `runStatusLabel`'s `plan_summary` heuristic.
- Moving logs onto step executions. `template_run_logs` stays keyed by run and
  log phase (`plan-init`, `apply`, …); a step and its log can be matched by
  phase and step, and folding them together is a later change.
- Retrying a failed apply on the same run.
- `TemplateSyncWorkflow`. Registrations are a different subject and keep
  their own row.

## Testing

- Store tests against real Postgres (`OPENPLAN_POSTGRES_TEST_DSN`):
  - each row of the write-rules table, including a retry of each write;
  - the fail-before-claim insert;
  - a step start closing the previous step, and the execution's end closing
    its last step as succeeded or failed;
  - a retried step doing nothing, and a second run of a finished step
    failing;
  - the one-running indexes rejecting a second running execution or step,
    and the composite foreign key rejecting a step whose `run_id` is not its
    execution's;
  - every `domain.AllTemplateRunSteps` value passing the step check;
  - the derived fields for a plan run, a plan with no changes
    (`plan_summary` null), an approved apply, an auto-approved apply, a
    failed apply, and an apply between its claim and its first step (`step`
    is `''`);
  - executions and steps read back in `id` order.
- Runner tests: an approved apply's and a destroy's closing line are counted.
- Workflow tests (`internal/workflows`): the status and claim inputs carry the
  phase and workflow ID, and an approved apply's `applied` event carries the
  counts.
- API tests: the run's JSON has `created_at` and the derived fields, and the
  single-run response has `executions` with their `steps`.
