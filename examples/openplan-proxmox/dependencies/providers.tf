provider "kubernetes" {
  host                   = var.kube_host
  cluster_ca_certificate = base64decode(var.kube_cluster_ca_data)
  client_certificate     = base64decode(var.kube_client_certificate_data)
  client_key             = base64decode(var.kube_client_key_data)
  tls_server_name        = var.kube_tls_server_name
}

provider "helm" {
  kubernetes = {
    host                   = var.kube_host
    cluster_ca_certificate = base64decode(var.kube_cluster_ca_data)
    client_certificate     = base64decode(var.kube_client_certificate_data)
    client_key             = base64decode(var.kube_client_key_data)
    tls_server_name        = var.kube_tls_server_name
  }
}

locals {
  openplan_host = "openplan.${var.environment}.${var.domain}"
  dex_host      = "dex.${var.environment}.${var.domain}"
  issuer_url    = "https://${local.dex_host}/dex"
}
