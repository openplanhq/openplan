# openplan Helm chart

Installs openplan on a Kubernetes cluster you already run: the api, the
executor that runs every plan and apply in its own gVisor sandbox, and the
web UI behind an Ingress. You bring the services it depends on.

The chart runs openplan in production mode. It refuses plain HTTP, the
published example keys and the example passwords, so a working install needs
real TLS, real keys and an OIDC provider reachable over HTTPS.

## Before you start

### The cluster

- **cgroup v2 on the nodes.** The executor puts each sandbox in its own
  cgroup under its container's private cgroup namespace. On cgroup v1, or a
  host cgroup namespace, it refuses to start.
- **A namespace that allows privileged pods.** The executor runs as root with
  seven added capabilities and no seccomp or AppArmor profile, and an init
  container turns on IP forwarding with `privileged: true`. Pod Security
  `baseline` and `restricted` reject both. The chart doesn't create the
  namespace; you label it (step 1 below).
- **Nodes you control.** GKE Autopilot and EKS Fargate don't allow the
  executor's security settings, so the chart doesn't work there. Use standard
  node pools.
- **An ingress controller** with an IngressClass, and a TLS certificate for
  the host you serve openplan on.

### The services you bring

| Service | What openplan needs from it |
|---|---|
| Postgres | A database the api owns. The api migrates it at startup. |
| Temporal | A frontend address both the api and the executor reach, and a namespace (default `default`). |
| Dex | An OAuth client for openplan with redirect URL `https://<host>/v1/auth/callback`, at an HTTPS issuer URL that resolves the same way from the browser and from the api pod. Other identity providers connect through Dex. |
| S3 | A bucket for run logs and plans, with an access key. Any S3-compatible store works (`S3_ENDPOINT`, `S3_FORCE_PATH_STYLE`). |
| Valkey | Two ACL users: `api`, and `executor`, which may only append. The `valkey` service in `docker-compose.release.yaml` has the exact rules. Turn the default user off. |

**S3 is required.** The filesystem artifact store doesn't work in this chart:
the executor writes logs and plans and the api reads them, and the two run in
separate pods with no shared volume.

## Install

All commands use the namespace `openplan` and the release name `openplan`.

**1. Create the namespace and allow privileged pods in it.**

```sh
kubectl create namespace openplan
kubectl label namespace openplan pod-security.kubernetes.io/enforce=privileged
```

**2. Create one Secret for the api and one for the executor.** Every key in
a Secret becomes an environment variable of that component under the same
name. Keep them apart: the executor gets no database, no encryption keys and
no OIDC secret.

The api's Secret:

| Key | Value |
|---|---|
| `DATABASE_URL` | Postgres URL for openplan's database |
| `TEMPORAL_ADDRESS` | Temporal frontend, `host:port` |
| `VALKEY_URL` | `redis://api:<password>@<host>:6379/0` |
| `CREDENTIAL_ENCRYPTION_KEY` | 32 random bytes as hex: `openssl rand -hex 32` |
| `SESSION_ENCRYPTION_KEY` | 32 random bytes as hex, different from the one above |
| `OIDC_ISSUER_URL` | Dex's issuer, `https://...` |
| `OIDC_CLIENT_ID` | openplan's client ID in Dex |
| `OIDC_CLIENT_SECRET` | openplan's client secret in Dex |
| `OPENPLAN_ROOT_SUBJECT` | The OIDC `sub` of the first administrator |
| `ARTIFACT_STORE_KIND` | `s3` |
| `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` | The bucket and its key |
| `S3_ENDPOINT`, `S3_FORCE_PATH_STYLE` | Optional; for a store other than AWS |
| `TEMPORAL_NAMESPACE` | Optional; default `default` |
| `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY` | Optional, set both or neither; a GitHub App that clones template repositories |
| `OPENPLAN_TERRAFORM_TIMEOUT` | Optional; the longest one Terraform command may run, as a duration such as `90m` |

The executor's Secret:

| Key | Value |
|---|---|
| `TEMPORAL_ADDRESS` | The same as the api's |
| `VALKEY_URL` | `redis://executor:<password>@<host>:6379/0` |
| `ARTIFACT_STORE_KIND` and the `S3_*` keys | The same as the api's |
| `TEMPORAL_NAMESPACE` | Optional; the same as the api's |
| `EXECUTOR_MAX_SESSIONS` | Optional; runs at once on one executor. The chart sets 1; a value here overrides it |
| `EXECUTOR_SANDBOX_MEMORY` | Optional; MiB per sandbox. The chart sets 1536; a value here overrides it. See "Sizing the executor" before you raise it |
| `EXECUTOR_SANDBOX_CPUS`, `EXECUTOR_SANDBOX_PIDS` | Optional; per sandbox, default 2 and 1024 |

