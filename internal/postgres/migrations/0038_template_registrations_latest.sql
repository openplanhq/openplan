-- Every template read looks up its newest sync (#460): the registration with
-- the greatest requested_at, ties broken by id. This index answers that with
-- one probe per template, and its leading column still serves the cascade
-- from source_templates that the index it replaces did.
drop index template_registrations_source_template_id_idx;
create index template_registrations_source_template_id_requested_at_id_idx
	on template_registrations (source_template_id, requested_at desc, id desc);
