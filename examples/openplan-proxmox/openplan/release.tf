locals {
  openplan_host = "openplan.${var.environment}.${var.domain}"
}

# The chart from the checkout this template runs from: no release has shipped
# the chart yet, and this way the chart and the template are always one
# commit. The names below are the Secrets and Services the dependencies
# template makes, openplan's keys included: they encrypt what its Postgres
# holds, so they live and die with it, not with this release.
resource "helm_release" "openplan" {
  name      = "openplan"
  namespace = var.namespace
  chart     = "${path.module}/../../../deploy/helm/openplan"
  wait      = true
  timeout   = 600
  # One Secret per revision otherwise, forever.
  max_history = 10

  values = [yamlencode({
    image    = { tag = var.image_tag }
    temporal = { address = "temporal:7233" }
    postgres = { urlSecret = { name = "openplan-credentials", key = "database-url" } }
    valkey = {
      api      = { urlSecret = { name = "openplan-credentials", key = "valkey-api-url" } }
      executor = { urlSecret = { name = "openplan-credentials", key = "valkey-executor-url" } }
    }
    s3 = {
      bucket                = "openplan-artifacts"
      region                = "us-east-1"
      endpoint              = "http://minio:9000"
      forcePathStyle        = true
      accessKeyIDSecret     = { name = "openplan-dependencies", key = "minio-root-user" }
      secretAccessKeySecret = { name = "openplan-dependencies", key = "minio-root-password" }
    }
    oidc = {
      issuerURL          = "https://dex.${var.environment}.${var.domain}/dex"
      clientID           = "openplan-api"
      clientSecretSecret = { name = "openplan-dependencies", key = "oidc-client-secret" }
    }
    api = {
      rootSubject                   = var.root_subject
      sessionEncryptionKeySecret    = { name = "openplan-keys", key = "session-encryption-key" }
      credentialEncryptionKeySecret = { name = "openplan-keys", key = "credential-encryption-key" }
      trustedCASecret               = "openplan-trusted-ca"
    }
    executor = { replicas = var.executor_replicas }
    ingress = {
      className     = "traefik"
      host          = local.openplan_host
      tlsSecretName = "openplan-tls"
    }
  })]
}
