# Contributing to openplan

openplan is an MVP with one contributor so far, so contributions are genuinely
welcome, including your first PR to someone else's project.

## Getting set up

You need Docker. You do not need Go or Node for running the app, only for
changing it.

```bash
git clone https://github.com/openplanhq/openplan.git
cd openplan
cp .env.example .env
docker compose up -d --wait
```

That builds every image from source, which takes a few minutes. It creates the
application database, runs OpenFGA inside the API, starts Temporal, and brings
up the UI on http://localhost:5173. Sign in through Dex as
`admin@openplan.local` / `admin-local-only`.

**Use `localhost`, not `127.0.0.1`.** The redirect URI is derived from a single
`OPENPLAN_PUBLIC_URL`, and only that exact origin is registered with the
identity provider.

For a faster edit loop, run the dependencies in Docker and the Go and Node
processes on the host:

```bash
# dependencies only
docker compose up -d --wait

# then, in separate shells from the repository root
set -a && source .env && set +a && go run ./cmd/api
set -a && source .env && set +a && go run ./cmd/executor
cd web && npm install && npm run dev
```

`npm run dev` binds 127.0.0.1:5173, and the Compose `web` service binds the same
port, so stop one before starting the other.

## Before you open a pull request

Run all four. CI runs exactly these, so anything that fails locally will fail
there:

```bash
# from the repository root
go test ./...
make lint

# from web/
npm test
npm run build
```

`make lint` downloads golangci-lint on first run and caches it after that.

### The authorization differential test

`internal/authorization/write.go` is a transcription of OpenFGA's own write path
with its transaction management removed, so a tuple write can join ours. A
transcription diverges silently: an upstream change that adds a column still
compiles here and starts writing subtly wrong rows.

`make differential-test` is the only thing that catches that, and it needs a real
Postgres. **If your change touches authorization at all, especially after any
`go get github.com/openfga/openfga@...`, run it:**

```bash
docker compose up -d postgres
make differential-test
```

It skips silently without a database, and a skipped guard is no guard.

## Commit messages

Short, imperative, lowercase, with a prefix:

```
feat(web): rebuild the Variables tab
fix: keep a known run's lock when a refetch fails
test: cover the run seal on a retried activity
refactor: share the stack_templates column list
docs: describe dex as the only identity provider
```

## Pull requests

Explain the behavior change, not the diff. Call out configuration or migration
impact, link the issue it closes, and list the commands you ran to validate it.
Include screenshots when you change UI. There is a script for this:

```bash
scripts/drive-web.mjs signs in through Dex and screenshots screens headlessly.
See the script's header for flags.
```

## Reporting bugs

Use the bug report template. The most useful thing you can include is what you
expected, what happened, and the run's output. Every run keeps its full
`init`/`plan`/`workspace` logs, and they are the fastest route to a cause.

## Security

Do not open a public issue for a security problem. The project has no private
advisory process yet; open an issue that says only that you have found something
and how to reach you.

## License

By contributing you agree that your work is licensed under Apache 2.0, the same
terms as the project.