# openplan on Docker Desktop, kept on develop by Argo CD

A homelab setup, not a supported topology. It runs openplan on Docker
Desktop's Kubernetes and lets Argo CD keep it on openplan's `develop`: every
merge to `develop` becomes an upgrade of a running install with real stacks.
It's the openplan that applies [`examples/openplan-proxmox`](../../../examples/openplan-proxmox).

Argo CD installs, from git and GHCR:

| Application | What | From |
|---|---|---|
| `traefik` | the ingress controller | Traefik's chart, pinned |
| `cert-manager` | issues the TLS certificate | cert-manager's chart, pinned |
| `dependencies` | Postgres, Temporal, Dex, Valkey, MinIO | `deploy/docker-desktop/dependencies.yaml` on `develop` |
| `openplan` | the api, executor and web | the newest develop chart, `oci://ghcr.io/openplanhq/charts-develop/openplan`, with `deploy/docker-desktop/values.yaml` from `develop` |

The dependencies are the ones [`../setup.sh`](../setup.sh) installs: single pods with no replication
or backups. Nothing here is precious; stacks keep their Terraform state in
their own backend.

## Before you start

- Docker Desktop with Kubernetes on (kubeadm, one node), and `kubectl`'s
  context set to `docker-desktop`.
- `helm` and `mkcert` (`brew install helm mkcert`).
- The by-hand setup (`deploy/docker-desktop/setup.sh`) removed with
  `deploy/docker-desktop/teardown.sh`: it
  uses the same namespace, ingress and DNS entry, and the bootstrap refuses
  to run alongside it.

## Bring it up

```sh
deploy/docker-desktop/argocd/bootstrap.sh
```

It makes the parts that can't live in git, then hands over to Argo CD:

- **A mkcert root of its own,** at `~/.local/share/mkcert-openplan`, trusted
  by the Mac (macOS asks for your password once). cert-manager signs the
  certificate for `openplan.localhost` and `dex.localhost` with it, so the
  root's key sits in the cluster; keeping it apart from your everyday mkcert
  root means it can be untrusted on its own.
- **The Secrets** for the dependencies and openplan's keys. Argo CD renders
  charts without `lookup`, so passwords generated in a chart would change on
  every sync.
- **A CoreDNS rewrite,** so the api reaches Dex at `https://dex.localhost`,
  the same URL the browser uses.
- **Argo CD,** and the one root Application that points it at `apps/`.

Then open https://openplan.localhost once every Application is Synced and
Healthy (`kubectl -n argocd get applications -w`), and sign in as
`admin@openplan.local` / `admin-local-only`.

Argo CD's own UI is https://argocd.localhost, with the same certificate root,
so no warning. Sign in as `admin` with the password from

```sh
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d
```

Then change it under User Info and delete that Secret: Docker Desktop
publishes Traefik on every network interface, so the page is reachable from
your network too, and Argo CD's admin can change anything in the cluster. The
`argocd` CLI works through the same host with
`argocd login argocd.localhost --grpc-web`.

The root Application follows `develop`, so a change to this folder applies
once it merges. To try a branch, run the bootstrap with `ROOT_REVISION=<branch>`;
rerunning it with the default switches back.
Rerunning is always safe: the root, the Secrets and the certificate are kept.

## Argo CD's own settings

Argo CD manages its own install: [`apps/argocd.yaml`](apps/argocd.yaml)
renders the `argo-cd` chart with [`argocd-values.yaml`](argocd-values.yaml)
from `develop`, so a change to either reaches the install when it merges. The
bootstrap installs Argo CD only the first time, with the same file and chart
version, so rerunning it never changes Argo CD.

It never prunes its own install, because a bad render that dropped the CRDs
would delete every Application with them. A setting that removes a resource
leaves it behind until you delete it by hand.

If a bad change breaks Argo CD so it can't fix itself, revert the change on
`develop`, then put the settings back by hand with the version from
`apps/argocd.yaml`:

```sh
helm --kube-context docker-desktop upgrade argocd argo-cd \
  --repo https://argoproj.github.io/argo-helm --version <version> -n argocd \
  -f deploy/docker-desktop/argocd/argocd-values.yaml --force-conflicts
```

## Following develop

A merge to `develop` reaches the pods in three steps:

1. `publish.yml` builds that commit's images and pushes them as `sha-<commit>`.
2. Once they're published and verified, it pushes the chart
   `oci://ghcr.io/openplanhq/charts-develop/openplan` at version
   `0.0.0-develop.<run number>`, whose `appVersion` is `sha-<commit>`.
3. Argo CD checks GHCR every few minutes, sees a higher version, and rolls
   the pods onto that commit's images. They already exist, so the api is only
   down for its own restart.

What comes with that:

- **A failed build never reaches the install.** It publishes no chart, so the
  install stays on the last good build.
- **Values and dependencies lead the images.** `values.yaml` and
  `dependencies.yaml` apply as soon as they merge; the chart and images
  follow when the build finishes.
- **A run in flight is lost** when the executor is replaced.
- **To hold or roll back,** set the chart source's `targetRevision` in
  `apps/openplan.yaml` to one version, such as `0.0.0-develop.42`, and commit
  it where the root reads: on `develop`, or on a branch you bootstrap with
  `ROOT_REVISION`. An edit to the live Application is undone by the root's
  self-heal.

**Once, after the first develop build:** GHCR creates the
`charts-develop/openplan` package private, and Argo CD reads it without
credentials. Make it public in the package's settings on GitHub. Until then,
and before the first develop chart exists at all, the `openplan` Application
shows a ComparisonError about listing the chart's tags, and any pods already
running keep running.

A change to `dependencies.yaml` on `develop` that alters an init Job fails the
`dependencies` sync, since a Job can't be changed in place. Delete the Job and
Argo CD recreates it.

## Tear it down

```sh
deploy/docker-desktop/argocd/teardown.sh
```

It removes everything the bootstrap and Argo CD added, stacks and stored
credentials included, and leaves the cluster as it was. It's safe to run
twice, or after a bootstrap that stopped halfway. The dedicated mkcert root
stays trusted by the Mac, because removing it needs your password; the
script prints the command.