For example, from two env files you keep out of version control:

```sh
kubectl -n openplan create secret generic openplan-api-env --from-env-file=api.env
kubectl -n openplan create secret generic openplan-executor-env --from-env-file=executor.env
```

The chart sets `HTTP_ADDRESS`, `OPENPLAN_ENVIRONMENT`, `OPENPLAN_PUBLIC_URL`,
`EXECUTOR_RUN_ROOT` and `EXECUTOR_SANDBOX_STATE` itself. The same keys in a
Secret are ignored.

**3. Create the TLS Secret for your host.**

```sh
kubectl -n openplan create secret tls openplan-tls --cert=tls.crt --key=tls.key
```

If Dex's certificate comes from an internal CA, put that CA in a Secret too.
The api trusts it in addition to the public roots:

```sh
kubectl -n openplan create secret generic openplan-trusted-ca --from-file=ca.crt=internal-ca.crt
```

**4. Write a values file.**

```yaml
api:
  existingSecret: openplan-api-env
  trustedCASecret: openplan-trusted-ca   # only for an internal CA
executor:
  existingSecret: openplan-executor-env
ingress:
  className: nginx
  host: openplan.example.com
  tlsSecretName: openplan-tls
```

**5. Install the chart.** The chart version is the openplan version, and it
runs the images of that version.

```sh
helm install openplan oci://ghcr.io/openplanhq/charts/openplan \
  --version <version> --namespace openplan -f values.yaml
kubectl -n openplan rollout status deployment/openplan-api
kubectl -n openplan rollout status deployment/openplan-executor
```

`helm install` succeeds before any pod is up. Wait for the executor too: if
it can't be scheduled or refuses to start, the UI still works, but every plan
and apply waits ten minutes for an executor and then fails.

**6. Sign in** at `https://<host>` as the user whose `sub` you set in
`OPENPLAN_ROOT_SUBJECT`.

## Values

| Key | Default | What it does |
|---|---|---|
| `image.registry` | `ghcr.io/openplanhq` | Where the `openplan-api`, `openplan-executor` and `openplan-web` images come from. Point it at a mirror in a network without internet access. |
| `image.tag` | the chart's version | Override only to test a build that isn't a release. |
| `api.existingSecret` | required | The api's Secret. |
| `api.trustedCASecret` | none | A Secret whose `ca.crt` the api trusts, for Dex behind an internal CA. |
| `api.resources` | 2 CPU, 2Gi, requests equal to limits | Requests and limits for the api container. Override requests and limits together. |
| `executor.existingSecret` | required | The executor's Secret. |
| `executor.replicas` | `1` | Executors. Each runs one run at a time by default (`EXECUTOR_MAX_SESSIONS`), so this is how many runs go at once. |
| `executor.resources` | 2 CPU, 2Gi, requests equal to limits | Requests and limits for the executor container. Override requests and limits together; see "Sizing the executor". |
| `executor.runRoot.sizeLimit` | `8Gi` | Disk for the workspaces of the runs in flight on one executor. |
| `ingress.className` | the cluster default | The IngressClass. |
| `ingress.host` | required | The host openplan is served on. The api's public URL is `https://<host>`. |
| `ingress.tlsSecretName` | required | The `kubernetes.io/tls` Secret for the host. |

A required value that is missing fails `helm install` with a message naming
the key.

## Sizing the executor

By default each executor pod gets 2 CPU and 2 GiB, reserved on its node
(requests equal limits, so Kubernetes runs it as `Guaranteed`), and runs one
plan or apply at a time in a 1536 MiB sandbox. The other 512 MiB is for the
executor itself.

**More runs at once: add replicas.** Set `executor.replicas` to the number of
runs you want going at the same time. Each replica needs another 2 CPU and
2 GiB on some node.

**When runs outnumber executors**, a new run shows "waiting for executor" and
waits for a free one, up to 10 minutes, whether it's a plan or an apply. If
none frees up in that time, the run fails and you start it again.

