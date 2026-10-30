# openplan on Kubernetes, deployed by openplan

A homelab example, not a supported topology. These OpenTofu modules are
registered as openplan templates, and an openplan instance uses them to stand
up another openplan on a Kubernetes cluster: its dependencies first, then
openplan itself. They're also openplan run against real infrastructure, so
what they need that openplan can't do yet is the point.

| Module | What it installs |
|---|---|
| [`dependencies/`](dependencies) | Postgres, Temporal, Dex, Valkey and MinIO, Traefik, a CA and TLS certificate per environment, and the DNS entry that lets openplan reach Dex |

The dependencies are the evaluation harness's (`deploy/helm/evaluation`) as
a module: single pods with no replication or backups inside the cluster.

## Registering and installing

Register each module as a template from `openplanhq/openplan` with its folder
as the root path, for example `examples/openplan-proxmox/dependencies`. One
stack is one environment (`local`, `prod`, `bench`) and installs the modules
in the order of the table above. Destroy them in reverse.

## State

Every module keeps its state in one S3-compatible bucket (the example uses
Cloudflare R2). openplan gives each installed template its own Terraform
workspace, so one bucket holds every stack. The backend settings that are
yours come from the stack's credentials, because a backend block can't read
variables.

## Credentials

Set these on the stack, not on an installed template. A credential's value
is a single line, so certificates are base64, as kubeconfig holds them
(`certificate-authority-data`, `client-certificate-data`, `client-key-data`).

| Credential | Sensitive | Value |
|---|---|---|
| `TF_CLI_ARGS_init` | no | `-backend-config=bucket=<bucket>` |
| `AWS_ENDPOINT_URL_S3` | no | `https://<account-id>.r2.cloudflarestorage.com` |
| `AWS_ACCESS_KEY_ID` | yes | the R2 token's access key |
| `AWS_SECRET_ACCESS_KEY` | yes | the R2 token's secret |
| `TF_VAR_kube_host` | no | the cluster's API server URL |
| `TF_VAR_kube_cluster_ca_data` | no | the cluster CA, base64 |
| `TF_VAR_kube_client_certificate_data` | no | a cluster-admin client certificate, base64 |
| `TF_VAR_kube_client_key_data` | yes | its key, base64 |
| `TF_VAR_kube_tls_server_name` | no | only when the API's address isn't in its certificate |
| `TF_VAR_dex_admin_password_hash` | yes | a bcrypt hash of the administrator's password |

Make the hash with:

```bash
htpasswd -BnC 10 admin | cut -d: -f2
```

## dependencies

| Variable | Default | |
|---|---|---|
| `environment` | | names the hosts: `openplan.<environment>.<domain>`, `dex.<environment>.<domain>` |
| `domain` | `home.arpa` | |
| `namespace` | `openplan` | |
| `dns_mode` | `k3s` | `k3s` writes k3s's `coredns-custom`; `none` leaves CoreDNS alone |
| `dex_admin_email` | | the administrator's sign-in email |

Its outputs are what the machine you open openplan from needs, and what
openplan's own module takes: `openplan_host`, `dex_issuer_url`,
`oidc_client_id`, `root_subject`, and `ca_certificate`.

After an apply, on the machine you'll open openplan from:

- trust `ca_certificate` (macOS:
  `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ca.pem`);
- point both hosts at Traefik in `/etc/hosts`: the cluster's node address,
  or `127.0.0.1` on Docker Desktop.

### On Docker Desktop

Tear the evaluation harness down first
(`deploy/helm/evaluation/teardown.sh`): its Traefik, namespace and CoreDNS
rewrite would collide with these.

- From inside openplan's executor the API server is
  `https://host.docker.internal:6443`, which isn't a name in its
  certificate, so also set `TF_VAR_kube_tls_server_name` to
  `kubernetes.docker.internal`. The certificates come from the
  `docker-desktop` user in `~/.kube/config`.
- Install with `dns_mode = "none"`. Docker Desktop's CoreDNS has no import
  hook, and writing its whole Corefile from a module would delete it on
  destroy, so add the rewrite once by hand, as `setup.sh` does (here for
  `environment = "local"`):

```bash
kubectl -n kube-system get configmap coredns -o jsonpath='{.data.Corefile}' \
  | awk '{ print } NR == 1 { print "    rewrite name exact dex.local.home.arpa traefik.traefik.svc.cluster.local answer auto" }' \
  > /tmp/Corefile
kubectl -n kube-system create configmap coredns --from-file=Corefile=/tmp/Corefile --dry-run=client -o yaml \
  | kubectl apply -f -
kubectl -n kube-system rollout restart deployment/coredns
```
