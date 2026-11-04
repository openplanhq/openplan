terraform {
  # Cloudflare R2 through the S3 backend. A backend can't read variables, and
  # the bucket and account endpoint are the operator's, so they come from the
  # stack's credentials: TF_CLI_ARGS_init="-backend-config=bucket=<name>",
  # AWS_ENDPOINT_URL_S3, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. openplan
  # gives every installed template its own workspace, so one bucket and this
  # one key hold every stack's state.
  backend "s3" {
    key            = "openplan-proxmox/cluster.tfstate"
    region         = "auto"
    use_path_style = true
    use_lockfile   = true

    # The checks below only make sense against AWS itself.
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }
}
