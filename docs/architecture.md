# openplan Architecture

The authentication and authorization trust boundaries, identity propagation,
role semantics, failure contracts, and threat model are defined in the
[Authentication and Authorization Security Architecture](security-architecture.md).

This document describes openplan's current MVP product model, system architecture, execution workflows, persistence boundaries, security posture, and deferred design topics. For setup and local development, see the [project README](../README.md).

## Goals

- Let users register Terraform templates from GitHub repositories.
- Let users create stacks and add templates to those stacks.
- Let users configure non-sensitive Terraform variables from the UI.
- Run Terraform `plan`, `apply`, and `destroy` from the UI.
- Use a Go workflow engine to orchestrate long-running operations.
- Start every Terraform run in a fresh empty working directory.
- Persist product metadata, activity history, run state, logs, and artifacts.
- Use a stable Terraform workspace per installed stack template.
- Keep the MVP fast by running Terraform in executor activity processes, not Kubernetes Jobs.

## Non-Goals For MVP

- Managed Terraform backend provisioning.
- Automatic data sharing between templates in a stack.
- Secret Terraform variable collection in the UI.
- Full RBAC or approval policy engine.
- Multi-provider Git integrations beyond GitHub.
- Kubernetes Job based Terraform execution.
- Persisting binary Terraform plan files for exact approved-plan apply.

## Architecture

The MVP uses Temporal OSS as the durable workflow engine, Postgres as the application product database, S3-compatible object storage for logs/artifacts, Valkey to carry a running command's output to the UI, and Vault or another secret store for execution credentials. Temporal also has its own persistence database for workflow history, timers, task queues, signals, and retry state.

The architecture also defines a pluggable `EventBus` interface for system events and future pub/sub needs, which can start as a no-op and later swap in Redis, NATS JetStream, Kafka, or another broker without changing API or workflow semantics. Live log output does not go through it: Valkey carries it between processes (see Logs).

The system is split into a control plane and a data plane. The API process is the control plane: it serves HTTP, owns the database and every key, runs the queue loop that turns committed intents into workflow starts and signals, and polls Temporal's `control` task queue for workflow tasks and the activities that write product state. The executor process is the data plane: it polls only the `execution` task queue and runs workspace preparation, source checkout, and Terraform. It holds no database URL and no key. Neither process calls the other; every exchange goes through Temporal, except a running command's output, which the executor appends to Valkey for the API to read (see Logs). [Apply run sequence](apply-run-sequence.md) walks an apply through every round trip.

```text
UI --> openplan-api  (control plane)                      Temporal Server
         HTTP                                                 ^       ^
         queue loop ---------- start workflow / signal -------+       |
         worker on "control" -- poll / respond ---------------+       |
           workflows, status and log writes,                          |
           template sync, credential sealing                          |
              |                                                       | poll / respond
              v                                                       | "execution"
         App Postgres                                                 |
           product state, work_queue                    openplan-executor  (data plane)
                                                          PrepareWorkspace (run key)
                                                          FetchSource, RunTerraform --> tofu
                                                          no database, no keys
                                                                      |
                                                                      v
                                              artifact store, Valkey, cloud APIs, state backends
```

## Core Boundaries

The platform has three important boundaries:

```text
Stack         = composition boundary
StackTemplate = Terraform state boundary
TemplateRun   = execution boundary
```

A `Stack` groups templates and stack-level metadata. A `StackTemplate` represents one template installed into one stack and owns a stable Terraform workspace. A `TemplateRun` represents one `plan`, `apply`, or `destroy` execution against one `StackTemplate`.

`StackRun` exists only for coordinated multi-template operations. The main MVP use case is destroying a whole stack in a controlled order.

## Major Components

### UI

The UI supports:

- Connecting a tenant to GitHub.
- Registering templates from GitHub repository, ref, and path.
- Creating stacks.
- Adding tags to stacks as key-value pairs.
- Adding templates to stacks.
- Entering non-sensitive template input values.
- Triggering `plan`, `apply`, and `destroy`.
- Viewing run lifecycle, activity events, and live logs.
- Pressing a dummy approval button for apply operations.

### API Server

The API server is responsible for:

- Validating requests.
- Using mock user identity for MVP.
- Creating and reading product records in Postgres.
- Atomically creating queued template runs and their Postgres workflow-outbox entries.
- Starting template-sync workflows directly in Temporal.
- Starting the workflows that plan and apply runs.
- Serving UI-facing run, stack, template, and log endpoints.
- Exposing live log streams to the UI.

The API does not execute Terraform and does not communicate directly with the executor; both talk only to Temporal.

### Temporal

Temporal is the execution coordinator. It owns workflow durability, task queues, retries, timers, cancellation, and approval waits.

Work is split across two task queues, named by constants in `internal/domain`:

```text
control     workflow tasks and control activities; polled by the API
execution   workspace, source, and Terraform activities; polled by executors
```

Operation type is represented in run metadata:

```text
plan | apply | destroy
```

Every executor replica enables Temporal Session Workers. A `TemplateRun` creates
one session for its filesystem-dependent work, so workspace preparation, source
checkout, and Terraform activities stay on the same executor replica. The session
also remains owned by that replica while an apply or destroy workflow waits for
approval before continuing. The session has a 1-minute creation timeout and a
24-hour execution timeout; the execution timeout bounds the entire session,
including any approval wait. Status activities can run independently. Run
metadata and status are persisted in Postgres, while completed logs and
artifacts are uploaded to the configured S3-compatible artifact store. The
live workspace and phase-log spool remain executor-local until the activity
completes, so an executor crash can lose in-flight local output.

