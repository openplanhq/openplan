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
| `EXECUTOR_MAX_SESSIONS` | Optional; runs at once on one executor, default 20 |
| `EXECUTOR_SANDBOX_MEMORY` | Optional; MiB per sandbox, default 2048 |
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
  resources:
    limits:
      memory: 41Gi   # see "Sizing the executor"
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
```

**6. Sign in** at `https://<host>` as the user whose `sub` you set in
`OPENPLAN_ROOT_SUBJECT`.

## Values

| Key | Default | What it does |
|---|---|---|
| `image.registry` | `ghcr.io/openplanhq` | Where the `openplan-api`, `openplan-executor` and `openplan-web` images come from. Point it at a mirror in a network without internet access. |
| `image.tag` | the chart's version | Override only to test a build that isn't a release. |
| `api.existingSecret` | required | The api's Secret. |
| `api.trustedCASecret` | none | A Secret whose `ca.crt` the api trusts, for Dex behind an internal CA. |
| `executor.existingSecret` | required | The executor's Secret. |
| `executor.replicas` | `1` | Executors. Each runs up to `EXECUTOR_MAX_SESSIONS` runs at once. |
| `executor.resources` | none | Requests and limits for the executor container. |
| `executor.runRoot.sizeLimit` | `8Gi` | Disk for the workspaces of the runs in flight on one executor. |
| `ingress.className` | the cluster default | The IngressClass. |
| `ingress.host` | required | The host openplan is served on. The api's public URL is `https://<host>`. |
| `ingress.tlsSecretName` | required | The `kubernetes.io/tls` Secret for the host. |

A required value that is missing fails `helm install` with a message naming
the key.

## Sizing the executor

Every sandbox's memory counts against the executor pod. Set the executor's
memory limit to at least `EXECUTOR_SANDBOX_MEMORY` × `EXECUTOR_MAX_SESSIONS`,
plus about 1 GiB for the executor itself. The defaults, 2048 MiB × 20
sessions, need 40 GiB of runs, so a limit around 41 GiB. To fit a smaller
node, lower `EXECUTOR_MAX_SESSIONS` in the executor's Secret, then the limit.

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
helm install openplan deploy/helm/openplan -n openplan -f deploy/helm/evaluation/values.yaml
```

Then open `https://openplan.localhost` and sign in as
`admin@openplan.local` / `admin-local-only`. `setup.sh` prints how to trust
the local CA on macOS, so the browser stops warning. Docker Desktop's VM has
about 8 GiB, so the harness runs two sessions at once. To remove everything:

```sh
./deploy/helm/evaluation/teardown.sh
```
