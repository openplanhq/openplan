# Local development

Instructions for working on openplan itself. To simply run it, see the
[README](../README.md).

## Running the app on the host

Run the API, executor, and UI as host processes against the containerised
dependencies. This gives you fast rebuilds and a debugger.

Start the dependencies:

```bash
cp .env.example .env
docker compose up -d --wait
```

That stack includes Dex, the local OIDC provider, at
`http://dex.localhost:5556/dex` (see `docs/authentication.md`). An issuer with
no provider reachable behind it is a boot failure: the API discovers the
provider while it starts. For a host-run API, `dex.localhost` resolves to
loopback, where Compose publishes Dex's port.

Nothing to copy afterwards. OpenFGA runs inside the API and resolves its store
and authorization model from the model in this repository at startup.

Compose does not inject `.env` into containers or host processes, so the API
and executor read those values from your shell. Load them and start each process
in its own terminal:

```bash
set -a
source .env
set +a
go run ./cmd/api
```

```bash
set -a
source .env
set +a
go run ./cmd/executor
```

```bash
cd web
npm install
npm run dev
```

The Vite dev server proxies `/v1/*` and `/healthz` to the Go API. It binds port
`5173`, the same port the `web` container uses, so run one or the other rather
than both.

`npm run dev` binds and prints `http://127.0.0.1:5173` — exactly how someone
hits the login trap below. Open `http://localhost:5173` instead: the redirect
URI is derived from a single `OPENPLAN_PUBLIC_URL`, so only that exact origin is
registered with an identity provider, and `127.0.0.1` fails OIDC sign-in with
an invalid `redirect_uri`.

## The authorization model

`internal/authorization/authorization-model.fga` is the source of truth and the
only form of the model in the repository. Edit and review permission changes
there — the DSL is the form in which a wrong permission is visible.

The API embeds the DSL and transforms it to OpenFGA's wire format in process at
startup, so there is nothing to regenerate after a change and no generated
artifact that can drift. A malformed DSL fails
`go test ./internal/authorization/...` immediately.

A model change writes a new immutable version the next time the API starts, and
there is no identifier to update anywhere: the store and model are resolved by
matching the repository model, not by configuration.

The transform rejects a syntax error but not a reference to a relation that
does not exist, so `define can_view: ownr` would transform cleanly and be
refused by OpenFGA at write time. The permission matrix in
`internal/authorization/authorization-model-tests.fga.yaml` is what catches that, along with
every permission that is merely wrong. Run it on any model change; it needs the
[`fga` CLI](https://github.com/openfga/cli) on `PATH`:

```bash
cd internal/authorization && fga model test --tests authorization-model-tests.fga.yaml
```

## Tests

```bash
go test ./...
gofmt -l cmd internal
node scripts/verify-auth-compose.mjs
```

The tests that need Postgres skip without one, including the differential test
that guards `internal/authorization/write.go` against upstream drift. With the
Compose stack up, run them for real:

```bash
make differential-test
```

```bash
cd web
npm test
npm run build
```

## Layout

```text
cmd/                  API and executor entry points
internal/app/         application use cases and ports
internal/api/         HTTP transport
internal/postgres/    product persistence and migrations
internal/queue/       durable work queue
internal/temporal/    Temporal client adapter
internal/workflows/   deterministic workflows
internal/activities/  side-effecting Temporal activities
internal/runseal/     per-run sealing of secrets sent to the executor
internal/runner/      OpenTofu execution
internal/authn/       token verification
internal/authorization/  embedded OpenFGA: model, bootstrap, checks, transactional tuple writes
web/                  Vite UI
```
