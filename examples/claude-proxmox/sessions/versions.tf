terraform {
  # 1.10 is the first with S3's own state lock (use_lockfile), which R2 needs
  # since it has no DynamoDB. terraform_data and terraform_remote_state are
  # built in, so there are no providers to lock.
  required_version = ">= 1.10"
}