Each Terraform command inside that session is bounded separately by
`OPENPLAN_TERRAFORM_TIMEOUT` (default 45 minutes), read by the API and stamped
onto the workflow input when the run is dispatched, so the budget a run started
with stays visible in its history. The ceiling is per command, not per run: a
run issues `init`, workspace selection, and then `plan`, `apply`, or `destroy`,
and each gets the full budget. While a command runs, the executor heartbeats
every 20 seconds, and Temporal fails the activity if two minutes pass without
one -- six intervals of slack, because a Terraform command is not retried and a
heartbeat lost to a pause should not kill a run mid-apply. The heartbeat
carries no payload: Temporal reads only its arrival, so what keeps the activity
alive is that one came, not what it said.

### Valkey

Valkey (or any server speaking the Redis protocol) carries each running command's output from the executor that runs it to whichever API replica a viewer is on. It is required: the API and the executor both refuse to start without it. It holds only live output, in memory, with no persistence and `maxmemory-policy noeviction`; finished logs come from the artifact store. Each process signs in as its own user, and the executor's can only append. See Logs.

### Executors

Executors are Go processes that poll the `execution` task queue and run the activities that sit next to tenant Terraform. They open no database connection and parse no key. Secrets a run needs, the repository token and the credential environment, are sealed on the control plane to an X25519 key the executor generates for that run in `PrepareWorkspace` (`internal/runseal`), so Temporal history holds only a public key and ciphertext. Results, including uploaded log metadata, return to the control plane through Temporal. Their one new route outside Temporal, beside the artifact store and GitHub, is to Valkey, as a user that can only append live output (see Logs).

The queue loop runs in the API. Multiple API replicas can safely run it because claims use short Postgres leases and `FOR UPDATE SKIP LOCKED`; no database transaction remains open during the Temporal RPC.

For MVP, executors use a local process runner:

```text
LocalTerraformRunner
```

Each Terraform-compatible activity uses the OpenTofu CLI (`tofu`) and:

- Creates a fresh temporary working directory.
- Clones the template GitHub repository.
- Checks out the selected ref.
- Changes into the template root path.
- Reads `template.yaml` as metadata when needed.
- Runs `tofu init`.
- Selects or creates the assigned Terraform workspace.
- Runs `tofu plan`, `tofu apply`, or `tofu destroy`.
- Streams redacted logs.
- Persists redacted logs.
- Cleans up the temporary working directory.

The runner should be implemented behind an interface so a future `KubernetesJobRunner` or sandboxed runner can be added without changing workflow semantics.

### Runner Security

Terraform code is untrusted. A template is arbitrary code: OpenTofu/Terraform providers, provisioners, external data sources and `local-exec` run anything inside the executor pod, with access to the run's injected credentials. Publishing templates is limited to editors, but that only limits who can write a template, not what its code can do. Every executor-side design must assume the template is hostile.

