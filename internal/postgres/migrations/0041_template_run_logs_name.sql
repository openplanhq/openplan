-- The column 0033 generates is the log's name (plan-1, apply-3), not its
-- phase: run_phase beside it is the phase, and the API already calls it name.
alter table template_run_logs rename column phase to name;
