#!/bin/sh
# Creates openplan's, Dex's and Temporal's databases and roles on RDS, as the
# master user. Runs as the control task's db-init container on every task
# start, so it must be idempotent: everything is created only if missing, and
# each role's password is set every time, which keeps it equal to SSM's.
#
# PostgreSQL 16 semantics: CREATE DATABASE ... OWNER x requires being able to
# SET ROLE x, which the creator of a role no longer gets implicitly. Hence the
# GRANT of each role to the master user.
#
# Passwords arrive in the environment and are passed in as psql variables,
# quoted by format(%L), so none is ever spliced into SQL by the shell.
set -eu

psql -v ON_ERROR_STOP=1 \
  -v openplan_pw="$OPENPLAN_DB_PASSWORD" \
  -v dex_pw="$DEX_DB_PASSWORD" \
  -v temporal_pw="$TEMPORAL_DB_PASSWORD" <<'EOSQL'
SELECT format('CREATE ROLE %I LOGIN', r)
FROM unnest(ARRAY['openplan', 'dex', 'temporal']) AS r
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = r)\gexec

SELECT format('ALTER ROLE openplan PASSWORD %L', :'openplan_pw')\gexec
SELECT format('ALTER ROLE dex PASSWORD %L', :'dex_pw')\gexec
SELECT format('ALTER ROLE temporal PASSWORD %L', :'temporal_pw')\gexec

GRANT openplan, dex, temporal TO CURRENT_USER;

SELECT format('CREATE DATABASE %I OWNER %I', d, o)
FROM (VALUES
  ('openplan', 'openplan'),
  ('dex', 'dex'),
  ('temporal', 'temporal'),
  ('temporal_visibility', 'temporal')
) AS t(d, o)
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = d)\gexec
EOSQL
