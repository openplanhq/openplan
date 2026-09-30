# Dex adoption — design

Status: implemented, 2026-09-30. Phases 1–3 landed on `dex-adoption`.

## Goal

openplan's only authentication logic is "be an OIDC client of Dex". Every way a
person proves who they are — a password, GitHub, a corporate IdP — is Dex's
business. openplan keeps what it already owns: the server-side session, the
`users` projection, and every authorization answer in OpenFGA.

## Decisions

1. **Dex replaces Keycloak** as the one issuer behind `OIDC_ISSUER_URL`.
   Pinned to the latest release, **v2.45.1**.
2. **openplan's local accounts go.** `LocalAuthenticator`, the `local_accounts`
   table and `POST /v1/auth/login` are removed. Passwords live in Dex's own
   password DB (`enablePasswordDB`), not in openplan. There is one credential
   store, not two.
3. **Root is a Dex identity plus an OpenFGA tuple.** The bootstrap
   administrator is a Dex `staticPasswords` entry. openplan seeds only the
   `{user:<sub>, root, platform:openplan}` tuple for a configured subject.
   Recovery is editing Dex's config, not openplan's database.
4. **No back-channel logout yet.** Dex's back-channel logout (#4945) is on
   master only, behind `DEX_SESSIONS_ENABLED`. openplan keeps its
   `/v1/auth/backchannel-logout` handler; nothing calls it until Dex releases
   the feature and the client registers the URI.

## What this reverses, and why that is accepted

Two earlier decisions are undone deliberately:

- **#211 — "a POC, demo, or test needs no IdP."** After this change, signing in
  always requires Dex. The cost is small: Dex is one static Go binary with a
  YAML config, and it joins the default Compose stack instead of hiding behind
  the `auth` profile Keycloak needed.
- **#212 — root cannot be locked out by an IdP outage.** Root now depends on
  Dex being up. Accepted: Dex is part of the platform, not an external
  dependency, and its password DB is on the same Postgres server as openplan.
  An outage of Dex is treated like an outage of Postgres.

## Dex facts this design relies on (v2.45.1)

- **Subjects are opaque.** `sub` is `base64url(protobuf{user_id, conn_id})`
  (`server/oauth2.go`, `genSubject`). It is stable for a given connector and
  user ID, and uses only `[A-Za-z0-9_-]`, so it is a valid OpenFGA tuple token.
  It changes if a user's connector ID changes, so connector IDs are fixed once
  chosen.
- **No `end_session_endpoint` in discovery.** `Flow.EndSessionURL` already
  returns `""` for a provider without one (`internal/authn/flow.go:123`), so
  logout ends the openplan session and returns home. The Dex SSO cookie
  outlives it; the next sign-in may not re-prompt. Acceptable while openplan is
  Dex's only client.
- **Password API is hash-only.** The gRPC `CreatePassword` takes a bcrypt hash,
  never plaintext. Any future openplan user admin hashes before calling Dex.
- **Postgres storage is built in**, so Dex gets its own database on the shared
  Compose server, the way Keycloak and Temporal do.

## Phasing

1. **Remove Keycloak.** Nothing in the API depends on it: it reads only the
   generic `OIDC_*` settings, and the default stack already signs in with
   local accounts. This phase deletes the provisioner, the `internal/keycloak`
   package, the Compose `auth` profile, the Keycloak database, its env vars,
   and Keycloak-specific wording in comments and docs. Behaviour is unchanged.
2. **Add Dex** to the default stack and point `OIDC_*` at it.
3. **Remove local accounts** and move root to a Dex identity plus a tuple.

Each phase ships and passes CI on its own.

## Users projection

Unchanged. `users` (0019) is written at every sign-in by
`Service.RecordSignIn` (`internal/api/auth.go:156`) from the verified ID
token's `sub`, `email` and display claims, and serves grant display names and
the grant search box. It was built to be provider-agnostic, so it works the same
with Dex. Two consequences:

- The `sub` stored is Dex's opaque subject, and it is also the OpenFGA
  `user:<sub>`. The two stay consistent because both come from the same token.
- Rows and tuples keyed by `local_root` or by Keycloak subjects become orphans.
  openplan is pre-production: the database is reset (`docker compose down -v`),
  not migrated.

Dex must send the display claims: the client requests the `openid email
profile` scopes, which Dex maps to `email` and `name`. A user who has never
signed in still cannot be granted a role; that rule is unchanged.

## Changes

### Removed

- `cmd/keycloak-provisioner`, `internal/keycloak`, `Dockerfile.keycloak-provisioner`.
- `internal/authn`: `local_authenticator.go`, `local_account.go`, `password.go`
  and their tests.
- `internal/api/local_login.go` and `POST /v1/auth/login`.
  `GET /v1/auth/methods` goes too: there is one way in, so the sign-in screen
  is a single "Sign in" button that goes to `GET /v1/auth/login`.
- `internal/postgres/local_accounts.go`. A new migration drops
  `local_accounts` (0020 stays; migrations are append-only).
- `OPENPLAN_ROOT_USERNAME`, `OPENPLAN_ROOT_PASSWORD`, every `KEYCLOAK_*`
  variable, and the Keycloak database in `deploy/postgres/init.sh`.
- Web: the password form, `loginAttempts.ts`, and the local-login client call.

### Changed

- **OIDC config becomes mandatory again.** `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`
  and `OIDC_CLIENT_SECRET` are required; `Flow` is never nil, so the auth
  routes are always registered.
- **`bootstrap.SeedRoot` seeds only the tuple.** New required
  `OPENPLAN_ROOT_SUBJECT` is the OIDC `sub` of the root identity. Keyed by
  `sub`, never by email: an upstream connector can assert any email, and the
  `sub` includes the connector, so a GitHub user cannot claim root by sharing
  an address with the static entry. Add-only, as today.
- **Compose**: a `dex` service (v2.45.1) in the default stack, on
  `http://dex.localhost:5556/dex`, with Postgres storage, one static client
  `openplan-api` whose redirect URI is `<OPENPLAN_PUBLIC_URL>/v1/auth/callback`,
  and one static password entry for root. The config lives in
  `deploy/dex/config.yaml`; secrets come from the environment.
  `.env.example` gets the matching `OIDC_*` block and `OPENPLAN_ROOT_SUBJECT`,
  precomputed for the static root's `userID` and the `local` connector.

### Unchanged

The session model, the middleware, the callback's ID-token verification, the
`users` projection, OpenFGA, and the back-channel logout handler.

## Out of scope

- A user admin UI calling Dex's gRPC API. Until it exists, operators add users
  through Dex config or `dexctl`-style tooling.
- Upstream connectors (GitHub, SAML). Adding one is Dex config only.
- Back-channel logout; revisit when a Dex release contains #4945.

## Open questions

- ~~Should a missing root tuple for `OPENPLAN_ROOT_SUBJECT` be checked against
  Dex at boot?~~ Settled: no. The API does not talk to Dex's admin API, and a
  wrong subject is visible immediately as a root who gets 403. The local
  stack's value is instead derived from `deploy/dex/config.yaml` by
  `scripts/verify-auth-compose.mjs`, so the two cannot drift there.
- Settled during implementation: `OPENPLAN_ROOT_PASSWORD` and
  `OPENPLAN_ROOT_USERNAME` are refused at startup rather than ignored, and
  migration 0030 drops `local_accounts`.
