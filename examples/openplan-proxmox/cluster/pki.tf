# The cluster's CAs and an admin certificate, made here rather than by k3s:
# a run has no ssh to fetch k3s's kubeconfig afterwards, and cloud-init returns
# nothing, so this is the only way a run ends up holding a way in. k3s uses CA
# files it finds in place before its first start and generates the rest.
resource "tls_private_key" "server_ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "server_ca" {
  private_key_pem   = tls_private_key.server_ca.private_key_pem
  is_ca_certificate = true

  subject {
    common_name = "openplan-${var.environment}-server-ca"
  }

  validity_period_hours = 87600
  allowed_uses          = ["cert_signing", "crl_signing", "digital_signature", "key_encipherment"]
}

resource "tls_private_key" "client_ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "client_ca" {
  private_key_pem   = tls_private_key.client_ca.private_key_pem
  is_ca_certificate = true

  subject {
    common_name = "openplan-${var.environment}-client-ca"
  }

  validity_period_hours = 87600
  allowed_uses          = ["cert_signing", "crl_signing", "digital_signature", "key_encipherment"]
}

resource "tls_private_key" "admin" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

# system:masters is bound to cluster-admin by Kubernetes itself, so a
# certificate signed by the client CA in that group needs no RBAC object.
resource "tls_cert_request" "admin" {
  private_key_pem = tls_private_key.admin.private_key_pem

  subject {
    common_name  = "openplan-admin"
    organization = "system:masters"
  }
}

resource "tls_locally_signed_cert" "admin" {
  cert_request_pem   = tls_cert_request.admin.cert_request_pem
  ca_private_key_pem = tls_private_key.client_ca.private_key_pem
  ca_cert_pem        = tls_self_signed_cert.client_ca.cert_pem

  validity_period_hours = 8760
  early_renewal_hours   = 720
  allowed_uses          = ["digital_signature", "key_encipherment", "client_auth"]
}

resource "random_password" "join_token" {
  length  = 48
  special = false
}
