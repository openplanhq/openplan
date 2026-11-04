# A cluster whose API doesn't answer must not block a plan or a destroy: the
# readiness check warns instead of failing.
mock_provider "proxmox" {
  mock_resource "proxmox_download_file" {
    defaults = { id = "local:import/openplan-prod-debian-13.qcow2" }
  }

  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/openplan-prod-0-user-data.yaml" }
  }
}

mock_provider "tls" {}
mock_provider "random" {}

mock_provider "http" {
  mock_data "http" {
    defaults = { status_code = 503 }
  }
}

variables {
  environment                  = "prod"
  node_name                    = "pve"
  addresses                    = ["10.10.10.10", "10.10.10.11"]
  k3s_version                  = "v1.36.5+k3s1"
  ssh_authorized_key           = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest owner"
  proxmox_ssh_private_key_data = "a2V5"
}

run "unready_api_does_not_fail_the_apply" {
  # A test counts a failed check as a failure unless it's expected; a real
  # plan or apply only warns, and the outputs below are still written.
  expect_failures = [check.api_ready]

  assert {
    condition     = output.api_url == "https://10.10.10.10:6443"
    error_message = "the apply should complete with the API down"
  }
}
