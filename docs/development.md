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

Those services publish prebuilt images. The api and executor services do too,
and a published image can shadow a local edit, so when you are changing
openplan start the dependencies and force a source build together:

```bash
OPENPLAN_PULL_POLICY=build docker compose up -d --wait
```

Nothing to copy afterwards. OpenFGA runs inside the API and resolves its store
and authorization model from the model in this repository at startup.

To run the API on the host instead, build the images once with the command
above, then bring the stack down to the dependencies and the executor, and
start the API in its own terminal. The executor stays in Compose: it runs only
in its Linux image, as root with the privileges runsc needs, starting each
activity in a gVisor sandbox of its own (see "Runner Security" in
[architecture.md](architecture.md)), and refuses to start anywhere else.
Compose does not inject `.env` into containers or host processes, so a host
process reads those values from your shell:

```bash
docker compose up -d postgres temporal dex executor

set -a
source .env
set +a
go run ./cmd/api
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

The model is validated as it loads, so a syntax error or a reference to a
relation that does not exist (`define can_view: ownr`) fails `go test` too.
What validation cannot catch is a permission that is well-formed and wrong.
The permission matrix in
`internal/authorization/authorization-model-tests.fga.yaml` catches that: it
asserts which roles hold each permission and which do not, and
`go test ./internal/authorization/` runs it against the embedded model, so CI
runs it on every change. The same test fails when the model defines a `can_*`
permission the matrix never asserts, so a new permission lands with its checks.

The file is in the format the [`fga` CLI](https://github.com/openfga/cli)
reads, so `fga model test` runs it as well:

```bash
cd internal/authorization && fga model test --tests authorization-model-tests.fga.yaml
```

The Go runner implements only the parts of that format the matrix uses, and
rejects the rest (`list_objects`, `list_users`, conditions) rather than
skipping it. Extend `model_matrix_test.go` when the matrix needs one.

## Tests

```bash
go test ./...
gofmt -l cmd internal
node scripts/verify-auth-compose.mjs
```

The tests that need Postgres skip without one. CI runs every one of them
against a Postgres service. Locally, with the Compose stack up,
`make differential-test` runs the authorization ones, including the
differential test that guards `internal/authorization/write.go` against
upstream drift:

```bash
make differential-test
```

and the same DSN runs the rest:

```bash
OPENPLAN_POSTGRES_TEST_DSN='postgres://openplan:openplan@localhost:55432/openplan_test?sslmode=disable' \
  go test ./internal/postgres/ ./internal/migrationlock/ ./cmd/api/
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
internal/authentication/  OIDC sign-in, ID-token verification, sessions
internal/authorization/  embedded OpenFGA: model, bootstrap, checks, transactional tuple writes
web/                  Vite UI
```
