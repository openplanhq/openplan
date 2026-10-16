-- A run log's name is chosen by the workflow (domain.RunLogName), so
-- the database no longer lists every name it may take. Listing them made each
-- new log a migration (0024 was one) and kept a dead 'clone' entry alive.
-- The check that stays is the shape the executor, the API and the object key
-- all rely on: one lowercase path segment (domain.ValidLogName).

alter table template_run_logs drop constraint template_run_logs_phase_check;

alter table template_run_logs add constraint template_run_logs_phase_check check (
	phase ~ '^[a-z][a-z0-9-]{0,62}$'
);
