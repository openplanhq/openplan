# openplan

openplan itself, from the Helm chart in this checkout
(`deploy/helm/openplan`), on a cluster the `dependencies` template has set
up: its values name the Secrets and Services that template makes. The chart
comes from the checkout because no release has shipped it yet, and so the
chart and this template are always one commit; the helm provider renders the
manifest at plan, so a change to the chart alone shows as an upgrade. The
session and credential keys come from `dependencies`, which holds the
database they encrypt.

Install it after `dependencies`, on the same stack, and destroy it first.

## Variables

| Variable | Default | |
|---|---|---|
| `environment` | | as in `dependencies`: openplan is served at `https://openplan.<environment>.<domain>` |
| `image_tag` | | one build: a release (`v0.5.0`) or a main build's full commit (`sha-<40 hex>`). Changing it upgrades openplan in place. Tags that move (`latest`, `main`, `v0`, `v0.5`) are refused |
| `root_subject` | | the `dependencies` template's `root_subject` output, shown in its apply log |
| `cluster_workspace`, `state_bucket` | | as in `dependencies`: read the connection from the cluster template's state |
| `domain` | `home.arpa` | as in `dependencies` |
| `namespace` | `openplan` | as in `dependencies` |
| `executor_replicas` | `1` | each executor runs one Terraform run at a time |

Without `cluster_workspace`, the connection comes from the same
`TF_VAR_kube_*` credentials as `dependencies`.

## Output

`url`, where openplan is served.
