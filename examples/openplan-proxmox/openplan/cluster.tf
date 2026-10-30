# Where the cluster connection comes from. With cluster_workspace set, it's the
# cluster template's outputs, read from its state in the same bucket: openplan
# can't hand one template's outputs to another, and the client key, a
# sensitive output, never shows in a log to be copied. Without it, it's the
# TF_VAR_kube_* credentials, for a cluster this example didn't make.
data "terraform_remote_state" "cluster" {
  count = var.cluster_workspace != null ? 1 : 0

  backend   = "s3"
  workspace = var.cluster_workspace

  # The cluster template's backend; the endpoint and keys come from the same
  # AWS_* credentials this stack's own state uses.
  config = {
    bucket                      = var.state_bucket
    key                         = "openplan-proxmox/cluster.tfstate"
    region                      = "auto"
    use_path_style              = true
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }
}

locals {
  cluster = one(data.terraform_remote_state.cluster[*].outputs)

  kube = local.cluster != null ? {
    host                   = local.cluster.api_url
    cluster_ca_certificate = base64decode(local.cluster.cluster_ca_data)
    client_certificate     = base64decode(local.cluster.client_certificate_data)
    # terraform_remote_state drops the sensitive mark on outputs it reads.
    client_key = sensitive(base64decode(local.cluster.client_key_data))
    } : {
    host                   = var.kube_host
    cluster_ca_certificate = base64decode(var.kube_cluster_ca_data)
    client_certificate     = base64decode(var.kube_client_certificate_data)
    client_key             = base64decode(var.kube_client_key_data)
  }
}
