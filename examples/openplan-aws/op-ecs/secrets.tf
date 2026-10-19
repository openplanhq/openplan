# Every secret is generated here and handed to the tasks from SSM Parameter
# Store, so an apply needs no secret from the operator and no secret appears
# in a task definition. They do sit in Terraform state, which is why the state
# must be treated as a secret too.

# No special characters: these end up inside URLs (DATABASE_URL, VALKEY_URL),
# and a Valkey password is also one word of an ACL rule.
resource "random_password" "db" {
  for_each = toset(["admin", "openplan", "dex", "temporal"])

  length  = 32
  special = false
}

resource "random_password" "valkey" {
  for_each = toset(["api", "executor"])

  length  = 32
  special = false
}

resource "random_password" "oidc_client_secret" {
  length  = 40
  special = false
}

# The password of the static Dex user. Dex stores only its bcrypt hash; read
# the password once with `terraform output -raw admin_password`.
resource "random_password" "admin" {
  length  = 24
  special = false
}

# 32-byte keys, hex-encoded, the form the api expects. Losing the credential
# key makes every stored Terraform credential unreadable, so this resource
# must never be replaced on a live install.
resource "random_id" "credential_key" {
  byte_length = 32
}

resource "random_id" "session_key" {
  byte_length = 32
}

locals {
  db_host = aws_db_instance.this.address

  # 127.0.0.1: valkey shares the control task's network namespace with the
  # api. The executor reaches it through Cloud Map instead (tasks.tf).
  secrets = {
    db-admin-password    = random_password.db["admin"].result
    db-dex-password      = random_password.db["dex"].result
    db-temporal-password = random_password.db["temporal"].result
    db-openplan-password = random_password.db["openplan"].result
    database-url         = "postgres://openplan:${random_password.db["openplan"].result}@${local.db_host}:5432/openplan?sslmode=require"
    valkey-api-password  = random_password.valkey["api"].result
    valkey-exec-password = random_password.valkey["executor"].result
    valkey-api-url       = "redis://api:${random_password.valkey["api"].result}@127.0.0.1:6379/0"
    valkey-exec-url      = "redis://executor:${random_password.valkey["executor"].result}@${local.control_dns}:6379/0"
    oidc-client-secret   = random_password.oidc_client_secret.result
    credential-key       = random_id.credential_key.hex
    session-key          = random_id.session_key.hex
    s3-access-key-id     = aws_iam_access_key.artifacts.id
    s3-secret-access-key = aws_iam_access_key.artifacts.secret
  }
}

resource "aws_ssm_parameter" "secret" {
  for_each = local.secrets

  name  = "/${local.name}/${each.key}"
  type  = "SecureString"
  value = each.value
}
