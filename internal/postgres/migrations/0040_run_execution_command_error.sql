-- What the failed Terraform command said, its first error block, kept apart
-- from the workflow's wrapper in error_summary so a failed run can lead with
-- it (issue 522). Empty for a failure outside a Terraform command.
alter table template_run_workflow_executions add column command_error text not null default '';
