# Authentication and Authorization

This document defines how openplan authenticates against an OIDC identity
provider and the OpenFGA model used for per-stack authorization. The broader
trust model and authorization invariants remain in the
[authentication and authorization security architecture](superpowers/specs/2026-07-14-authn-authz-security-architecture-design.md).

## Identity Provider

openplan works with any compliant OIDC provider named by `OIDC_ISSUER_URL`, and
the provider is the only way in: openplan holds no passwords and checks no
credential of its own. It provisions nothing on the provider either; a
deployment registers one confidential client itself.

The client's one redirect URI is derived, never configured separately:

```text
Redirect URIs:
  <OPENPLAN_PUBLIC_URL>/v1/auth/callback
```

Allowed web origins stay empty: the browser only ever calls the API's own
origin, so no CORS configuration exists anywhere in `internal/api`.

### Dex on the local stack

Compose runs [Dex](https://dexidp.io) v2.45.1 at `http://dex.localhost:5556/dex`,
configured by `deploy/dex/config.yaml` and stored in its own `dex` database on
the shared Postgres server. It registers the `openplan-api` client with the
same `OIDC_CLIENT_SECRET` the API reads, and one static password user,
`admin@openplan.local` / `admin-local-only`, who is root (see "Root" below).
Local users are Dex's password DB; add more to `staticPasswords` in the config,
or through Dex's gRPC API, which takes a bcrypt hash and never plaintext.

`dex.localhost` resolves to loopback in the browser and to the Dex container
through its Compose network alias, so one issuer string satisfies both — the
API compares the discovery document's issuer to `OIDC_ISSUER_URL` byte for
byte. `curl` resolves `*.localhost` to its own loopback and ignores the alias,
so test from inside a container with `wget` instead.

Dex v2.45.1 advertises no `end_session_endpoint` and sends no back-channel
logout, so logout ends the openplan session and returns home. That is a
complete logout on this stack: v2.45.1 keeps no browser session of its own
(browser sessions exist only on Dex's master branch, behind
`DEX_SESSIONS_ENABLED`), so the next sign-in asks for the password again. An
upstream connector such as GitHub keeps its own session, and sign-in through
one can be silent. Dex's `sub` is `base64url(protobuf{user_id, connector_id})`,
stable for a given user and connector.

## OIDC Client and Claims

There is one client. The API is the only OIDC client and the only party that
ever talks to the provider: `/v1/auth/login`, `/v1/auth/callback`, and
`/v1/auth/logout` on the API run the entire authorization-code exchange
server-side with PKCE S256, holding the client secret, and the browser receives
nothing but an httpOnly session cookie (see "Browser Session" below).

The API verifies **ID tokens**, not access tokens, and checks `aud` against
`OIDC_CLIENT_ID`. An ID token's `aud` is the client ID by construction, so the
audience is already correct and no provider-side mapper or custom
authorization server is needed to mint it.

## Global Roles

Global roles are relationships on the OpenFGA `platform` singleton, not IdP
claims. Nothing in an ID token grants authority; see "OpenFGA Stack
Authorization" below.

### Root

A fresh install has no administrator, and granting admin requires already
being one. The API breaks that loop at every boot: before it serves, it writes
the `{user:<OPENPLAN_ROOT_SUBJECT>, root, platform:openplan}` tuple if it is
absent, and refuses to start if it cannot.

Root is an identity-provider user plus that tuple, nothing more. openplan holds
no credential for it; whoever the provider signs in with that `sub` is root.
It is keyed on `sub`, never on email: an upstream connector can assert any
address, whereas Dex's `sub` includes the connector, so a GitHub user who
shares an address with the static entry cannot claim root. On the local stack
the value is the sub of `admin@openplan.local`, and
`scripts/verify-auth-compose.mjs` derives it from `deploy/dex/config.yaml` so
the two cannot drift.

Seeding is add-only. Changing `OPENPLAN_ROOT_SUBJECT` adds a root and leaves the
previous one standing, and `root` is outside the grantable set, so the grant
API cannot remove it either. Recovering a lost root password is a change to the
provider's user, not to openplan's database.

The API does not check with the provider that the subject exists. It does not
talk to Dex's admin API, and a wrong subject shows up at once as a root who
gets `403` everywhere.

`OPENPLAN_ROOT_PASSWORD` and `OPENPLAN_ROOT_USERNAME` are retired along with the
local accounts they seeded. Setting either is a startup error rather than being
ignored: a root password left in an environment would be a secret that
protects nothing while looking as if it does.

## API Runtime Security Configuration

The API validates its authentication and authorization configuration before it
connects to Postgres or Temporal or starts its HTTP listener.

| Variable | Sensitive | Purpose |
|---|---:|---|
| `OPENPLAN_ENVIRONMENT` | No | Optional runtime mode; empty defaults to `development`; valid values are `development` and `production` |
| `OPENPLAN_TENANT_ID` | No | Required single configured tenant identifier |
| `VITE_OPENPLAN_TENANT_ID` | No | Frontend build-time tenant context; must exactly match `OPENPLAN_TENANT_ID`; local development falls back to `tenant_123` |
| `OIDC_ISSUER_URL` | No | Required exact OIDC issuer URL; any compliant provider |
| `OIDC_CLIENT_ID` | No | Required OAuth client ID; also the ID token audience the verifier checks against |
| `OIDC_CLIENT_SECRET` | Yes | Required; the API is a confidential client and authenticates as one when it exchanges a code |
| `OPENPLAN_PUBLIC_URL` | No | Required; the origin the browser reaches. The API derives its own OIDC redirect URI (`<OPENPLAN_PUBLIC_URL>/v1/auth/callback`) and post-logout redirect URI from it — never from `Host` or `X-Forwarded-Proto`, which an attacker can set |
| `SESSION_ENCRYPTION_KEY` | Yes | Required 32-byte key (raw, base64, or hex) that seals the short-lived login transaction cookie (`state`, `nonce`, PKCE verifier, `return_to`) and encrypts each session row's stored ID token at rest |
| `OPENPLAN_SESSION_ABSOLUTE_TTL` | No | Optional hard cap on a session from sign-in, never extended; defaults to `8h` |
| `OPENPLAN_SESSION_IDLE_TTL` | No | Optional idle bound, sliding on activity; defaults to `1h`; must not exceed `OPENPLAN_SESSION_ABSOLUTE_TTL` |
| `OPENPLAN_ROOT_SUBJECT` | No | Required OIDC `sub` of the bootstrap administrator; see "Root" above |
| `OPENFGA_STORE_NAME` | No | Optional name of the store the API adopts; defaults to `openplan` |

`OPENPLAN_TENANT_ID` is the authoritative security boundary. Every authenticated
tenant-scoped route compares its `{tenant_id}` path value with that configured
tenant before decoding a body or accessing application services, repositories,
logs, artifacts, or authorization data. Missing, malformed, and mismatched
tenant paths return `404` without disclosing whether a referenced resource
exists.

The React application reads `VITE_OPENPLAN_TENANT_ID` as non-editable build-time
context. Deployments must set it to the same value as `OPENPLAN_TENANT_ID`; a
mismatch is safe but prevents tenant-scoped requests from succeeding. Changing
this value requires rebuilding and redeploying the frontend bundle.

Development permits the documented loopback HTTP issuer. Production must be
selected explicitly with `OPENPLAN_ENVIRONMENT=production`; it requires HTTPS for
external dependencies. Unknown modes, malformed tenant IDs, and unsafe URLs or
identifiers stop startup.

There is nothing to record for OpenFGA. It runs inside the API process, creates
its tables in the application database, and resolves the store and the
authorization model from the model in this repository at startup — adopting an
existing store and model when they match, and refusing to start when more than
one matches, because picking one would silently decide which tuples count.

`OIDC_AUDIENCE` is retired: it named a resource identifier that used to be
forced into an access token's `aud`, and that concept does not exist for the
ID token the API verifies now. Setting it is a hard startup error rather than
a silently ignored or aliased variable — this codebase carries no
compatibility shims, and a config file that still names the old variable
should fail loudly rather than start with a meaning it no longer has. Set
`OIDC_CLIENT_ID` instead.

## API Token Verification

The API accepts compact **ID tokens** — not access tokens, and not opaque
tokens — for the configured issuer and `OIDC_CLIENT_ID`. Signature, issuer,
audience, expiry (with a small acceptable clock skew), and subject checks
complete before identity data is returned. During the login callback the
handler additionally compares the token's `nonce` claim against the one it
sealed into the transaction cookie; PKCE plus a confidential client already
closes code injection, so this is defence in depth rather than the
load-bearing control.

Provider discovery and JWKS signing keys are cached. A new or replaced signing
key triggers one bounded refresh, so routine key rotation does not require an
API restart. A fresh cached key continues to work through a short provider
outage. If the verifier cannot fetch required public keys, it fails closed and
exposes no token or provider-response detail.

AUTH-007 middleware owns HTTP status mapping and credential parsing.

## API Request Authentication

The `openplan_session` cookie is the only credential `/v1` accepts. The
middleware hashes it, looks up the session row, and checks `Session.IsLive`
against openplan's own bounds — see "Browser Session" below. The IdP is not
consulted on this path at all; the ID token behind the session was verified
once, at the callback, and its claims copied onto the row there.

An `Authorization` header authenticates nothing. openplan previously accepted an
`Authorization: Bearer <id-token>` header for "a CLI or service-to-service
caller," and that was removed because:

- **Nothing used it.** There is no CLI in this repository; the executor reaches
  only Temporal and the artifact store and never calls the API; and the web
  client sends no `Authorization` header — it relies on the cookie.
- **A CLI could not have used it.** The flow is server-side, so no endpoint
  hands a token to a caller, and the token lives 300 seconds with no refresh
  token stored. A CLI would have had to re-run the whole browser flow every
  five minutes.
- **It made the ID token a key.** RP-initiated logout necessarily puts that
  token in a URL, where it reaches browser history and every access log in
  between. While the Bearer path existed, a copy read out of one of those
  worked against every `/v1` route.
- **A signed token cannot be revoked.** Logout, back-channel logout, and an
  admin disabling an account all mark a session row. None of them can reach a
  JWT somebody already holds.

A non-browser caller therefore wants a credential openplan issues and can revoke
— personal access tokens, the device authorization flow, or service accounts —
not a re-used ID token. None of those exist yet.

`/healthz` and the four `/v1/auth/*` routes below remain public. A missing,
unknown, expired, or revoked cookie receives `401` with the stable JSON body
`{"code":"unauthorized"}`; tokens, claims, and verifier details are never
written to logs or responses.

After authentication, the request context contains an `authn.Principal` with
the immutable subject and safe display claims — `Name`, `PreferredUsername`,
`Email`, and `ExpiresAt` (openplan's own idle/absolute bound). It carries no role
claim: OpenFGA is the sole authorization source, so nothing from the token
feeds an access decision. Handlers and application services obtain it with
`authn.PrincipalFromContext` rather than parsing HTTP headers or tokens.

## Browser Session

The browser never speaks OIDC. Three API routes run the entire
authorization-code flow on its behalf, plus one more the IdP calls directly:

| Route | Purpose |
|---|---|
| `GET /v1/auth/login` | Starts the OIDC flow: generates `state`, `nonce`, and a PKCE verifier, seals them into the transaction cookie, and redirects to the IdP |
| `GET /v1/auth/callback` | Redeems the code on the back channel, verifies the resulting ID token, creates a session row, and hands the browser the session cookie |
| `POST /v1/auth/logout` | Revokes the session row, clears the session cookie, and redirects to the IdP's RP-initiated logout where there is one |
| `POST /v1/auth/backchannel-logout` | Unauthenticated; ends sessions on the IdP's own instruction — see "Back-Channel Logout" below |

The sign-in screen itself is a client route, `/signin`, not a server-rendered
page. It is one "Sign in" button that navigates to `GET /v1/auth/login`; every
credential prompt is the provider's. It is a button rather than an automatic
redirect so that a lost session lands on a page the user chooses to leave,
instead of spending a trip to the provider on every bounce.

`GET /v1/auth/login` takes a `return_to` query parameter, the one place the flow
accepts untrusted input, and `authn.SafeReturnTo` reduces it to a same-origin
path or `/`. Absolute, protocol-relative (`//host`, `/\host`),
userinfo-bearing, control-character, and unnormalized paths are all refused. So
is anything under `/v1`: no API path is a place for a browser to land, and one
of them is a trap — `return_to=/v1/auth/login` would make the callback restart
the login it has just finished, and with an SSO session standing the browser
loops until it gives up. The SPA's own loop guard cannot catch that, because
server-side redirects never load a page for it to count. `/signin` carries the
same parameter and applies the same rule to it in the client before handing
it on, so neither side has to trust the other to have done it.

Two cookies carry the interactive flow:

| | `openplan_session` | `openplan_auth_tx` |
|---|---|---|
| Contents | an opaque 43-character reference to a session row — not a token | sealed `{state, nonce, code_verifier, return_to}` |
| Path | `/` | `/v1/auth` |
| Max-Age | none (session cookie) | 600s |
| `HttpOnly` | yes | yes |
| `SameSite` | `Lax` | `Lax` |
| `Secure` | production only | production only |

Both are `SameSite=Lax`, not `Strict`. The IdP's callback to
`/v1/auth/callback` is a cross-site top-level GET — the browser is navigating
back from the IdP's origin, not from openplan's own origin — and `Strict`
would withhold the transaction cookie on exactly that request, breaking every
login. It would look like a random state-mismatch failure rather than an
obviously misconfigured cookie.

`Lax` still stops CSRF on every mutating route in this API, because `Lax`
withholds the cookie from a cross-site request unless it is a top-level GET
navigation. A forged cross-site form or script can `POST`, `PATCH`, or
`DELETE` all it wants; the browser will not attach `openplan_session` to any of
it, so the request arrives unauthenticated. This holds only because every
mutating route in the API is `POST`, `PATCH`, or `DELETE` — a mutating `GET`
would defeat it, so there is not one, and there is no separate CSRF token.

`openplan_session` needs no encryption: it carries no claims to protect, only 32
bytes of CSPRNG output rendered as base64. The database never stores that
value, only its SHA-256 hash (`id_hash`), so a leaked row of the `sessions`
table yields no usable cookie, and a tampered cookie value simply hashes to no
row — indistinguishable from an expired session. The transaction cookie
**is** sealed with AEAD (`SESSION_ENCRYPTION_KEY`), because `state` is only
meaningful if the browser cannot forge it — an attacker who can set a cookie
can set a query parameter too, and unsealed state would let login-CSRF back
in.

### Session Lifetime

A session is openplan's own record, not the IdP's. Before this design the
session cookie held the raw ID token, so how long a sign-in lasted was decided
by the provider's token lifespan and whether silent renewal was governed by
its SSO idle timeout. openplan is BYO-IdP and configures neither on a
deployment's provider, so a row in the `sessions` table
(`internal/postgres/migrations/0018_sessions.sql`) is a session openplan issues,
expires, and revokes on its own terms. `internal/authn.Session` is the Go
type; `internal/authn.SessionStore` is the persistence interface the cookie
path of `RequireAuthentication` depends on.

`handleAuthCallback` copies the verified ID token's claims onto the row once,
at sign-in — subject, name, preferred username, email, and the `sid` claim
when the provider sends one. Every later request authenticates against that
row; the ID token is never re-verified or re-read after the callback. That is
the whole point: session length becomes openplan's to choose instead of a
consequence of whatever access-token lifespan or SSO idle timeout a customer's
IdP happens to run.

Two independent bounds decide whether a session is live (`Session.IsLive`):

| Bound | Config | Default | Behavior |
|---|---|---|---|
| Absolute | `OPENPLAN_SESSION_ABSOLUTE_TTL` | `8h` | Set once at sign-in and never extended — a hard cap from `CreatedAt`, not slid by activity |
| Idle | `OPENPLAN_SESSION_IDLE_TTL` | `1h` | Slides on activity, but the row is written back at most once every 5 minutes (`SessionTouchInterval`), not on every request |

The session's effective expiry is the earlier of the two (`Session.ExpiresAt`),
which is what `/v1/me` reports as `sessionExpiresAt` so the SPA can
re-authenticate proactively rather than being surprised by a `401` mid-action.
That proactive path is deliberately one-sided: asking `/v1/me` again is itself
a request and would slide the very bound it is checking, so the SPA only
re-checks when it has made some *other* request since the snapshot. A tab
nobody is using makes none, so it makes no noise at its expiry either and the
idle bound is reached — the alternative is a browser that renews itself once an
hour forever and an idle bound that can never fire.
`OPENPLAN_SESSION_IDLE_TTL` must not exceed `OPENPLAN_SESSION_ABSOLUTE_TTL` — the
API refuses to start otherwise, since an unreachable idle bound is a
configuration mistake, not a permissive setting. Revocation (`RevokedAt`) is
checked first in `IsLive` and is unconditional: a revoked session is dead
regardless of either bound, which is what lets back-channel logout end a
session immediately instead of waiting on a TTL.

Because claims are copied once, an IdP-side change — a renamed user, a
disabled account, a role change — is not observed by openplan until the session
ends. Without back-channel logout that staleness window is bounded by the 8h
absolute cap; with it, the window closes as soon as the notification arrives
(see below). That trade is deliberate: session length a BYO-IdP deployment
never has to negotiate with its provider, at the cost of display claims that
are a snapshot rather than live.

The row also keeps the raw ID token, encrypted at rest, solely so
`handleAuthLogout` can pass it to the IdP as `id_token_hint` during
RP-initiated logout — without it, a provider may show a logout confirmation page
instead of signing out silently. Despite that parameter's name it is the whole
token, not a reference to one. It is encrypted with
`SESSION_ENCRYPTION_KEY` — the same required key that seals the transaction
cookie above, through a separate cipher instance
(`encryptSession`/`decryptSession` in `internal/postgres/store.go`) so the two
uses stay independently testable.

Expired rows are swept out of the table, not left in it. Revoking marks a row
rather than deleting it and expiry is a comparison made at read time, so
nothing else would ever remove one — and every row holds an encrypted ID
token. `authn.ReapSessions` runs as a goroutine alongside the API server
(`cmd/api/main.go`) and every 15 minutes deletes rows whose absolute bound has
passed, the query the `sessions_absolute_expires_at_idx` index exists for. It
sweeps on the absolute bound alone: a row past it is dead whatever its idle
bound says, and the absolute TTL is therefore the cap on how long any dead row
can linger. Several API replicas sweeping at once is harmless — a `DELETE`
over a closed range is idempotent — so there is no leader election.

Rotating `SESSION_ENCRYPTION_KEY` therefore invalidates every stored session,
not only an in-flight login: `SessionByHash` decrypts the stored ID token on
every lookup, so a session created under the old key fails that decrypt and
is treated as unauthenticated, the same as if it had been revoked.

There is still no refresh token — that part of the design is unchanged.
Storing and rotating one was evaluated and rejected: correct handling needs a
transactional store with row locking to survive concurrent requests racing a
single-use refresh token, the new cookie has nowhere reliable to ride out on a
streaming log response, and `offline_access` means three different things
across providers such as Okta and Google. The full reasoning, including the ArgoCD
comparison that shaped it, is in the [design
doc](superpowers/specs/2026-08-25-oidc-server-side-flow-design.md). What has
changed is what "expired" means: it is no longer the IdP's ID token `exp` but
openplan's own idle and absolute bounds.

### What ends a session, and what does not

An expiring IdP session does **not** end a openplan session. Providers
typically expire their own sessions without notifying anyone, so back-channel
logout fires only on explicit events — a logout, or an admin disabling a
user. This is the independence the app-owned session was built for.

Re-authentication is **not** silent, though. openplan never contacts the IdP
after the callback — no refresh, no userinfo call — so the IdP's SSO idle
timer starts at sign-in and is never refreshed. With a typical 30-minute SSO
idle timeout it is dead long before openplan's own session ends. When a openplan session does expire, the trip
through `/v1/auth/login` therefore finds no SSO session to pick up and the user
gets a full credential prompt.

That is the safer of the two possible behaviours, and it is worth being
deliberate about: silent re-authentication is the IdP renewing someone without
asking, which is exactly what would nullify `OPENPLAN_SESSION_IDLE_TTL`. An
abandoned browser whose openplan session idled out would simply be resumed.

The BYO-IdP consequence follows: **openplan's idle bound is only enforceable if
the deployment's IdP idles out at least as fast.** A provider configured with a
long SSO idle timeout makes `OPENPLAN_SESSION_IDLE_TTL` advisory, because the
redirect that follows it will be answered silently.

### Back-Channel Logout

`POST /v1/auth/backchannel-logout` is unauthenticated by necessity: it is
called by the IdP's own server, which holds no openplan cookie and no bearer
token. The credential is the logout token itself (OIDC Back-Channel Logout
1.0), verified against the same JWKS and issuer that verify ID tokens
(`OIDCVerifier.VerifyLogoutToken`). openplan checks signature, issuer, audience,
`iat` freshness (2-minute maximum age), the required
`http://schemas.openid.net/event/backchannel-logout` event, and rejects any
token carrying `nonce` — its presence would mean an ID token is being replayed
as a logout token, which would let anyone holding one revoke another user's
sessions.

A logout token identifies what to revoke by `sid` or `sub`, and openplan prefers
the narrower one: a `sid` match revokes one browser session
(`RevokeSessionsByIDPSessionID`); a `sub`-only match revokes every session for
that user (`RevokeSessionsBySubject`). The endpoint returns `200` whether or
not anything matched — whether openplan holds a session for a given `sid` is not
something an unauthenticated caller gets to learn.

A BYO-IdP deployment needs **no** session or timeout configuration on its
provider; the 8h/1h bounds above are entirely openplan's own. To get immediate
revocation instead of waiting on those bounds, point the provider's
back-channel logout at the API's `/v1/auth/backchannel-logout` endpoint —
**reachable from the identity provider**, not from the browser. Those are
frequently different addresses: the callback and post-logout redirect URIs
are resolved by the browser, so `OPENPLAN_PUBLIC_URL` (e.g.
`http://localhost:5173` on the reference stack) is correct for them, but a
back-channel logout is a server-to-server POST from the IdP's own process —
if the IdP runs in its own container or network, `OPENPLAN_PUBLIC_URL` names
nothing it can reach, and the notification silently never arrives.

`OPENPLAN_PUBLIC_URL` is not that address. The back-channel logout URL is
registered on the identity provider, not read by openplan: the API accepts
the POST wherever it arrives. An IdP on the local Compose network would
register `http://api:8081/v1/auth/backchannel-logout` — the API's address
there — rather than `http://localhost:5173`, which inside the IdP's own
container means its own loopback. Dex v2.45.1 sends no back-channel logout,
so the reference stack registers nothing.

Also enable session-required logout so the provider includes `sid` in both the
ID token and the logout token — without it, openplan can only match on `sub`,
so signing one device out signs out every session the user has.

Matching prefers `sid` over `sub`, so a provider that signs one device out does
not sign the user out everywhere. When the `sid` matches no row the handler
falls back to `sub` in the same token: a provider may put `sid` in the logout
token but not in the ID token the session was built from, leaving
`idp_session_id` empty on every row, and the narrow key would then silently
revoke nothing at all. Either way the endpoint answers `200` — whether openplan
holds a session for a given `sid` is not something an unauthenticated caller
gets to learn.

A provider that never calls this endpoint is not a broken deployment: sessions
still end at their own absolute and idle bounds, exactly as if back-channel
logout did not exist. The endpoint only closes the gap between "the IdP
considers this session over" and "openplan does too."

Logout redirects rather than returning the IdP's logout URL in a JSON body.
That URL carries the raw ID token as `id_token_hint`, and there is no reason to
hand that to script on the origin. A `Location` header on a `303` is not
script-readable. The route is `POST`, not `GET`, so a cross-site image tag
cannot trigger it — though `SameSite=Lax` would already make such a request
arrive without the session cookie regardless.

The token does still travel in a URL the browser navigates to, so it reaches
browser history, the IdP's access log, and any proxy in between. That is normal
and is what the spec assumes: an ID token is an identity assertion issued to
one client, not a credential. It is only true here because `/v1` no longer
accepts bearer tokens — while it did, a copy read out of any of those logs was
a working API credential that survived the very logout that wrote it there.

The ID token's lifespan is the provider's and is hygiene rather than a control
now. RP-Initiated Logout 1.0 says the OP **SHOULD** honour an expired
`id_token_hint` (conditioned on the RP having a current or recent session at
the OP, and a `sid` matching neither MAY be declined as suspect), and a session
running up to eight hours means ours is normally expired at logout anyway.

## Identity Projection

Grant display names and the user-search box read a local `users` table, not the
IdP.

There is nothing standard to read from an OIDC provider here. `/userinfo` only
ever describes the bearer of the token presented, so it cannot look up a third
user, and cross-user lookup is vendor-specific admin API territory — Okta's
Users API, Microsoft Graph, Google's Admin SDK. Each needs elevated permissions
a customer's security team must approve, and each would put that provider on
the critical path for rendering a grants list. openplan previously did exactly
this against one provider's admin API. That is gone.

Instead, every ID token openplan verifies already carries what the UI needs, and
the callback writes it down:

| Column | Source |
|---|---|
| `sub` | the `sub` claim — the only stable key, and the same string that becomes `user:<sub>` in an OpenFGA tuple |
| `display_name` | the first non-empty of `name`, `preferred_username`, `email`, `sub` |
| `email` | the `email` claim, which may be empty |
| `first_seen_at` | set on the first sign-in and never updated |
| `last_seen_at` | moved on every sign-in |

The write happens in the OIDC callback, before the session row is created, and
a failure there fails the sign-in. Ordering it first is what makes the useful
statement true: **a session row exists only for a subject that is in the
projection.**

This is a projection, not a directory. openplan is not the source of truth for
identity and performs no account lifecycle: nothing creates, disables, or
deletes a row except a sign-in. A changed name or email is picked up the next
time that person signs in.

### Accepted limitation: a user must sign in before they can be granted a role

A person who has never signed in is not in the projection, does not appear in
the search box, and cannot be granted a role — `POST .../grants` answers
`400 unknown_user`. They sign in once, which lands them in the projection, and
are grantable from then on.

This is deliberate, and it has an ordering consequence worth knowing before you
hit it: to give a colleague access, they must sign in first, and only then can
you grant them a role. The same applies to bootstrapping the first
administrator.

There is no pending-grant mechanism and no email-to-`sub` reconciliation, both
of which would mean inventing an identity openplan has not been told about. SCIM
is the answer if pre-provisioning is ever genuinely required, and is
out of scope here and in the security architecture.

## OpenFGA Stack Authorization

The model defines `user` and `stack` types. Only the four direct stack roles can
be assigned to a user; the permission relations are derived and cannot be
assigned directly.

### Role and Permission Matrix

| Direct stack role | `can_view` | `can_operate` | `can_approve` | `can_manage_access` | Meaning |
|---|---:|---:|---:|---:|---|
| `owner` | Allowed | Allowed | Allowed | Allowed | View, operate, approve, and manage access |
| `operator` | Allowed | Allowed | Denied | Denied | View and operate |
| `approver` | Allowed | Denied | Allowed | Denied | View and approve |
| `viewer` | Allowed | Denied | Denied | Denied | View only |

The derived relations are exactly:

- `can_view = owner or operator or approver or viewer`
- `can_operate = owner or operator`
- `can_approve = owner or approver`
- `can_manage_access = owner`

`operator` always means the per-stack OpenFGA role in this repository. The
person or pipeline that configures and deploys the services is the deployment
administrator.

The model is authored in OpenFGA's DSL at
`internal/authorization/authorization-model.fga`, which is what changes to
permissions are written and reviewed in and the only form of the model in the
repository. The API embeds that file and transforms it to the wire format in
process at startup; see
[local development](development.md#the-authorization-model).

### Runtime Authorization Behavior

- Stack creation requires `can_create_stack` on the platform singleton. The
  stack row, the audit event, the `owner` relationship for the creator's
  immutable subject, and the `parent` edge that lets platform administrators
  reach the stack are all written in **one Postgres transaction**. A stack that
  exists is therefore always one its creator can reach, and a failed grant
  leaves no stack behind. Creation is synchronous: the response describes a
  usable stack, and there is nothing to poll.
- Role changes are the same shape. The current grants are read, the last-owner
  guard is applied, and the converging writes are made — all inside the
  transaction that records the audit event. Reading inside it is what closes
  the window in which two concurrent demotions could each see two owners, each
  pass the guard, and leave the stack with none.
- The authorization queue kinds are retired. `grant_stack_owner`,
  `mark_stack_ready` and `reconcile_stack_grant` existed only because a tuple
  write could not commit with the domain write that caused it.
- Higher-consistency confirmation is gone with them. It existed because a
  rejected remote write might still have landed; inside a transaction the write
  lands or the caller rolls back, and there is no third outcome.
- A denial is an answer and reaches the caller as `403` (or `404` where a route
  refuses to disclose existence). Anything else — a database failure, a timeout,
  a malformed request this code built — is an error, never a decision, and
  reaches the caller as `500`. A failure can never be rendered as a refusal:
  callers reach `ErrForbidden` only when the check returned `(false, nil)`.

### Endpoint Permission Matrix

The API enforces permissions in the application layer. Frontend button
visibility is never an authorization boundary.

| Route | Required permission |
|---|---|
| `POST /v1/tenants/{tenant_id}/template-revisions` | Global `platform-admin` or `stack-creator` |
| `GET /v1/tenants/{tenant_id}/template-revisions` | Global `platform-admin` or `stack-creator` |
| `GET /v1/tenants/{tenant_id}/template-registrations/{registration_id}` | Global `platform-admin` or `stack-creator` |
| `GET /v1/tenants/{tenant_id}/template-revisions/{template_revision_id}/variables` | Global `platform-admin` or `stack-creator` |
| `POST /v1/tenants/{tenant_id}/stacks` | Global `platform-admin` or `stack-creator` |
| `GET /v1/tenants/{tenant_id}/stacks` | Complete tenant stack scan with bounded OpenFGA `BatchCheck(can_view)` calls; `platform-admin` bypasses the ordinary stack list check |
| `GET /v1/tenants/{tenant_id}/stacks/{stack_id}` | `can_view` |
| `POST /v1/tenants/{tenant_id}/stacks/{stack_id}/templates` | `can_operate` |
| `PATCH /v1/tenants/{tenant_id}/stack-templates/{stack_template_id}/config` | Owning stack `can_operate` |
| `POST /v1/tenants/{tenant_id}/stack-templates/{stack_template_id}/upgrade` | Owning stack `can_operate` |
| `POST /v1/tenants/{tenant_id}/stack-templates/{stack_template_id}/runs` | Owning stack `can_operate` |
| `GET /v1/tenants/{tenant_id}/template-runs/{run_id}` | Owning stack `can_view` |
| `GET /v1/tenants/{tenant_id}/template-runs/{run_id}/logs` | Owning stack `can_view` |
| `GET /v1/tenants/{tenant_id}/template-runs/{run_id}/logs/{phase}` | Owning stack `can_view` |
| `POST /v1/tenants/{tenant_id}/template-runs/{run_id}/approval` | Owning stack `can_approve` |
| `POST /v1/tenants/{tenant_id}/template-runs/{run_id}/cancellation` | Owning stack `can_operate` |

`can_manage_access` has no current route; the access-management API will use it.
Stack templates, runs, logs, log artifacts, and credential identifiers returned
with a stack inherit the owning stack decision. Non-administrator stack lists
read tenant stacks from Postgres in stable `(created_at, id)` keyset pages of at
most 50 candidates and authorize each page through `BatchCheck(can_view)`. The
API returns only after every page succeeds. A timeout, a result count mismatch,
or a later-page failure discards all earlier results and returns `500`; a
partial list is never returned, because it would tell the caller they have
access to less than they do.

Missing and inaccessible inherited reads both return `404 not_found`. Missing
and inaccessible inherited mutation targets both return `403 forbidden`, so
stack-template and run identifiers cannot be enumerated by comparing statuses.
An authorization failure -- a database error, a timeout, or a missing runtime
authorization -- fails closed as `500` and never produces an allowed operation
or an unfiltered list. A denial stays distinct: it is `(false, nil)` at the
boundary and becomes `403` or `404`, so a failure can never be rendered as a
refusal.

### Store and Model Resolution

There is no provisioning step and no verification command. The API resolves both
identifiers itself at startup, by the same rules the retired provisioner used:

- a store whose name matches `OPENFGA_STORE_NAME` (default `openplan`) is adopted;
  one is created only when absent
- a stored model semantically equal to the repository model is adopted; a new
  immutable version is written only when none matches
- **more than one matching store, or more than one matching model, fails
  startup.** Picking one would silently decide which tuples count, so it refuses
  rather than choosing

Repeated starts converge rather than duplicating, and a restart adopts the same
store and model rather than minting a model id that existing tuples were never
written against. A model definition change writes a new immutable version on the
next start, with no configuration to update.

Because resolution is content-addressed by the model in this repository, nothing
has to be recorded between phases and nothing has to be pasted into an
environment file. OpenFGA's own tables live in the application database, created
at startup — which is also what lets a tuple write join the same transaction as
the domain write that caused it.
