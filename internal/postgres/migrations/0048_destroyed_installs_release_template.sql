-- A destroyed install lets go of its template (#596). The key on template_id
-- refuses deleting a template that any row names, and a key can't see
-- lifecycle, so a destroyed install, kept for its runs, held its template
-- forever. Null is the link ended: Postgres never checks a null key, and the
-- restrict key now counts only installs that aren't destroyed.
alter table stack_templates
	alter column template_id drop not null,
	alter column template_id drop default;

update stack_templates set template_id = null where lifecycle = 'destroyed';
