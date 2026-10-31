# openplan on Docker Desktop, kept on main by Argo CD

A homelab example, not a supported topology. It runs openplan on Docker
Desktop's Kubernetes and lets Argo CD keep it on openplan's `main`: every
merge to `main` becomes an upgrade of a running install with real stacks.
It's the openplan that applies [`examples/openplan-proxmox`](../openplan-proxmox).

Argo CD installs, from git:

| Application | What | From |
|---|---|---|
| `traefik` | the ingress controller | Traefik's chart, pinned |
| `cert-manager` | issues the TLS certificate | cert-manager's chart, pinned |
| `dependencies` | Postgres, Temporal, Dex, Valkey, MinIO | `deploy/helm/evaluation/dependencies.yaml` on `main` |
| `openplan` | the api, executor and web | `deploy/helm/openplan` on `main`, images of the same commit |

The dependencies are the evaluation harness's: single pods with no replication
or backups. Nothing here is precious; stacks keep their Terraform state in
their own backend.

## Before you start

- Docker Desktop with Kubernetes on (kubeadm, one node), and `kubectl`'s
  context set to `docker-desktop`.
- `helm` and `mkcert` (`brew install helm mkcert`).
- The evaluation harness removed (`deploy/helm/evaluation/teardown.sh`): it
  uses the same namespace, ingress and DNS entry, and the bootstrap refuses
  to run alongside it.

## Bring it up

```sh
examples/openplan-docker-desktop/bootstrap.sh
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

The root Application follows `develop`, so a change to this folder applies
without waiting for a release. To try a branch, run the bootstrap with
`ROOT_REVISION=<branch>`; rerunning it with the default switches back.
Rerunning is always safe: the root, the Secrets and the certificate are kept.

## Following main

The `openplan` Application renders the chart at the commit `main` points to
and runs the images `publish.yml` pushed for that commit (`sha-<commit>`).
Argo CD checks git every few minutes, so a merge to `main` rolls the pods with
no hand step. Two things come with that:

- **The api is down until the new images exist.** It's replaced in one step,
  and Argo CD usually sees the merge before `publish.yml` has pushed the
  images. If a publish fails, the api stays down until the next good one.
- **A run in flight is lost** when the executor is replaced.

A change to `dependencies.yaml` on `main` that alters an init Job fails the
`dependencies` sync, since a Job can't be changed in place. Delete the Job and
Argo CD recreates it.

## Tear it down

```sh
examples/openplan-docker-desktop/teardown.sh
```

It removes everything the bootstrap and Argo CD added, stacks and stored
credentials included, and leaves the cluster as it was. It's safe to run
twice, or after a bootstrap that stopped halfway. The dedicated mkcert root
stays trusted by the Mac, because removing it needs your password; the
script prints the command.
