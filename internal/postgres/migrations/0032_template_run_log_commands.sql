-- A run log records what wrote it: the run phase, its number among that
-- phase's logs, and the Terraform command. The workflow supplies all three; the
-- executor does not. The page shows "tofu init" under its Plan or Apply, and
-- lists a phase's logs by seq, the order they ran in, rather than by
-- uploaded_at, which the executor's clock sets.
--
-- Its name, phase, is now just run_phase and seq (plan-1, apply-2), so a
-- command run twice in a phase gets a log each time.
--
-- Existing rows are dropped rather than backfilled: openplan is
-- pre-production, and the local stack is reset with `docker compose down -v`
-- rather than migrated in place. Their objects stay in the store, unreferenced.

delete from template_run_logs;

alter table template_run_logs
	add column run_phase text not null,
	add column seq integer not null,
	add column command text not null,
	add constraint template_run_logs_run_phase_check check (run_phase in ('plan', 'apply')),
	add constraint template_run_logs_seq_check check (seq >= 1),
	add constraint template_run_logs_command_check check (
		command in ('init', 'select_workspace', 'plan', 'plan_destroy', 'apply', 'destroy', 'apply_auto_approve')
	),
	add constraint template_run_logs_run_phase_seq_key unique (tenant_id, run_id, run_phase, seq);
