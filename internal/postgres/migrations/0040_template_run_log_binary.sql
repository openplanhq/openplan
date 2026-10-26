-- Which CLI a command ran under, tofu or terraform (#152). A log's command
-- line is derived from this column and command, so a deployment that switches
-- binaries leaves its earlier logs saying what actually ran. Every row so far
-- ran under tofu, the only binary openplan had.
alter table template_run_logs
	add column terraform_binary text not null default 'tofu',
	add constraint template_run_logs_terraform_binary_check check (terraform_binary in ('tofu', 'terraform'));
