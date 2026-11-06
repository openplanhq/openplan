-- What a failure event was an attempt at, why it failed, and on what. Without
-- these a refusal only says that someone was refused something. Older rows keep
-- '': they never recorded it.
alter table security_audit_log
    add column operation text not null default '',
    add column reason text not null default '',
    add column target_id text not null default '';
