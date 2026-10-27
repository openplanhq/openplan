-- A run's ref is the revision's source_ref, copied at start; nobody selects
-- it, so it carries the revision's name for it.
alter table template_runs rename column selected_ref to source_ref;

-- Queued starts carry the workflow input as JSON under the Go field's old
-- name, and would decode with an empty ref, so they move with it.
update work_queue
set payload = (payload - 'SelectedRef') || jsonb_build_object('SourceRef', payload -> 'SelectedRef')
where payload ? 'SelectedRef';
