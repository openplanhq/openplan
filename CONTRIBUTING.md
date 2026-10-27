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

That pulls prebuilt images and starts the application database, runs OpenFGA
inside the API, starts Temporal, and brings up the UI on http://localhost:5173.
Sign in through Dex as `admin@openplan.local` / `admin-local-only`.

**Use `localhost`, not `127.0.0.1`.** The redirect URI is derived from a single
`OPENPLAN_PUBLIC_URL`, and only that exact origin is registered with the
identity provider.

> [!IMPORTANT]
> `docker compose up` pulls the published image when it can, and only builds
> from source when the pull fails. That means an edit to the Go or the frontend
> can be silently shadowed by a published image. When you are changing
> openplan, build from source explicitly:
>
> ```bash
> OPENPLAN_PULL_POLICY=build docker compose up -d --build
> ```

For a faster edit loop, run the dependencies in Docker and the API and the UI
on the host. The executor stays in its container: it runs only in its Linux
image, and refuses to start anywhere else. The API and executor images have to exist before this
works, so build them once with `OPENPLAN_PULL_POLICY=build` as above:

```bash
# dependencies and the built images
OPENPLAN_PULL_POLICY=build docker compose up -d --wait

# then, in separate shells from the repository root
set -a && source .env && set +a && go run ./cmd/api
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

If you changed `docs/openapi.yaml`, run `make postman-collection` too. The
Postman collection in `postman/` is generated from the spec, and `go test ./...`
fails while the two disagree.

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

## Keeping upgrades working

An install upgrades by pulling the new release's images and restarting them on
the same volumes, with runs still in flight. Every stack, run, credential, grant
and session has to survive that, so what one release writes, the next has to
read. That holds from the first release after v0.3.0 on. Releases up to v0.3.0
were allowed to need a wipe, which is how migration `0030` could drop a table
outright.

- **Schema.** Migrations in `internal/postgres/migrations` are append-only:
  never edit one that has merged, add a new one. They are also data-preserving.
  Don't drop a table or column that still holds data until its data has moved,
  and rename by adding the new column, copying into it, and dropping the old
  one in a later release.
- **Work-queue payloads.** Rows in `work_queue` outlive the binary that wrote
  them, and the new one decodes them. The payloads (`StartTemplatePlanPayload`
  and the rest, in `internal/app`) are the workflow inputs, so they change only
  additively: a new field must mean what it meant before when it is missing.
  Anything else is a new payload kind.
- **The OpenFGA model.** The api writes `internal/authorization/authorization-model.fga`
  at boot, and every stored tuple names a type and relation from it. Renaming
  or removing one orphans those tuples. Add the new name, move the tuples, and
  remove the old name in a later release.
- **Temporal workflows.** See below.
- **Configuration.** A renamed or removed environment variable keeps working
  for one release, or the api refuses to start with an error naming its
  replacement, the way it already does for `OPENPLAN_ROOT_PASSWORD`
  (`internal/config/auth.go`).

The upgrade test in CI ([#313](https://github.com/openplanhq/openplan/issues/313))
will check the whole of this by upgrading a stack with state in it from the
previous release. Until it lands, review holds the line.

### Changing a workflow

Temporal replays a running workflow's history against the code the worker has
now. A change to the commands a workflow issues, in their order or their kind,
fails every execution that was running across the upgrade with a
non-determinism error, and the worst case is an apply that stops with the
infrastructure half changed.

Commands are what a workflow asks Temporal to do: scheduling an activity, a
timer, a session, a child workflow, a side effect. A change alters them when it
adds, removes or reorders one, renames an activity, or changes the logic that
decides which ones run. Activity code, activity options (timeouts, retry
policies) and the values a workflow passes do not, though inputs and results are
in the history too, so their types change only additively, like the payloads
above.

The `workflow-replay` CI job checks this on every change to
`internal/workflows` or `internal/domain`. It checks out the last release,
records that release's histories against a real Temporal, and replays them
against your change. Run the same locally with Compose's Temporal up:

```bash
docker compose up -d --wait temporal
make workflow-replay
```

When it fails, branch your change on a version marker, so a run that started
under the release keeps the old commands and a new run takes the new ones:

```go
if workflow.GetVersion(ctx, "record-plan-summary", workflow.DefaultVersion, 1) == 1 {
	// the new command
}
```

The old branch can go once no supported upgrade starts from a release that
lacks the marker.

The recordings come from `TestRecordHistories`, one scenario per path through
a workflow, and nothing it records is committed. A new path needs a scenario of
its own, and a new workflow needs at least one: the next release records from
them.

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