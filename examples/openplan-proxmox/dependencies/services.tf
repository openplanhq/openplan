# The evaluation harness's dependencies (deploy/helm/evaluation), one
# kubernetes_manifest per document, so a change to one service is one
# resource's diff. Single pods with no replication: an example, not a
# production database.
locals {
  postgres_image = "postgres:16.10-alpine"

  manifest_files = ["postgres", "temporal", "dex", "valkey", "minio"]

  template_values = {
    namespace      = var.namespace
    postgres_image = local.postgres_image
    dex_host       = local.dex_host
    config_hash    = ""
  }

  # for_each keys must be known at plan and never sensitive. Dex's config hash
  # is neither on a first plan (it hashes a generated UUID and the password
  # hash), so the keys come from a render without it; the manifests applied
  # come from the full render.
  keyed_documents = flatten([
    for file in local.manifest_files : [
      for document in split("\n---\n", templatefile("${path.module}/manifests/${file}.yaml.tftpl", local.template_values)) : yamldecode(document)
    ]
  ])
  manifest_kinds = { for m in local.keyed_documents : "${m.kind}/${m.metadata.name}" => m.kind }

  applied_documents = flatten([
    for file in local.manifest_files : [
      for document in split("\n---\n", templatefile("${path.module}/manifests/${file}.yaml.tftpl", merge(local.template_values, {
        # A hash reveals nothing of what it hashes.
        config_hash = nonsensitive(sha256(kubernetes_secret_v1.dex_config.data["config.yaml"]))
      }))) : yamldecode(document)
    ]
  ])
  manifests = { for m in local.applied_documents : "${m.kind}/${m.metadata.name}" => m }
}

resource "kubernetes_manifest" "dependency" {
  for_each = local.manifest_kinds
  manifest = local.manifests[each.key]

  # An apply that returns before the services are up would hand openplan's
  # template a cluster that can't start it.
  dynamic "wait" {
    for_each = each.value == "Deployment" ? [1] : []
    content {
      rollout = true
    }
  }

  dynamic "wait" {
    for_each = each.value == "Job" ? [1] : []
    content {
      condition {
        type   = "Complete"
        status = "True"
      }
    }
  }

  timeouts {
    create = "10m"
    update = "10m"
  }

  depends_on = [
    kubernetes_namespace_v1.openplan,
    kubernetes_secret_v1.dependencies,
    kubernetes_secret_v1.dex_config,
    kubernetes_secret_v1.tls,
  ]
}
