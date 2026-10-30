provider "kubernetes" {
  host                   = local.kube.host
  cluster_ca_certificate = local.kube.cluster_ca_certificate
  client_certificate     = local.kube.client_certificate
  client_key             = local.kube.client_key
  tls_server_name        = var.kube_tls_server_name
}

provider "helm" {
  kubernetes = {
    host                   = local.kube.host
    cluster_ca_certificate = local.kube.cluster_ca_certificate
    client_certificate     = local.kube.client_certificate
    client_key             = local.kube.client_key
    tls_server_name        = var.kube_tls_server_name
  }
}

locals {
  openplan_host = "openplan.${var.environment}.${var.domain}"
  dex_host      = "dex.${var.environment}.${var.domain}"
  issuer_url    = "https://${local.dex_host}/dex"
}
