provider "kubernetes" {
  host                   = local.kube.host
  cluster_ca_certificate = local.kube.cluster_ca_certificate
  client_certificate     = local.kube.client_certificate
  client_key             = local.kube.client_key
  tls_server_name        = var.kube_tls_server_name
}

provider "helm" {
  # The chart comes from this checkout and its version never changes, so
  # without a rendered manifest a change to the chart alone would plan as no
  # change and never reach the cluster. With it, the plan shows the upgrade's
  # manifest diff. The chart puts no secret in a manifest: only secretKeyRefs.
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
