-- A run log records the command that wrote it and the phase that ran it, so
-- the run's page can show "tofu init" under its Plan or Apply rather than the
-- log's storage name. The workflow supplies both; the executor does not.
--
-- Existing rows are dropped rather than backfilled: openplan is
-- pre-production, and the local stack is reset with `docker compose down -v`
-- rather than migrated in place. Their objects stay in the store, unreferenced.

delete from template_run_logs;

alter table template_run_logs
	add column run_phase text not null,
	add column command text not null,
	add constraint template_run_logs_run_phase_check check (run_phase in ('plan', 'apply')),
	add constraint template_run_logs_command_check check (
		command in ('init', 'select_workspace', 'plan', 'plan_destroy', 'apply', 'destroy', 'apply_auto_approve')
	);
