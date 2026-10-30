resource "kubernetes_namespace_v1" "openplan" {
  metadata {
    name = var.namespace
    labels = {
      "app.kubernetes.io/part-of" = "openplan"
      # The executor runs gVisor as root with SYS_ADMIN, which every lower Pod
      # Security level refuses.
      "pod-security.kubernetes.io/enforce" = "privileged"
    }
  }
}

# Letters and digits only: the passwords go into URLs unescaped.
resource "random_password" "dependency" {
  for_each = toset([
    "postgres",
    "dex-db",
    "temporal-db",
    "valkey-api",
    "valkey-executor",
    "minio-root",
    "oidc-client",
  ])

  length  = 48
  special = false
}

locals {
  passwords = { for name, password in random_password.dependency : name => password.result }
}

# What the dependencies read, and the parts of it openplan's chart reads too
# (MinIO's keys, the OIDC client secret).
resource "kubernetes_secret_v1" "dependencies" {
  metadata {
    name      = "openplan-dependencies"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  data = {
    postgres-password        = local.passwords["postgres"]
    dex-db-password          = local.passwords["dex-db"]
    temporal-db-password     = local.passwords["temporal-db"]
    valkey-api-password      = local.passwords["valkey-api"]
    valkey-executor-password = local.passwords["valkey-executor"]
    minio-root-user          = "openplan"
    minio-root-password      = local.passwords["minio-root"]
    oidc-client-secret       = local.passwords["oidc-client"]
  }
}

# openplan's connection URLs, which carry the dependencies' passwords.
resource "kubernetes_secret_v1" "credentials" {
  metadata {
    name      = "openplan-credentials"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  data = {
    database-url        = "postgres://openplan:${local.passwords["postgres"]}@postgres:5432/openplan?sslmode=disable"
    valkey-api-url      = "redis://api:${local.passwords["valkey-api"]}@valkey:6379/0"
    valkey-executor-url = "redis://executor:${local.passwords["valkey-executor"]}@valkey:6379/0"
  }
}

locals {
  # A fixed userID, so the administrator's subject, and so root in openplan,
  # survives every apply. Derived rather than generated: a generated one is
  # unknown at the first plan, and kubernetes_manifest refuses any manifest
  # that isn't wholly known then. A subject isn't a secret.
  dex_admin_user_id = coalesce(var.dex_admin_user_id, uuidv5("url", "${local.issuer_url}/admin"))

  # The OIDC subject Dex issues a static user: base64 of the protobuf
  # {1: userID, 2: connector "local"}. A UUID is 36 bytes, so its length byte
  # is 0x24, "$".
  root_subject = base64encode(format("\n$%s\u0012\u0005local", local.dex_admin_user_id))

  dex_config = templatefile("${path.module}/manifests/dex-config.yaml.tftpl", {
    issuer_url    = local.issuer_url
    redirect_uri  = "https://${local.openplan_host}/v1/auth/callback"
    admin_email   = var.dex_admin_email
    admin_user_id = local.dex_admin_user_id
    # Base64, so Dex's environment expansion leaves the hash's "$" alone.
    admin_hash = base64encode(var.dex_admin_password_hash)
  })
}

# A Secret, not a ConfigMap: it holds the administrator's password hash.
resource "kubernetes_secret_v1" "dex_config" {
  metadata {
    name      = "dex-config"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  data = {
    "config.yaml" = local.dex_config
  }
}

# openplan's session and credential keys. The credential key encrypts every
# credential openplan stores in this template's Postgres, so it lives and dies
# with that database: here, not in the openplan template, whose release can
# be reinstalled. Its backup is this module's state, in a versioned bucket.
resource "random_bytes" "session_key" {
  length = 32
}

resource "random_bytes" "credential_key" {
  length = 32
}

resource "kubernetes_secret_v1" "keys" {
  metadata {
    name      = "openplan-keys"
    namespace = kubernetes_namespace_v1.openplan.metadata[0].name
  }

  data = {
    session-encryption-key    = random_bytes.session_key.hex
    credential-encryption-key = random_bytes.credential_key.hex
  }
}
