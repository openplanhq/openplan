-- A run log's identity is its run phase and number, (run_phase, seq). The name
-- it is stored and read under, phase, is plan-1 or apply-3 (domain.RunLogName),
-- so it is now derived from them rather than written beside them: the two can
-- never disagree, and this table no longer repeats the name pattern 0031
-- checked. 0032 is left as applied: databases have run it.
--
-- Existing rows are dropped rather than rewritten, as 0032 did: openplan is
-- pre-production.

delete from template_run_logs;

alter table template_run_logs
	drop constraint template_run_logs_pkey,
	drop constraint template_run_logs_run_phase_seq_key,
	drop column phase,
	add column phase text generated always as (run_phase || '-' || seq::text) stored,
	add constraint template_run_logs_pkey primary key (tenant_id, run_id, run_phase, seq);
