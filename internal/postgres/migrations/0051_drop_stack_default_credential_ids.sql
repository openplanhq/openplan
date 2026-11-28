-- Stacks once listed default credential IDs. Nothing has read them since
-- credentials became stack- and template-scoped rows (scoped_credentials), so
-- the column goes before 1.0 freezes the API that exposed it.
alter table stacks drop column default_credential_ids_json;
