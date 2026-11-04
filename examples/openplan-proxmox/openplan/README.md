# openplan

openplan itself, from its published Helm chart at one version, on a cluster
the `dependencies` template has set up: its values name the Secrets and
Services that template makes. The chart version picks the images too, since
each chart runs the build it was published with, so `chart_version` is the
one thing to change for an upgrade. The session and credential keys come
from `dependencies`, which holds the database they encrypt.

Install it after `dependencies`, on the same stack, and destroy it first.

## Variables

| Variable | Default | |
|---|---|---|
| `environment` | | as in `dependencies`: openplan is served at `https://openplan.<environment>.<domain>` |
| `chart_version` | | one exact chart version: a release (`0.5.0`) or a develop build (`0.0.0-develop.146`). Changing it upgrades openplan in place. Ranges are refused, since they move under a quiet stack |
| `chart_repository` | `oci://ghcr.io/openplanhq/charts` | the release charts; set `oci://ghcr.io/openplanhq/charts-develop` for develop builds |
| `root_subject` | | the `dependencies` template's `root_subject` output, shown in its apply log |
| `cluster_workspace`, `state_bucket` | | as in `dependencies`: read the connection from the cluster template's state |
| `domain` | `home.arpa` | as in `dependencies` |
| `namespace` | `openplan` | as in `dependencies` |
| `executor_replicas` | `1` | each executor runs one Terraform run at a time |

Without `cluster_workspace`, the connection comes from the same
`TF_VAR_kube_*` credentials as `dependencies`.

## Output

`url`, where openplan is served.
