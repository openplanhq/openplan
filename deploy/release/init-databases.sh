#!/bin/sh
# Creates one database per component on the shared local Postgres. Runs once,
# when Postgres initializes an empty data directory.
#
# Written for docker-compose.release.yaml, which cannot bind-mount a script
# from a repository it does not require. Kept byte-equivalent in behaviour to
# the project's deploy/postgres/init.sh, with one difference: the roles are
# created idempotently, so re-running against a populated volume is a no-op
# rather than an error.
#
# OpenFGA has no entry here: it is embedded in the API and its tables live in
# the application database, created by the API at startup. That is what lets a
# tuple write join the same transaction as the domain write that caused it.
set -eu

# The host matters: this script runs in a container built from the postgres
# image, so it has a psql client but no server of its own. The repository's
# deploy/postgres/init.sh gets away with a local socket because it is bind-
# mounted INTO the postgres container; a standalone compose file has to reach
# the postgres service over the network instead.
psql -v ON_ERROR_STOP=1 -h postgres -U openplan -d postgres <<-'EOSQL'
	-- The application database. The api service connects to this one, and the
	-- API creates its own tables at startup.
	SELECT 'CREATE DATABASE openplan OWNER openplan'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'openplan')\gexec

	SELECT 'CREATE ROLE dex LOGIN PASSWORD ''dex-local-only'''
	WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'dex')\gexec
	SELECT 'CREATE DATABASE dex OWNER dex'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'dex')\gexec

	SELECT 'CREATE ROLE temporal LOGIN PASSWORD ''temporal'''
	WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'temporal')\gexec
	SELECT 'CREATE DATABASE temporal OWNER temporal'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'temporal')\gexec
	SELECT 'CREATE DATABASE temporal_visibility OWNER temporal'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'temporal_visibility')\gexec
EOSQL
