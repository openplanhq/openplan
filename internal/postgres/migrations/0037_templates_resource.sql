-- A template is a resource of its own (#461): registering creates its
-- source_templates row before any sync, and deleting it removes what hangs off
-- it. The foreign keys below are what make that safe.
--
-- The two keys on existing columns are added not valid: they bind every new
-- write and every delete, without failing on a development database that still
-- holds a pre-0006 row with an empty id. Pre-production, so nothing is
-- backfilled or validated.

-- A template's revisions go with it; their variables already cascade from them.
alter table template_revisions
	add constraint template_revisions_source_template_id_fkey
	foreign key (source_template_id) references source_templates (id)
	on delete cascade not valid;

-- A template that any stack has installed, in whatever lifecycle, can't be
-- deleted. A key rather than a check in the delete, because the key's
-- triggers lock the row and see concurrent commits, so an install and a
-- delete racing each other can't both succeed.
alter table stack_templates
	add constraint stack_templates_source_template_id_fkey
	foreign key (source_template_id) references source_templates (id)
	on delete restrict not valid;

-- The restrict check on delete looks stack templates up by template.
create index stack_templates_source_template_id_idx
	on stack_templates (source_template_id);

-- A registration (a sync, on the wire) belongs to the template it syncs. Null
-- on rows written before this migration.
alter table template_registrations
	add column source_template_id text
	references source_templates (id) on delete cascade;

create index template_registrations_source_template_id_idx
	on template_registrations (source_template_id);

-- A template's revisions page, newest first; the leading columns still serve
-- every lookup the index it replaces did.
drop index template_revisions_tenant_id_source_template_id_idx;
create index template_revisions_tenant_id_source_template_id_created_at_id_idx
	on template_revisions (tenant_id, source_template_id, created_at desc, id desc);

-- The templates page, newest first, as a (created_at, id) keyset.
create index source_templates_tenant_id_created_at_id_idx
	on source_templates (tenant_id, created_at desc, id desc);
