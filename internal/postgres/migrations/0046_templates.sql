-- The catalogue template was source_templates here and Template on the wire.
-- It takes the wire's name, and every column that points at it follows.
alter table source_templates rename to templates;
alter table templates rename constraint source_templates_pkey to templates_pkey;
alter index source_templates_identity_idx rename to templates_identity_idx;
alter index source_templates_tenant_id_id_idx rename to templates_tenant_id_id_idx;
alter index source_templates_tenant_id_created_at_id_idx rename to templates_tenant_id_created_at_id_idx;

alter table template_revisions rename column source_template_id to template_id;
alter table template_revisions rename constraint template_revisions_source_template_id_fkey to template_revisions_template_id_fkey;
alter index template_revisions_tenant_id_source_template_id_created_at_id_i rename to template_revisions_tenant_id_template_id_created_at_id_idx;

alter table stack_templates rename column source_template_id to template_id;
alter table stack_templates rename constraint stack_templates_source_template_id_fkey to stack_templates_template_id_fkey;
alter index stack_templates_source_template_id_idx rename to stack_templates_template_id_idx;

alter table template_syncs rename column source_template_id to template_id;
alter table template_syncs rename constraint template_syncs_source_template_id_fkey to template_syncs_template_id_fkey;
alter index template_syncs_source_template_id_requested_at_id_idx rename to template_syncs_template_id_requested_at_id_idx;

alter table template_runs rename column source_template_id to template_id;

-- Queued work carries Go structs as JSON under their field names, and would
-- decode with an empty template, so the old key moves with the field.
update work_queue
set payload = (payload - 'SourceTemplateID') || jsonb_build_object('TemplateID', payload -> 'SourceTemplateID')
where payload ? 'SourceTemplateID';
