-- The paged lists (#390) read newest first and resume after the last row a
-- page held, as a (created_at, id) keyset. These indexes are those orders, so
-- a page is an index range scan rather than a sort of the tenant's rows.

create index stacks_tenant_id_created_at_id_idx
	on stacks (tenant_id, created_at desc, id desc);

create index template_revisions_tenant_id_created_at_id_idx
	on template_revisions (tenant_id, created_at desc, id desc);

-- A caller's queue is listed by (created_at, id); id breaks the tie between
-- items queued in the same instant, which created_at alone left unordered.
drop index work_queue_actor_idx;
create index work_queue_actor_idx
	on work_queue (tenant_id, actor_subject, created_at desc, id desc);
