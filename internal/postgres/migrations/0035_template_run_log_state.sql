-- A run log now exists from the moment its command starts, before its output
-- reaches the object store, so a row no longer implies an object. state says
-- where the output is: live while the command runs, then complete (the object
-- columns are set) or lost (lost_reason says why there is no object). The
-- checks tie each state to exactly the columns it carries, so a row can never
-- claim an object it lacks or lose one without a reason.
--
-- Existing rows are dropped rather than given a state, as 0032 and 0033 did:
-- openplan is pre-production.

delete from template_run_logs;

alter table template_run_logs
	add column state text not null,
	add column lost_reason text,
	alter column object_key drop not null,
	alter column content_type drop not null,
	alter column size_bytes drop not null,
	alter column uploaded_at drop not null,
	add constraint template_run_logs_state_check check (state in ('live', 'complete', 'lost')),
	add constraint template_run_logs_lost_reason_check check (
		lost_reason is null or lost_reason in ('upload_failed', 'executor_gone', 'run_terminated', 'record_failed')
	),
	add constraint template_run_logs_state_object_check check (
		(state = 'complete') = (object_key is not null and uploaded_at is not null and content_type is not null and size_bytes is not null)
	),
	add constraint template_run_logs_state_lost_reason_check check (
		(state = 'lost') = (lost_reason is not null)
	);