Every executor activity that touches tenant content runs in a gVisor sandbox of its own (#245), started by the executor with runsc: `FetchSource`, each `RunTerraform` command, `UploadLog`, `UploadPlan` and `DownloadPlan`. The Temporal worker stays outside every sandbox, with its connection to Temporal and the run keys. A sandbox boots a read-only guest root (tofu, git, CA certificates and the executor binary) and runs the activity's body as `openplan-executor activity <name>`. `internal/sandbox` holds the mechanism; `internal/activities` splits each activity into a worker-side shim and the body.

| Mechanism | What it stops | Where |
|---|---|---|
| A fresh gVisor sandbox per activity, with its own kernel (gVisor's Sentry) and process space | A run reading another run's processes, files or credentials, or the executor's; a kernel exploit reaching the executor's kernel | `runsc.go`, `bundle.go` |
| Credentials only over stdin; tofu's sandbox gets only the run's cloud credentials; storage access and the plan key only in sandboxes that run no tenant code | Template code holding the artifact store's keys, the plan key or the Temporal identity | `internal/activities/template_run.go`, `bodies.go` |
| An environment built from scratch | The executor's own environment reaching a sandbox | `env.go` |
| A network namespace per sandbox, wired to the executor's network through a bridge and NAT; its port on the bridge isolated, and traffic between sandboxes dropped; DNS through a relay in the executor | A run reaching another run's sandbox over the network; a sandbox taking the executor's own address, which runsc would do in the executor's namespace | `network.go`, `dns.go` |
| Memory (with no swap), CPU and process limits per sandbox, or no sandbox | One run starving the others | `bundle.go`, `cgroup.go` |
| The worker never opens a path a sandbox wrote, and our code inside a sandbox reads tofu's files as regular files only, without following links out | A planted symlink turning the worker, or a storage sandbox, against its own secrets | `frame.go`, `files.go` |
| The log spool outside the workspace, capped at 64 MiB per command | tofu editing its own log; one run filling the disk through its output | `internal/activities/spool.go` |
| Sandbox user 70000, no capabilities, `no_new_privs` | Template code holding any privilege inside its sandbox | `bundle.go` |

What it does not cover:

- Egress: a sandbox reaches whatever the executor reaches, through NAT. tofu reaches Temporal, the api, Postgres, Valkey and the cloud metadata address. Block the metadata address outside openplan (an IMDSv2 hop limit of 1 on AWS; a NetworkPolicy on Kubernetes).
- Temporal has no access control yet (#246).
- The run's own cloud credentials: tofu needs them, so a template's author can use or copy the credentials of whoever runs it.
- Disk: a workspace has no quota.
- Escaping gVisor: code that escapes lands as root in the executor container, which runs with `SYS_ADMIN` and with Docker's seccomp and AppArmor profiles off.

`SECURITY.md` (#321) should list these gaps once it exists.

MVP executor deployments should use the following guardrails:

- Run the executor as `docker-compose.yaml` does: root with `cap_drop: [ALL]`, `cap_add: [SYS_ADMIN, SETUID, SETGID, SETFCAP, CHOWN, NET_ADMIN, NET_RAW]`, `no-new-privileges`, `seccomp=unconfined`, `apparmor=unconfined`, `net.ipv4.ip_forward=1`, a private cgroup namespace, and the run root (mounted `exec`, since providers run from it) and sandbox state on tmpfs. On Kubernetes that takes the Pod Security level "privileged" and an allowed `net.ipv4.ip_forward` sysctl. The executor refuses to start without any of these.
- Keep the sandboxes' subnet (`EXECUTOR_SANDBOX_SUBNET`, default `10.237.0.0/24`) clear of every network tofu must reach: addresses inside it route to the sandboxes' bridge.
- Avoid host mounts and shared writable volumes beyond the artifacts volume.
- Inject only the credential sets required for the target stack or template run, and prefer short-lived credentials.
- Avoid persisting secret values to Postgres, object storage, Temporal payloads, or logs; known values are redacted before a log is spooled.
- Separate executors by task queue or deployment if future tenants, templates, or credential sets require stronger trust boundaries.

Further isolation, such as a Kubernetes Job per run, remote runners, or per-tenant runner pools, remains a deferred design topic. `sandbox.Runner` is the seam: a backend that runs a job somewhere else needs no change to the activities.

## Go Package Layout

The Go implementation should keep product concepts, application use cases, adapters, and execution orchestration separated by package boundaries.

```text
cmd/
  api/
  executor/

internal/
  domain/
  app/
  api/
  auth/
  postgres/
  dispatch/
  temporal/
  workflows/
  activities/
  runner/
  terraform/
  githubapp/
  secrets/
  artifacts/
  events/
  logsink/
  locks/
  config/
```

Package ownership:

- `cmd/api`: control plane boot: config loading, dependency wiring, HTTP server, the queue loop, and the Temporal worker on the `control` queue.
- `cmd/executor`: data plane boot: Temporal worker on the `execution` queue with the execution activities. No database, no keys.
- `internal/domain`: the product's core entities and shared contracts, including IDs, statuses, operation types, validation helpers, entities such as `Tenant`, `Template`, `Stack`, `StackTemplate`, `TemplateRun`, `StackRun`, and `CredentialSet`, plus Temporal workflow payloads, signal names, query names, and constants. Keep this package focused on stable cross-boundary data contracts; concrete behavior and side effects belong in the packages that own them.
- `internal/app`: application use cases such as creating stacks, registering templates, adding templates to stacks, starting runs, approving runs, discarding plans, listing runs, and fetching log metadata. This package owns use-case interfaces for persistence, workflow dispatch, events, locks, artifacts, and secrets; concrete adapters implement those interfaces outside `app`.
- `internal/api`: HTTP handlers, request and response DTOs, routing, SSE endpoints, API validation, and mapping API input into app commands.
- `internal/auth`: mock identity for MVP, tenant/user context extraction, and the future authentication boundary.
- `internal/authorization`: embedded OpenFGA and the only authorization package. It owns the model, in-process store and model bootstrap, `Can`/`CanAll`/`ListGrants` checks, and `Grant`/`Revoke` tuple writes that join the caller's Postgres transaction. `app` calls it directly; there is no provider port. See [Authorization](#authorization).
- `internal/postgres`: Postgres repositories, transactions, SQL queries, persistence models, workflow-outbox operations, and migration helper code.
- `internal/dispatch`: broker-free Postgres-to-Temporal dispatch loop. It leases pending workflow-start intents, invokes the narrow workflow starter interface, marks successful entries complete, and schedules failed entries for retry.
- `internal/temporal`: Temporal client adapter that implements `app` workflow-dispatch interfaces and is wired in `cmd`. API and app code should depend on interfaces, not on this adapter package directly.
- `internal/workflows`: deterministic Temporal workflow definitions such as `TemplatePlanWorkflow`, `TemplateSyncWorkflow`, and future `StackRunWorkflow`.
- `internal/activities`: Temporal activities that perform side effects such as cloning repositories, parsing templates, acquiring locks, running the OpenTofu CLI for Terraform-compatible operations, persisting logs, and writing activity events.
- `internal/runner`: the OpenTofu-backed `LocalTerraformRunner` and the `LocalGitRunner`, which build their commands and start them through the shared `CommandExecutor`.
- `internal/terraform`: Terraform-specific helpers for HCL variable parsing, tfvars rendering, backend metadata extraction, plan summary parsing, and workspace command modeling.
- `internal/githubapp`: GitHub App integration, installation token generation, repository clone helpers, and ref-to-commit resolution.
- `internal/secrets`: secret store boundary, credential set resolution, and Vault or local development adapters.
- `internal/artifacts`: object storage adapter, redacted log artifact writes, plan summary artifacts, and artifact key generation.
- `internal/events`: pluggable `EventBus` interface, event types, and no-op or in-memory implementations.
- `internal/logsink`: a command's output from where it is written to where it is read: the spool file on the executor (its name and the sink that opens it in the run's logs directory), and live output through Valkey: the stream key and entry format, the executor's flusher that publishes the spool as it grows, and the api's registry of tailers that feeds the SSE endpoint.
- `internal/locks`: product-level `StackTemplate` lease lock, including acquire, refresh, release, fencing token checks, and stale lock handling.
- `internal/config`: environment and config structs, default values, and config validation.

Temporal workflows should stay thin and deterministic. Workflows decide sequence; activities perform side effects through interfaces.

Workflow code may import `domain` and Temporal SDK packages, but it should not import concrete adapters such as `postgres`, `githubapp`, `secrets`, `artifacts`, or `logsink`.

Package dataflow:

```text
                   core entities and shared contracts
                      +----------------------+
                      | internal/domain      |
                      +----------+-----------+
                                 ^
                                 |
cmd/api                         |                   cmd/executor
  config + wiring                |                     config + wiring
        |                        |                           |
        v                        |                           v
+----------------+        +------+-------+          +--------------------+
| internal/api   +------->+ internal/app |          | internal/workflows |
| HTTP/SSE DTOs  |        | use cases    |          | deterministic      |
+-------+--------+        +------+-------+          +---------+----------+
        |                        ^                            |
        |                        | app-owned interfaces       |
        |                        |                            v
        |              +---------+----------+       +--------------------+
        |              | concrete adapters  |       | internal/activities|
        |              +---------+----------+       | side effects       |
        |                        |                  +---------+----------+
        |                        |                            |
        |                        v                            v
        |              postgres, temporal,          runner, terraform,
        |              events, artifacts,           githubapp, secrets,
        |              secrets, locks               artifacts, events,
        |                                             logsink, locks,
        |                                             postgres
        |
        v
internal/auth
tenant/user context
```

The API composition path additionally wires `internal/postgres` as the durable work queue, `internal/queue` as the claim/retry loop, and `internal/temporal` as the workflow starter, assembled in `cmd/api`.

## Template Model

Templates are GitHub sourced in MVP.

```text
Template
  tenant_id
  github_installation_id
  repo_owner
  repo_name
  default_ref
  root_path
  metadata from template.yaml
  inferred Terraform variables
```

Template source is identified by:

```text
repo_owner/repo_name
ref
path
```

`template.yaml` is intentionally metadata-only:

```yaml
name: vpc
description: Creates a VPC
tags:
  - network
  - aws
```

It does not define input schema, secret schema, dependency schema, or backend config.

## Template Registration Workflow

Template registration and refresh run as background Temporal workflows.

```text
TemplateSyncWorkflow
  -> generate GitHub App installation token
  -> clone repo/ref into fresh temp dir
  -> validate root path exists
  -> read template.yaml
  -> parse Terraform root module files
  -> extract variable metadata
  -> persist template metadata and inferred variables
  -> mark template active or invalid
```

The UI can show states such as:

```text
pending_validation
validating
active
invalid
```

Terraform variable parsing should use HashiCorp HCL/Terraform parsing libraries, not regex.

Inferred variable metadata includes:

- name
- type expression
- description
- required status
- default presence
- sensitive flag
- validation presence

A variable is considered required when the Terraform variable block has no `default`.

## Stack Model

A stack is a logical infrastructure composition.

```text
Stack
  tenant_id
  name
  slug
  tags
  default credential sets
```

Stack tags are key-value pairs.

```text
environment = production
team        = payments
region      = us-east-1
```

## StackTemplate Model

A `StackTemplate` is one installed template inside one stack.

```text
StackTemplate
  stack_id
  template_id
  selected_ref
  workspace_name
  config_json
  last_applied_run_id
  last_applied_ref
  last_applied_at
  lifecycle state
```

The `StackTemplate` is the Terraform state boundary.

Each `StackTemplate` gets one stable, unique Terraform workspace name when it is created. That workspace remains attached to the `StackTemplate` for its full lifecycle.

Example:

```text
Stack: prod-payments
  vpc      -> workspace mtp_acme_prod_payments_vpc_a13f9c
  eks      -> workspace mtp_acme_prod_payments_eks_b47d21
  postgres -> workspace mtp_acme_prod_payments_postgres_91aa0e
```

The workspace name is:

- generated by the platform
- unique per `StackTemplate`
- stable across all runs
- not user-editable after creation
- included in run metadata and audit logs
- injected into every Terraform run

MVP requires workspace-compatible templates. The runner fails early if it cannot select or create the assigned Terraform workspace.

## Terraform Backend

MVP does not manage Terraform backend infrastructure.

Templates must bring their own backend configuration. The platform does not provision state buckets, state tables, encryption keys, or backend config files.

The platform still enforces workspace selection:

```text
tofu init
tofu workspace select -or-create=true <workspace>
```

The isolation contract is:

```text
backend owned by user/template
workspace owned by platform
```

MVP templates should use workspace-compatible remote backends. Backend state locking is required where the selected backend supports it, because the platform lock prevents concurrent openplan runs but does not replace Terraform's backend-level state lock.

Unsupported or risky backend configurations should fail validation or receive an explicit warning before execution. Examples include:

- local backends
- backends that do not support workspaces
- remote backends without state locking when locking is available
- backend configuration that changes unexpectedly between runs

Each run should record backend identity metadata, such as backend type and a normalized backend configuration hash when it can be safely derived without storing secrets. If backend identity changes between runs for the same `StackTemplate`, the system should emit an activity event and surface a warning in the UI.

## Inputs And Secrets

Template input schema is not declared in `template.yaml`.

The platform infers Terraform variables from root module `.tf` files and lets users enter non-sensitive values from the UI.

Non-sensitive values are stored per `StackTemplate`:

```text
StackTemplate.config_json
```

At runtime, these values are rendered into Terraform variable input, such as:

```text
terraform.tfvars.json
```

The platform does not collect secret Terraform input values in MVP. Templates should fetch secret material directly from Vault using Terraform providers/data sources.

Runner secrets are execution credentials, not template input values. Examples:

- Vault credentials
- cloud provider credentials
- GitHub App installation token

These are resolved on the control plane, sealed to the run's executor key, and opened only inside the executor for the Terraform subprocess environment. Known secret values are redacted from logs before streaming or persistence.

## Credential Sets

Credential sets represent execution credentials needed by Terraform.

```text
CredentialSet
  tenant_id
  name
  provider_type
  secret_store_ref
```

Stacks inherit credential sets by default:

```text
Stack default credential sets
  -> inherited by all StackTemplates
```

Future versions may allow `StackTemplate` level credential overrides or additions.

GitHub source access is not modeled as a stack credential. GitHub source access uses the tenant's GitHub App installation.

## GitHub Integration

MVP supports GitHub only.

Tenants connect GitHub by installing a GitHub App. The platform stores installation metadata, not long-lived Git credentials.

```text
GitHubIntegration
  tenant_id
  installation_id
  account_login
  account_type
  status
```

As of the private-repository work (#239) no `GitHubIntegration` row exists yet. The API holds
the App's id and private key in configuration and resolves the installation covering a repository
on demand via `GET /repos/{owner}/{repo}/installation`, then mints a repository-scoped read-only
token for the clone. Nothing is persisted, so no schema or lifecycle has to stay in sync with
GitHub, and one App installed on several organizations works without additional configuration.

The table above becomes real with the installation-lifecycle work, where it serves as a cache and
an allowlist in front of that same resolver, governed by the `github_integration` OpenFGA type
(#144). Until it exists, any principal who can publish a template can cause a clone from any
repository the App was installed on.

Resolving that token is best effort, and deliberately so. The installation endpoint 404s both for
a private repository nobody granted access to and for a public one nobody ever needed to install
the App on, and those two are indistinguishable from the API; a 503 says nothing about the
repository at all. So a failed resolution never fails the fetch. The clone proceeds with no
credential, which is exactly right for a public repository and fails at git for a private one,
carrying the reason it was unauthenticated.

What that failure means then depends on the cause. A missing installation is permanent and is
recorded as an invalid registration for the user to act on. A transient API failure is returned as
an error instead, so Temporal retries it under `syncRetryPolicy` rather than burning the
registration on an outage -- an outcome returned with a nil error reads as a successful activity
and would spend none of the attempts the policy grants.

The installation token reaches git through `GIT_CONFIG_*` environment variables carrying an
`http.https://github.com/.extraheader` header, never through the clone URL or argv, so it is not
written into the run workspace's `.git/config` — the directory Terraform then executes from.

Not covered: repositories using git-LFS, and Terraform module sources of the form
`source = "git::https://github.com/org/private-module"`, which Terraform fetches itself without
the credential.

During template registration the API mints a short-lived GitHub App installation token and clones the repository itself. During a run the API mints the token and seals it to the run's executor key; the executor opens it and clones over HTTPS.

Selected refs are resolved to immutable commit SHAs during template sync and every run. A `TemplateRun` stores both the user-selected ref and the resolved commit SHA. For apply operations, the post-approval apply uses the same resolved commit SHA that produced the displayed plan unless the user starts a new run.

## Reliable Template Run Dispatch

Starting a template run uses a transactional outbox instead of an API-side Postgres/Temporal dual write:

```text
API request
  -> Postgres transaction
       -> insert queued template_runs row
       -> insert workflow_outbox row
  -> commit and return

API queue loop
  -> lease one pending row
  -> start the Temporal workflow
  -> mark the row processed, or schedule a retry
```

The outbox ID and Temporal workflow ID are both deterministic:

```text
template-run/{tenant_id}/{run_id}
```

Temporal is configured to use an existing running execution and reject a duplicate completed execution. Consequently, dispatch is at least once but workflow creation is idempotent: the queue loop can safely retry after a timeout, crash, or successful Temporal start followed by a failed Postgres completion update.

Dispatchers poll Postgres for eligible rows, claim with `FOR UPDATE SKIP LOCKED`, and store a short lease before calling Temporal. The claim transaction ends before the network call. A failed start clears the lease and defers the next attempt; an abandoned lease becomes claimable after it expires. This lets multiple API replicas drain the same table without a separate message broker or a long-running database transaction.

Migration `0007_workflow_outbox.sql` also backfills existing queued template runs. Replaying a run that was already submitted to Temporal is safe because it targets the same deterministic workflow ID.

## TemplatePlanWorkflow

`TemplatePlanWorkflow` is the main execution workflow.

A run's `status` is its lifecycle state; what it is doing while `running` is
its `step`, written as each step starts and kept when the run ends.

Plan run:

```text
queued → running → completed
```

Apply or destroy run:

```text
queued → running → waiting_approval → approved → running → completed
```

An auto-approved apply goes `queued → running → completed`. Any non-terminal
run can end `failed`, and a plan waiting for approval, or approved but not yet
claimed by its apply, can be discarded, which ends it `canceled`.

Steps, in order: `waiting_for_executor`, `preparing_workspace`,
`fetching_source`, `restoring_plan` (approved apply only), `initializing`,
`selecting_workspace`, then `planning` and `saving_plan` in the plan phase or
`applying` in the apply phase.

What a run does to its stack template is recorded as an event, not a status:
`applied` makes the run the template's live state, and a destroy records
`destroying` before it destroys and `destroyed` once it has.

If a destroy run fails after recording `destroying`, its associated
`StackTemplate` lifecycle transitions from `destroying` to `failed`. This does
not roll back infrastructure changes; the template is treated as unsafe to run
again until an explicit recovery or reconciliation flow exists.

A run in flight cannot be stopped. A plan waiting for approval can be
discarded, which ends the run `canceled` in one write and drops the saved
plan's key; nothing is running at that point, so there is nothing to unwind.

The workflow should acquire a product-level lock before running Terraform so only one active run can target a `StackTemplate` at a time.

## Approval

MVP approval is intentionally simple.

- Apply runs perform a plan first.
- Workflow enters `waiting_approval`.
- UI shows plan logs and summary.
- Any mock authenticated user in the tenant can approve.
- API records the approval actor and timestamp.
- API sends an approval signal to Temporal.
- Workflow resumes and runs apply.

Apply may re-run planning after approval. MVP does not persist binary `tfplan` files for exact approved-plan apply.

The UI should communicate that approval is based on the displayed plan, but apply will re-evaluate before execution. The re-evaluation must use the same resolved commit SHA as the approved plan.

A plan's resource changes are what a reviewer reads before approving (#420). The plan's sandbox reads `tofu show -json` with `internal/planchanges` (on HashiCorp's `terraform-json`, which reads OpenTofu's plans too), replaces every value the plan marks sensitive with `(sensitive)`, sorts the changes removals first, and cuts them to `domain.MaxPlanResourceChanges` and `MaxPlanChangesBytes`. Only that list leaves the sandbox, in its result frame; the JSON, with its variable values and prior state, never does. RunTerraform's worker writes it beside the command's log, `UploadLog`'s storage sandbox puts it in the artifact store with the log, and `FinishPlan` checks it again (it came from the sandbox that ran the template's code) and stores it in `template_run_resource_changes` in the transaction that records the plan's counts. Nothing passes through Temporal's history, and the workflow's commands are unchanged. A list that can't be read, such as a plan format major version other than 1, is recorded as unavailable and never fails the plan: the plan log still has every change. `GET .../template-runs/{run_id}/changes` serves the list; the run's `plan_changes` says whether it is complete, truncated or unavailable.

## Logs

Each Terraform command's output is one log. While the command runs, the run
page follows it live through Valkey; once the command ends, the copy stored in
the artifact store (S3) is the log, and Valkey's copy expires. The two never
mix: a viewer reads a log from one source or the other, chosen when it opens
the log.

```text
EXECUTOR                                  STORES       API (any replica)                     BROWSER
tofu (sandbox) -> redaction -> 64 MiB cap
  -> spool file
     flusher, ~1 s -------- XADD -------> Valkey ----- XREAD --> tailer, one per live log
                                                                   -> GET .../logs/{name}/events -> live
UploadLog, after tofu exits -- PUT -----> S3 --------- GET ----> GET .../logs/{name} ----------> complete
  result via Temporal -------------------------------------------> run workflow
                                          Postgres <-- 1. row: complete | lost --+
                                          Valkey   <-- 2. end entry, expire -----+
```

**Writing.** `RunTerraform` writes tofu's output to a spool file on the
executor, outside the workspace, already redacted (exact credential values
replaced) and capped at 64 MiB. A flusher in the executor process tails that
file about once a second and appends what is new to the log's Valkey stream,
in pieces of at most 1 MiB, each carrying its first byte's offset so the api
can drop bytes a viewer already holds. A piece never ends inside a multi-byte
UTF-8 sequence, so each decodes as text on its own. It tails the file rather than sitting in
tofu's path, so a slow or unreachable Valkey costs the live view, never the
command: the spool absorbs the delay and the next flush carries more. When
tofu exits, `UploadLog` PUTs the whole spool to S3, tried up to five times.
Nothing stored is ever rebuilt from Valkey.

**Valkey** holds one stream per live log, keyed `runlog:{<run_id>}:<name>`.
The braces are a cluster hash tag, so every stream of a run lands on one
shard. It keeps them in memory only, with no persistence, because nothing
depends on them surviving a restart: the stored log comes from the spool. It
runs with `maxmemory-policy noeviction`, because an evicted stream would look
like a log that never ends; a full Valkey refuses new output instead. Size
`maxmemory` at the live cap (below) times the most commands that run at once,
plus headroom. A stream gets a 24 hour expiry, as the safety net for a log
nobody ends, from the first flush that appends to it while it has none, so a
stream Valkey lost to a restart gets it back and a live one is never extended.
It is shortened to 60 seconds once the log ends, long enough for a viewer that
is reconnecting to see the end.

Valkey is required. The api and the executor read `VALKEY_URL` (`redis://`, or
`rediss://` for TLS) and refuse to start without a reachable server. There is
no in-memory fallback, because the two are separate processes and memory in
one is invisible to the other.

**The executor's one new route outside Temporal** (it already reaches the
artifact store and GitHub) is to Valkey, for live output only. Its Valkey user
can sign in, `PING`, append to `runlog:*` streams and set their expiry, and
nothing else; it cannot read a stream. Status, results and every row still
travel through Temporal. tofu's sandbox gets an environment built from
scratch, so it never sees `VALKEY_URL`; it can reach Valkey's port (see
Runner Security), which is why Valkey must require authentication. One
Valkey user serves every tenant's runs on an executor, as its S3 credential
does, so a compromised executor can corrupt what viewers see of any live log.
The same append right lets it forge an end entry, which ends live views early
but changes nothing stored: the row, its state and its object key are written
on the control plane only.

**The row.** Postgres holds each log's metadata in `template_run_logs`, never
its body. A row moves `live -> complete | lost`, and only the run workflow
moves it:

- It writes `live` before `RunTerraform`, so the run page can open the log
  while the command runs. The page learns of the new command by refetching the
  log list whenever the run's status or step moves, as it does when each
  command starts.
- After `UploadLog`, `RecordTemplateRunLog` sets `complete` (with the object
  key, size and content type) or `lost`, then appends the end entry to the
  stream and shortens its expiry. The row comes first, so once a viewer can
  see the log ended, its end entry is on the way.

A lost log does not fail the run: the command's own result stands, and only
its output is missing. The row says why:

- `upload_failed`: `UploadLog` failed every attempt.
- `executor_gone`: the executor that ran the command died, and its spool with
  it.
- `run_terminated`: the row is still `live` but its run is `canceled`, so the
  workflow was stopped before it could record the log.
- `record_failed`: the row is still `live` but its run is `completed` or
  `failed`: the commands ran to the end, so the workflow reached the log's
  final record and every attempt at it failed.

The last two are not stored: the api derives them from the run's status when
it reads a `live` row, which needs no reconciler. The stream, if any, expires
on its own. A run whose workflow is terminated outside openplan (by hand in
Temporal, or by a workflow timeout) keeps its status today, so its live rows
read `live` until that is fixed; it is tracked as a follow-up issue.

**Reading.** The viewer picks its source once, when it opens a log, from the
row's state:

- `live`: `GET /v1/tenants/{tenant_id}/template-runs/{run_id}/logs/{name}/events`,
  a Server-Sent Events stream. It sends the log from the stream's start as
  `log` events, then each piece as it arrives, then one `log_end` with the
  row's final state, and closes. Every 20 seconds it writes a comment, so
  proxies keep a quiet stream open, and re-reads the row: a row that ended
  without its end entry landing still ends the stream.
- `complete`: `GET .../logs/{name}` returns the body from S3, once; it never
  changes.
- `lost`: nothing to show; the row gives the reason.

The page never asks for the body of a log that is not complete. The body
endpoint answers `log-live` (409) for a live log and `log-lost` (404) for a
lost one, for anyone scripting the API.

Nothing switches mid-stream. A live viewer keeps the output it has already
shown rather than swapping to the stored copy, which would mean resuming S3 at
a byte offset. A reconnect is a fresh choice: the browser resumes from its
last event id, and the api sends none of the bytes the browser already
holds, so the page appends every event as it comes. If the row has ended by
then, the endpoint sends `log_end` at once and the page offers the stored
log.

Each api replica runs one tailer per live log that has a viewer on it, which
blocks on `XREAD` and fans entries out to that replica's viewers, so a log
watched by many browsers costs one Valkey read per replica, and viewers add no
load to the executor, Postgres or S3. A replica follows at most 256 live logs
at once. A viewer it cannot follow, because it is full or Valkey failed, still
gets a stream: one that asks the browser to reconnect in three seconds.

**The live cap.** A stream carries at most 8 MiB of a log, because Valkey
holds every live log in memory at once. Past it the flusher appends one
truncated entry and stops publishing; the viewer gets `log_truncated`, and the
page says live output stops there and, once the command ends, offers "Load the
full log" from S3. The spool keeps going to its own 64 MiB cap, so the stored
log is the whole one.

Object storage keys should be tenant and run scoped:

```text
tenants/{tenant_id}/runs/{run_id}/logs/{phase}.log
```

Each log is named for its run phase and its number among that phase's
logs, counting from 1:

```text
plan-1.log    tofu init
plan-2.log    tofu workspace select
plan-3.log    tofu plan
apply-1.log   tofu init
apply-2.log   tofu workspace select
apply-3.log   tofu apply
```

The name says nothing about the command. What wrote the log, its run phase,
number and command, is recorded beside it, and the run page shows the
command under its phase. The workflow numbers its own commands as it runs
them, so a plan and the apply that follows it a day later never have to agree
on a count, a retry or replay keeps the same name, and a command run twice in
a phase gets a log each time. The control plane derives the object key from
the run and the name, so an executor never chooses which object a run's log
points at. Postgres derives the name from the run phase and number rather
than storing it beside them, and the API checks only its shape (one
lowercase path segment).

## Activity Logs

Activity logs capture user and system actions.

Examples:

- template registered
- template validation failed
- stack created
- template added to stack
- run queued
- lock acquired
- plan completed
- approval submitted
- apply completed
- destroy completed
- lock released

Activity records include actor metadata:

```text
actor_user_id
actor_display_name
actor_email
tenant_id
timestamp
resource_type
resource_id
action
metadata
```

Auth is mocked in MVP, but actor fields are real so the system can adopt proper auth later.

## Persistence

Postgres stores product metadata and queryable state:

- tenants
- mock users
- GitHub integrations
- templates
- template versions or sync attempts
- inferred template variables
- stacks
- stack tags
- stack templates
- workspace names
- credential set references
- template runs
- stack runs
- approvals
- activity events
- log metadata
- durable workflow-outbox entries and their dispatch state

Run records should capture the selected template ref, resolved commit SHA, operation type, trigger source, trigger actor, workspace name, backend identity metadata, lifecycle status, started/completed timestamps, and error summary.

Object storage stores:

- redacted run logs
- future artifacts

Secret store stores:

- cloud provider execution credentials
- Vault execution credentials
- other runtime credential material

Temporal stores:

- workflow execution history
- timers
- signals
- task queue state
- retry/cancellation state

Postgres remains the product source of truth for the UI.

## Scaling

Executors scale horizontally as Kubernetes deployments.

Each executor pod has local concurrency limits, for example:

```text
max concurrent Terraform activities per pod = 2
```

Total execution capacity is:

```text
executor replicas * per-pod Terraform concurrency
```

If all executors are busy, new tasks remain queued in Temporal until an executor has capacity.

Session affinity makes these capacity settings distinct. The ordinary Terraform
activity concurrency shown above controls activity execution; it is not the
session limit. Temporal Session Workers have separate session execution
capacity. This implementation enables session workers but does not set
`MaxConcurrentSessionExecutionSize`, so that capacity remains at the Temporal Go
SDK default. An apply or destroy run waiting for approval continues to occupy
its session until it completes or the 24-hour session execution timeout expires.
Separate workflow runs may be assigned to different replicas while all replicas
continue polling the shared `execution` task queue. No shared filesystem is
required for the session's executor-private `EXECUTOR_RUN_ROOT` live workspace while
the session owner remains available. If the session-owning executor crashes,
Temporal fails the session and the run; this design does not automatically
re-establish a session or reconstruct its local workspace on another replica.

HPA can be added using metrics such as:

- Temporal task queue backlog
- schedule-to-start latency
- active Terraform activity count
- CPU and memory as secondary signals

`EXECUTOR_RUN_ROOT` is an executor-private local root for each run's temporary
workspace and local phase-log spool. It is not a shared-volume configuration:
another replica cannot use the path to recover an interrupted session. Durable
recovery would require rehydrating the workspace from durable inputs before a
new session can continue.

## Deferred Design Topics

### Data Sharing Within A Stack

Automatic output-to-input wiring is deferred.

Future design should answer:

- how outputs from one `StackTemplate` are persisted
- how secret outputs are classified
- how secret outputs are stored
- how downstream templates reference upstream outputs
- whether dependencies are explicit or inferred from references

### Stronger Runner Isolation

MVP uses local subprocess execution inside executor pods for speed. 1.0 adds a gVisor sandbox per session (#245).

Future options:

- Kubernetes Job per `TemplateRun`
- remote execution workers
- per-tenant runner pools

### Authorization

Authorization is OpenFGA, embedded in the API process. There is no OpenFGA
service to run and nothing to provision: the API applies OpenFGA's migrations to
the application database, then adopts or creates its store and model from
`internal/authorization/authorization-model.fga` at startup. The executor does no
authorization work and embeds nothing.

`internal/authorization` is the whole boundary. It exposes `Can`, `CanAll`,
`ListGrants`, `Grant` and `Revoke` directly, with no provider-neutral port in
front of it, because no other provider is planned. A check returns
`(bool, error)`: denial is `false, nil`, and any error means the answer could not
be determined. Only a denial becomes `ErrForbidden`; a failure surfaces as a
500, never as a 403.

Tuple writes are transactional with domain writes. OpenFGA's datastore opens and
commits its own transaction, so `internal/authorization/write.go` replaces its
`Write` with a transcription that runs on the caller's `pgx.Tx`. Creating a
stack writes the stack row, its audit event, and its owner and parent tuples in
one commit, and role changes do the same, so there is no queue between a domain
change and the grant it implies.

That transcription mirrors upstream deliberately and will drift silently if
upstream changes its schema. `differential_test.go` compares its rows with
upstream's, column for column; it needs a real database, so run
`make differential-test` after every OpenFGA upgrade.

Still deferred: approval policies, and separate permissions for plan, apply,
destroy, and template registration.

### Managed Backends

MVP requires templates to bring their own Terraform backend.

Future versions may support managed backend profiles for S3, GCS, AzureRM, or Terraform Cloud.

### Log Retention And Access Rules

The MVP persists redacted logs and log metadata, but detailed retention and access policy design is deferred.

Future design should answer:

- how long run logs are retained
- how object storage log encryption is configured
- how tenant isolation is enforced for log reads
- how max log size and truncation are handled
- how redaction failures are handled
- whether discovered-after-the-fact secrets require log rewriting or deletion

### Variable Input Limits

The MVP infers Terraform variables and supports non-sensitive UI-entered values, but advanced variable editing rules are deferred.

Future design should answer:

- how complex types such as `object`, `map`, `list`, `set`, and `tuple` are edited
- whether unsupported complex values require raw JSON input
- how nullable values are represented
- how Terraform validation metadata is surfaced
- how sensitive variables are displayed as unsupported in the UI

### Observability

The MVP identifies useful scaling metrics, but a full observability contract is deferred.

Future design should answer:

- which structured log fields are required across API, executor, and workflow code
- which run and phase duration metrics are emitted
- how cancellation, failure, and approval wait time are measured
- whether traces connect API requests, Temporal workflows, activities, and log streams

### StackTemplate Lifecycle States

`StackTemplate` has a lifecycle state, but the full lifecycle state machine is deferred.

Future design should answer:

- which states exist for active, destroying, destroyed, failed, and orphaned templates
- whether destroyed templates retain workspace names and history
- how deletion differs from infrastructure destroy
- how lifecycle state affects allowed plan, apply, and destroy operations

### Drift And Refresh Behavior

The MVP supports user-triggered plan, apply, and destroy. Dedicated drift detection and refresh behavior are deferred.

Future design should answer:

- whether normal plan runs are enough for MVP drift visibility
- whether latest plan status should be stored on `StackTemplate`
- whether scheduled drift checks are needed
- how drift findings appear in activity history and the UI

### Monorepo Template Layouts

A repository holding several templates under `templates/` alongside shared
modules under `modules/`, where templates source those modules with relative
paths such as `../modules/vpc`, already executes correctly. The runner performs
a full shallow clone and runs Terraform from the configured root path, so paths
referenced outside that root remain present on disk.

Revision identity is the gap. A revision is `(repo, ref, root_path)` resolved to
a commit SHA and does not record which modules a template references. A commit
touching only a shared module changes a template's effective content without
changing anything the platform recognizes as that template's identity.

Future design should answer:

- whether a revision records the local module paths a template transitively
  references, discovered by parsing `module` blocks during template sync
- how change detection distinguishes a commit affecting one template from an
  unrelated commit elsewhere in the same repository
- whether a module change with no template change should surface a new revision,
  and how a user learns that it did
- how upgrade and drift notifications avoid flagging every template in a
  monorepo on any commit
- whether per-run clone cost justifies sparse checkout once the set of required
  paths is known
- how module sources that escape the repository are rejected or reported

## MVP Summary

The recommended MVP is the minimal local platform architecture:

```text
Temporal OSS + Go SDK
Postgres
S3-compatible object storage
Vault/secret store
GitHub App integration
Go API server
Go Temporal workers
LocalTerraformRunner
```

It prioritizes fast feedback and a clean product model while leaving room for stronger runner isolation and richer governance later.
