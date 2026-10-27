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
  the host you serve openplan on. The chart creates a standard Ingress with
  no controller-specific annotations, so any controller that serves one
  works. Don't start on ingress-nginx: Kubernetes retired it in March 2026,
  and it gets no more releases or security fixes.

### The services you bring

| Service | What openplan needs from it |
|---|---|
| Postgres | A database the api owns. The api migrates it at startup. |
| Temporal | A frontend address both the api and the executor reach, and a namespace (default `default`). |
| Dex | An OAuth client for openplan with redirect URL `https://<host>/v1/auth/callback`, at an HTTPS issuer URL that resolves the same way from the browser and from the api pod. Other identity providers connect through Dex. |
| S3 | A bucket for run logs and plans, with an access key. Any S3-compatible store works (`s3.endpoint`, `s3.forcePathStyle`). |
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

**2. Put the credentials in Secrets.** Every setting is a value (step 4)
except the credentials, because values are stored in the release and usually
committed to git. Values name each credential by the Secret and the key
holding it, so the Secrets can have any names, and a credential your cluster
already holds (a database password from an operator, say) can be used where it
is. For example:

```sh
kubectl -n openplan create secret generic openplan-credentials \
  --from-literal=postgres-password=<password> \
  --from-literal=valkey-api-password=<password> \
  --from-literal=valkey-executor-password=<password> \
  --from-literal=s3-access-key-id=<key id> \
  --from-literal=s3-secret-access-key=<secret key> \
  --from-literal=oidc-client-secret=<openplan's client secret in Dex> \
  --from-literal=session-encryption-key=$(openssl rand -hex 32) \
  --from-literal=credential-encryption-key=$(openssl rand -hex 32)
```

The executor reads only its Valkey password and the S3 key, whichever Secret
they're in. It never gets the database password, the encryption keys or the
OIDC secret. A password may contain any character: the chart passes the
Postgres and Valkey passwords apart from their URLs, and openplan escapes them
for you.

**3. Create the TLS Secret for your host.**

```sh
kubectl -n openplan create secret tls openplan-tls --cert=tls.crt --key=tls.key
```

If Dex's certificate comes from an internal CA, put that CA in a Secret too.
The api trusts it in addition to the public roots:

```sh
kubectl -n openplan create secret generic openplan-trusted-ca --from-file=ca.crt=internal-ca.crt
```

**4. Write a values file.** This one sets every required value; "Values"
below lists the rest.

