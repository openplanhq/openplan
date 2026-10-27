-- An installed template's key was called component_key, though nothing else
-- calls an installed template a component; it is the stack template's key.
alter table stack_templates rename column component_key to key;
alter index stack_templates_active_component_key_idx rename to stack_templates_active_key_idx;
