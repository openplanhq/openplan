# openplan Terraform Platform

> [!WARNING]
> openplan is not production ready. It is an MVP baseline intended for local
> development, evaluation, and continued hardening.

## What it is

openplan turns Terraform and OpenTofu modules into infrastructure you can hand to
a team. Register a module once, and anyone with access can stand up their own
copy of it from a web UI — with their own variables, their own credentials, and
a full history of every change.

## Demo

[![Watch the openplan demo](https://img.youtube.com/vi/2oFE764dnIs/maxresdefault.jpg)](https://youtu.be/2oFE764dnIs)

## Screenshots

**Stacks.** Everything a team has running, in one list.

![The stacks list](docs/ss/stacks-list.png)

**Templates and runs.** The templates a stack is built from, the plan and apply
controls, and the input variables for this stack — on one screen.

![A stack's template tab, showing runs and variables](docs/ss/stack-template-and-runs.png)

**Runs and logs.** Every run keeps its status, its timings, and the full
`init`, `plan` and `workspace` output.

![A completed plan run with its logs](docs/ss/run-logs.png)

**Credentials.** Cloud credentials are write-only and injected only while
Terraform runs.

![The environment credentials tab](docs/ss/environment-credentials.png)

**Access.** Grant people a role on the stacks they need, not on everything.

![The stack access tab, showing grants and role assignment](docs/ss/stack-access.png)

**History, and a way out.** Past runs stay linked, and destroying is an
explicit, guarded action.

![Run history and the destroy danger zone](docs/ss/run-history-and-destroy.png)

## What you can do

- **Register templates.** Point openplan at a Git repository and a revision. That
  becomes a reusable template anyone on the platform can install.
- **Compose stacks.** A stack groups the templates that make up one environment
  or one service, each with its own configuration.
- **Configure per stack.** Set input variables and attach cloud credentials
  without editing anyone's Terraform.
- **Plan, apply and destroy from the browser.** Runs are durably orchestrated,
  so a plan or apply survives a restart rather than being lost halfway.
- **Watch runs and read logs.** Every run keeps its output and its history.
- **Upgrade deliberately.** When a template gets a new revision, upgrade a stack
  to it as an explicit, reviewable step instead of drifting silently.
- **Control access per stack.** Grant people access to the stacks they need,
  rather than to everything.
- **Sign in with SSO.** Authentication is standard OIDC. The local stack ships
  Dex as its identity provider, so there is nothing to wire up to try it.

openplan requires no session or timeout configuration on your identity provider:
signed-in sessions are openplan's own record, bounded by its own absolute and
idle timeouts, independent of whatever token lifespan or SSO idle timeout the
provider runs. To get immediate revocation when a user signs out or is
disabled at the IdP, instead of waiting for those bounds, point the provider's
back-channel logout at the API's `/v1/auth/backchannel-logout` endpoint and
enable session-required logout so it includes `sid`. Without that, sessions
still end at their own bounds — nothing breaks.

That URL must be **reachable from the identity provider**, not from the
browser — a back-channel logout is a server-to-server POST, not a redirect
the browser follows. The two addresses differ whenever the IdP runs on an
internal network or behind split-horizon DNS, which is why it is a separate
setting, `OPENPLAN_BACKCHANNEL_LOGOUT_URL`, rather than always derived from
`OPENPLAN_PUBLIC_URL`: an IdP running in a container resolves
`http://localhost:5173` (`OPENPLAN_PUBLIC_URL`) to its own loopback, not the
host's browser-facing port, so it would need
`http://api:8081/v1/auth/backchannel-logout` instead.

## Running it locally

Requires Docker. No Go or Node toolchain.

> [!NOTE]
> **Upgrading an existing local stack?** Run `docker compose down -v` before
> starting it back up. OpenFGA's tables moved out of their own database and
> into the application database, so tuples written before the move are not
> carried over. Dex's database is created only when Postgres initializes an
> empty volume, so on an old volume Dex cannot start, and the API waits on it.
> Grants held by the old `root` local account do not carry over either: root
> is now a Dex user.

**1. Start everything.**

```bash
cp .env.example .env
docker compose up -d --wait
```

First run builds from source, so it takes a few minutes. Later runs are cached.

There is no second step. OpenFGA runs inside the API, which creates its tables
in the application database and resolves the store and authorization model from
the model in this repository at startup. Nothing has to be recorded between
phases, and nothing has to be pasted into `.env`.

**2. Open http://localhost:5173** and sign in through Dex as
`admin@openplan.local` / `admin-local-only`. That user is root: the API grants
the platform `root` relationship to its `sub` (`OPENPLAN_ROOT_SUBJECT`) at every
boot.

> [!IMPORTANT]
> Use `localhost`, not `127.0.0.1`. The redirect URI is derived from a single
> `OPENPLAN_PUBLIC_URL`, so only that exact origin is registered with an
> identity provider — `127.0.0.1` fails OIDC sign-in with an invalid
> `redirect_uri`.

### Stopping it

```bash
docker compose down
```

### Starting over

To wipe everything and begin from a clean slate:

```bash
docker compose down -v
docker compose up -d --wait
```

### Optional extras

```bash
docker compose --profile s3 up -d      # MinIO, for S3-backed artifacts
docker compose --profile debug up -d   # Temporal UI on http://localhost:8080
```

## Documentation

- [Local development](docs/development.md) — running openplan from source
- [Architecture and product model](docs/architecture.md)
- [Authentication and authorization](docs/authentication.md)
