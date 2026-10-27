-- The queue kind that starts a run's plan workflow was start_template_run,
-- though an auto-approved apply goes out as start_template_apply instead.
-- Queued rows carry the old kind and would find no handler, so they move with
-- it.
update work_queue set kind = 'start_template_plan' where kind = 'start_template_run';