**A heavy module: give each run more memory.** If a run's sandbox runs out of
memory, the run fails, and an apply can stop halfway and leave the state
locked. Raise `EXECUTOR_SANDBOX_MEMORY` in the executor's Secret, and raise
the executor's memory with it, so that memory is at least
`EXECUTOR_MAX_SESSIONS` × `EXECUTOR_SANDBOX_MEMORY` + 512Mi. Set requests and
limits together: Helm merges the map, so setting only `limits.memory` keeps
the 2Gi request and the pod is no longer `Guaranteed`. For 3072 MiB per run,
set `EXECUTOR_SANDBOX_MEMORY=3072` in the Secret and:

```yaml
executor:
  resources:
    requests:
      cpu: "2"
      memory: 3584Mi
    limits:
      cpu: "2"
      memory: 3584Mi
```

The run root holds the checked-out template and providers of every run in
flight. When it passes `executor.runRoot.sizeLimit`, the kubelet evicts the
executor pod, and every run on it fails.

## Running it

- **A rollout of the executor kills its in-flight runs.** A run's sandbox and
  its key live in the executor pod, so `helm upgrade`, a node drain or a
  crash fails the runs on that pod. Upgrade when nothing is running.
- **The api runs one replica** and is replaced, not rolled, on upgrade, so
  the UI is down for the seconds the new api takes to start and migrate.
- Neither the run root nor the sandbox state survives a restart. Logs and
  plans of finished runs are in S3.

## Security

- **Block the cloud metadata endpoint on the nodes.** In 1.0 a run's network
  is not filtered: Terraform code can reach the cluster network and the
  node's metadata endpoint (169.254.169.254), and with it the node's cloud
  credentials (#504). On AWS, require IMDSv2 with a hop limit of 1; on GKE,
  use metadata concealment or Workload Identity. Treat every template as
  untrusted code.
- The executor needs a privileged namespace, so keep other workloads out of
  that namespace.

## Troubleshooting

- **The executor restarts at startup.** Its log names what it refused. The
  usual causes: cgroup v1 or a host cgroup namespace on the node; a run root
  mounted `noexec`; IP forwarding off because the init container was blocked
  by Pod Security.
- **The api restarts at startup.** It checks its configuration and fetches
  Dex's discovery document at startup, and its log names what failed. If Dex can't be reached, check that `OIDC_ISSUER_URL` resolves from
  inside the pod, and that the api trusts Dex's certificate (set
  `api.trustedCASecret` for an internal CA).

## What has been tested, and what hasn't

The chart is tested only on Docker Desktop's Kubernetes (cri-dockerd, cgroup
v2, arm64). A pass there does not prove:

- containerd's handling of the cgroup namespace on managed clusters
- AppArmor or SELinux enforcement (GKE's Container-Optimized OS,
  Bottlerocket). The chart sets `appArmorProfile: Unconfined` on the
  executor; Docker Desktop's kernel has no AppArmor, so whether a node
  honours that field is unproven.
- scheduling and storage across several nodes
- amd64 nodes
- real DNS and TLS

A node with cgroup v1 or a host cgroup namespace fails closed: the executor
refuses to start rather than running without a sandbox. The first install on
a managed cluster is the real test; if you make it, please open an issue with
how it went.

## Trying it on Docker Desktop (evaluation only)

`deploy/helm/evaluation/` in the repository sets up everything above on
Docker Desktop's Kubernetes, so you can try the chart without your own
services: single-pod Postgres, Temporal, Dex, Valkey and MinIO, ingress-nginx,
a local CA, fresh keys and the two Secrets. It is for evaluation only: one
replica of everything, no backups, no persistence you should trust.

From a checkout, with Docker Desktop's Kubernetes on and selected:

```sh
./deploy/helm/evaluation/setup.sh
helm install openplan deploy/helm/openplan -n openplan -f deploy/helm/evaluation/values.yaml \
  --set image.tag=main
```

A chart from a checkout carries a placeholder version (`0.0.0-dev`) with no
images behind it, so set `image.tag`: `main` for the latest build, or the
release you checked out without its `v`, such as `1.0.0`.

Then open `https://openplan.localhost` and sign in as
`admin@openplan.local` / `admin-local-only`. `setup.sh` prints how to trust
the local CA on macOS, so the browser stops warning. The harness runs the
chart's defaults, which fit in Docker Desktop's VM of about 8 GiB: one run at
a time, and a second run waits for the first. To remove everything:

```sh
./deploy/helm/evaluation/teardown.sh
```
