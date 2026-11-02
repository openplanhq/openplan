-- The commit's subject line, read at sync, so a revision can say what it is.
-- Revisions synced before this keep '': pre-production, so no backfill.
alter table template_revisions add column commit_subject text not null default '';
