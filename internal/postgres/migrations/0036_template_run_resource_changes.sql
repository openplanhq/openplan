-- Each resource a run's plan changes, for the run's Changes view (#420). The
-- executor reads them from `tofu show -json` inside the plan's sandbox, masks
-- every sensitive value there, and uploads them beside the plan's log;
-- FinishPlan records them here in the transaction that records the plan's
-- counts. The plan JSON itself never leaves the executor.
--
-- position is the reading order the executor sorted them in (removals first,
-- then by address), and the order the API pages them in.

create table template_run_resource_changes (
	tenant_id              text    not null,
	run_id                 text    not null references template_runs (id) on delete cascade,
	position               integer not null,
	address                text    not null,
	previous_address       text    not null default '',
	module_address         text    not null default '',
	mode                   text    not null,
	type                   text    not null,
	name                   text    not null,
	provider_name          text    not null,
	deposed                text    not null default '',
	-- Free text on purpose: an action tofu adds in a later version is stored
	-- as its own word rather than refused.
	action                 text    not null,
	create_before_destroy  boolean not null default false,
	action_reason          text    not null default '',
	importing_id           text    not null default '',
	replace_paths          jsonb,
	before                 jsonb,
	after                  jsonb,
	primary key (tenant_id, run_id, position),
	constraint template_run_resource_changes_position_check check (position >= 0)
);

-- What the run's list is: complete, cut short, or not listed at all although
-- the plan had changes. Null for a run whose plan has no changes or has not
-- finished. total counts every change the plan had, listed or not.
alter table template_runs
	add column plan_changes_state text,
	add column plan_changes_total integer,
	add constraint template_runs_plan_changes_state_check check (
		plan_changes_state is null or plan_changes_state in ('complete', 'truncated', 'unavailable')
	),
	add constraint template_runs_plan_changes_total_check check (
		(plan_changes_total is null) = (plan_changes_state is null or plan_changes_state = 'unavailable')
	);
