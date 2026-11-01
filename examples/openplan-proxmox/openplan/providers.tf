provider "kubernetes" {
  host                   = local.kube.host
  cluster_ca_certificate = local.kube.cluster_ca_certificate
  client_certificate     = local.kube.client_certificate
  client_key             = local.kube.client_key
  tls_server_name        = var.kube_tls_server_name
}

provider "helm" {
  # Without a rendered manifest, an upgrade's plan shows only the version
  # changing. With it, the plan shows what the new chart changes in the
  # cluster, which is what a reviewer approves. The chart puts no secret in a
  # manifest: only secretKeyRefs.
  experiments = {
    manifest = true
  }

  kubernetes = {
    host                   = local.kube.host
    cluster_ca_certificate = local.kube.cluster_ca_certificate
    client_certificate     = local.kube.client_certificate
    client_key             = local.kube.client_key
    tls_server_name        = var.kube_tls_server_name
  }
}
