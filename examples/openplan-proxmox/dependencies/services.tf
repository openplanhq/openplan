# The evaluation harness's dependencies (deploy/helm/evaluation), one
# kubernetes_manifest per document, so a change to one service is one
# resource's diff. Single pods with no replication: an example, not a
# production database.
#
# Everything rendered here must be known at plan: kubernetes_manifest refuses
# a manifest that isn't wholly known then, on the very first plan included.
locals {
  postgres_image = "postgres:16.10-alpine"

  manifest_files = ["postgres", "temporal", "dex", "valkey", "minio"]

  rendered = flatten([
    for file in local.manifest_files : [
      for document in split("\n---\n", templatefile("${path.module}/manifests/${file}.yaml.tftpl", {
        namespace      = var.namespace
        postgres_image = local.postgres_image
        dex_host       = local.dex_host
        # A hash reveals nothing of what it hashes.
        config_hash = nonsensitive(sha256(local.dex_config))
      })) : yamldecode(document)
    ]
  ])

  # A Job's pod template is immutable, so a Job whose spec changes has to be
  # a new Job: its name carries a hash of its spec, and a change replaces it.
  documents = [
    for m in local.rendered : m.kind != "Job" ? m : merge(m, {
      metadata = merge(m.metadata, { name = "${m.metadata.name}-${substr(sha256(jsonencode(m.spec)), 0, 8)}" })
    })
  ]

  manifests = { for m in local.documents : "${m.kind}/${m.metadata.name}" => m }
}

resource "kubernetes_manifest" "dependency" {
  for_each = local.manifests
  manifest = each.value

  # An apply that returns before the services are up would hand openplan's
  # template a cluster that can't start it.
  dynamic "wait" {
    for_each = each.value.kind == "Deployment" ? [1] : []
    content {
      rollout = true
    }
  }

  dynamic "wait" {
    for_each = each.value.kind == "Job" ? [1] : []
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
