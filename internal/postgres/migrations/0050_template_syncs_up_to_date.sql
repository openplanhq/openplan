-- Whether a completed sync found a commit already recorded, written by the
-- transaction that records what the sync found. It used to be worked out on
-- read by comparing the revision's created_at (the worker's clock) with the
-- sync's requested_at (the api's), which clock skew could misread.
alter table template_syncs add column up_to_date boolean not null default false;

-- Syncs that finished before this column existed keep the reading they had.
update template_syncs s
set up_to_date = true
from template_revisions r
where s.status = 'completed'
	and r.tenant_id = s.tenant_id
	and r.id = s.template_revision_id
	and r.created_at < s.requested_at;
