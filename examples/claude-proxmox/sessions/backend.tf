terraform {
  # The same bucket as vm/, from the same credentials: TF_CLI_ARGS_init,
  # AWS_ENDPOINT_URL_S3, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
  backend "s3" {
    key            = "claude-proxmox/sessions.tfstate"
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
