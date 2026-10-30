# One CA per environment, trusted once on the operator's machine and by the
# api (openplan-trusted-ca), so it can reach Dex over HTTPS. mkcert's CA is
# not used: its key would have to be handed to openplan.
resource "tls_private_key" "ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "ca" {
  private_key_pem   = tls_private_key.ca.private_key_pem
  is_ca_certificate = true

  subject {
    common_name  = "openplan ${var.environment} CA"
    organization = "openplan"
  }

  validity_period_hours = 87600
  allowed_uses          = ["cert_signing", "crl_signing"]
}

resource "tls_private_key" "server" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_cert_request" "server" {
  private_key_pem = tls_private_key.server.private_key_pem
  dns_names       = [local.openplan_host, local.dex_host]

  subject {
    common_name = local.openplan_host
  }
}

resource "tls_locally_signed_cert" "server" {
  cert_request_pem   = tls_cert_request.server.cert_request_pem
  ca_private_key_pem = tls_private_key.ca.private_key_pem
  ca_cert_pem        = tls_self_signed_cert.ca.cert_pem

  # Under Apple's 825-day limit for TLS certificates, which macOS applies to
  # private CAs too; an apply inside the last 30 days issues a new one.
  validity_period_hours = 8760
  early_renewal_hours   = 720
  allowed_uses          = ["digital_signature", "key_encipherment", "server_auth"]
}

resource "kubernetes_secret_v1" "tls" {
  metadata {
    name      = "openplan-tls"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  type = "kubernetes.io/tls"
  data = {
    "tls.crt" = tls_locally_signed_cert.server.cert_pem
    "tls.key" = tls_private_key.server.private_key_pem
  }
}

resource "kubernetes_secret_v1" "trusted_ca" {
  metadata {
    name      = "openplan-trusted-ca"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  data = {
    "ca.crt" = tls_self_signed_cert.ca.cert_pem
  }
}
