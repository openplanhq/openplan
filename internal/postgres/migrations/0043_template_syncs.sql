-- A row here is one sync of a template, and everything around it says so: the
-- route (/syncs), the workflow (TemplateSyncWorkflow) and the API's
-- latest_sync. "Register" means creating the template, so the table, its
-- constraints and its indexes take the sync's name.
alter table template_registrations rename to template_syncs;

alter table template_syncs rename constraint template_registrations_pkey to template_syncs_pkey;
alter table template_syncs rename constraint template_registrations_status_check to template_syncs_status_check;
alter table template_syncs rename constraint template_registrations_step_check to template_syncs_step_check;
alter table template_syncs rename constraint template_registrations_source_template_id_fkey to template_syncs_source_template_id_fkey;

alter index template_registrations_tenant_id_id_idx rename to template_syncs_tenant_id_id_idx;
alter index template_registrations_tenant_id_status_idx rename to template_syncs_tenant_id_status_idx;
alter index template_registrations_source_template_id_requested_at_id_idx rename to template_syncs_source_template_id_requested_at_id_idx;
