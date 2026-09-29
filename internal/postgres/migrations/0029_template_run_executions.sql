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

-- A run's step, error and counts belong to its workflow executions from here
-- on, and reads derive them from there. Dropping step drops its check too.
alter table template_runs
	drop column step,
	drop column error_summary,
	drop column plan_add,
	drop column plan_change,
	drop column plan_destroy;

-- started_at was always set when the run was queued, not when any work
-- started; when work starts is now its executions' started_at.
alter table template_runs rename column started_at to created_at;