```yaml
temporal:
  address: temporal-frontend.temporal:7233
postgres:
  url: postgres://openplan@postgres.example.internal:5432/openplan?sslmode=require
  passwordSecret: {name: openplan-credentials, key: postgres-password}
valkey:
  api:
    url: redis://api@valkey.example.internal:6379/0
    passwordSecret: {name: openplan-credentials, key: valkey-api-password}
  executor:
    url: redis://executor@valkey.example.internal:6379/0
    passwordSecret: {name: openplan-credentials, key: valkey-executor-password}
s3:
  bucket: openplan-artifacts
  region: us-east-1
  accessKeyIDSecret: {name: openplan-credentials, key: s3-access-key-id}
  secretAccessKeySecret: {name: openplan-credentials, key: s3-secret-access-key}
oidc:
  issuerURL: https://dex.example.com/dex
  clientID: openplan
  clientSecretSecret: {name: openplan-credentials, key: oidc-client-secret}
api:
  rootSubject: <the OIDC sub of the first administrator>
  sessionEncryptionKeySecret: {name: openplan-credentials, key: session-encryption-key}
  credentialEncryptionKeySecret: {name: openplan-credentials, key: credential-encryption-key}
  trustedCASecret: openplan-trusted-ca   # only for an internal CA
executor:
ingress:
  className: <your-ingress-class>   # kubectl get ingressclass; omit for the cluster default
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
`api.rootSubject`.

## Values

Every setting the chart passes to openplan is here. A key ending in `Secret`
names a credential as `{name, key}`: the Secret in the release namespace and
the key in it.

| Key | Default | What it does |
|---|---|---|
| `image.registry` | `ghcr.io/openplanhq` | Where the `openplan-api`, `openplan-executor` and `openplan-web` images come from. Point it at a mirror in a network without internet access. |
| `image.tag` | the chart's version | Override only to test a build that isn't a release. |
| `temporal.address` | required | The Temporal frontend, `host:port`. |
| `temporal.namespace` | `default` | The Temporal namespace openplan's workflows run in. |
| `postgres.url` | required | The api's database as a URL without the password. |
| `postgres.passwordSecret` | required | The database user's password. |
| `valkey.api.url` | required | Valkey as the `api` user, without the password: `redis://api@<host>:6379/0`, or `rediss://` for TLS. The database must be 0. |
| `valkey.api.passwordSecret` | required | The `api` user's password. |
| `valkey.executor.url` | required | The same Valkey as the `executor` user. |
| `valkey.executor.passwordSecret` | required | The `executor` user's password. |
| `s3.bucket`, `s3.region` | required | The bucket for run logs and plans. |
| `s3.endpoint` | AWS | The endpoint of an S3-compatible store other than AWS. |
| `s3.forcePathStyle` | `false` | Address the bucket by path, which most stores other than AWS need. |
| `s3.accessKeyIDSecret`, `s3.secretAccessKeySecret` | required | The bucket's access key. |
| `oidc.issuerURL` | required | Dex's issuer, `https://...`. |
| `oidc.clientID` | required | openplan's client ID in Dex. |
| `oidc.clientSecretSecret` | required | openplan's client secret in Dex. |
| `api.rootSubject` | required | The OIDC `sub` of the first administrator. |
| `api.sessionEncryptionKeySecret` | required | 32 random bytes as hex (`openssl rand -hex 32`) that seal sign-in sessions. |
| `api.credentialEncryptionKeySecret` | required | 32 random bytes as hex, different from the session key, that encrypt stored credentials. |
| `api.terraformTimeout` | openplan's default | The longest one Terraform command may run, as a duration such as `90m`. |
| `api.githubApp.id`, `api.githubApp.privateKeySecret` | none | A GitHub App that clones private template repositories. Set both or neither. |
| `api.trustedCASecret` | none | A Secret whose `ca.crt` the api trusts, for Dex behind an internal CA. |
| `api.resources` | 2 CPU, 2Gi, requests equal to limits | Requests and limits for the api container. Override requests and limits together. |
| `executor.replicas` | `1` | Executors. Each runs `executor.maxSessions` runs at once, one by default, so this is how many runs go at once. |
| `executor.resources` | 2 CPU, 2Gi, requests equal to limits | Requests and limits for the executor container. Override requests and limits together; see "Sizing the executor". |
| `executor.maxSessions` | `1` | Runs at once on one executor, each in its own sandbox. |
| `executor.sandbox.memoryMiB` | `1536` | Memory per sandbox, in MiB. See "Sizing the executor" before you raise it. |
| `executor.sandbox.cpus`, `executor.sandbox.pids` | `2`, `1024` | CPUs and processes per sandbox. |
| `executor.sandbox.subnet` | `10.237.0.0/24` | The IPv4 subnet the sandboxes' addresses come from, `/29` or larger. Move it if it collides with your pod or service CIDR. |
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
locked. Raise `executor.sandbox.memoryMiB`, and raise the executor's memory
with it, so that memory is at least `executor.maxSessions` ×
`executor.sandbox.memoryMiB` + 512Mi. Set requests and limits together: Helm
merges the map, so setting only `limits.memory` keeps the 2Gi request and the
pod is no longer `Guaranteed`. For 3072 MiB per run:

```yaml
executor:
  sandbox:
    memoryMiB: 3072
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
  Dex's discovery document at startup, and its log names what failed. If Dex can't be reached, check that `oidc.issuerURL` resolves from
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
services: single-pod Postgres, Temporal, Dex, Valkey and MinIO, Traefik,
a local CA, fresh keys and the credentials Secret. It is for evaluation
only: one replica of everything, no backups, no persistence you should trust.

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
