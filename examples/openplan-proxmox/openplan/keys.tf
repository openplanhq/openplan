# openplan's two keys. The credential key encrypts every credential openplan
# stores, so losing it loses them all: it lives in this module's state, in a
# versioned bucket, which is its backup. A new one is made only if this
# resource is replaced.
resource "random_bytes" "session_key" {
  length = 32
}

resource "random_bytes" "credential_key" {
  length = 32
}

resource "kubernetes_secret_v1" "keys" {
  metadata {
    name      = "openplan-keys"
    namespace = var.namespace
  }

  data = {
    session-encryption-key    = random_bytes.session_key.hex
    credential-encryption-key = random_bytes.credential_key.hex
  }
}
