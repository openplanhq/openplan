# The real tls and random providers, so the certificates are real multi-line
# PEMs: the cloud-init has to stay valid YAML and hand k3s each file intact.
mock_provider "proxmox" {
  mock_resource "proxmox_download_file" {
    defaults = { id = "local:import/openplan-prod-debian-13.qcow2" }
  }

  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/openplan-prod-0-user-data.yaml" }
  }
}

mock_provider "http" {}

override_data {
  target = data.http.api_ready
  values = { status_code = 200 }
}

variables {
  environment                  = "prod"
  node_name                    = "pve"
  addresses                    = ["10.10.10.10", "10.10.10.11"]
  cpu_affinity                 = "0-2,6-8"
  k3s_version                  = "v1.36.5+k3s1"
  ssh_authorized_key           = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest owner"
  proxmox_ssh_private_key_data = "a2V5"
}

run "server_files_intact" {
  assert {
    condition = {
      for f in yamldecode(proxmox_virtual_environment_file.cloud_init[0].source_raw[0].data).write_files :
      basename(f.path) => f.content
      } == {
      "server-ca.crt" = tls_self_signed_cert.server_ca.cert_pem
      "server-ca.key" = tls_private_key.server_ca.private_key_pem
      "client-ca.crt" = tls_self_signed_cert.client_ca.cert_pem
      "client-ca.key" = tls_private_key.client_ca.private_key_pem
    }
    error_message = "a CA file in the server's cloud-init doesn't round-trip to its PEM"
  }

  assert {
    condition     = startswith(proxmox_virtual_environment_file.cloud_init[0].source_raw[0].data, "#cloud-config\n")
    error_message = "cloud-init reads a user-data file as config only when its first line is #cloud-config"
  }
}

run "agent_joins_the_server" {
  assert {
    condition     = strcontains(join("\n", yamldecode(proxmox_virtual_environment_file.cloud_init[1].source_raw[0].data).runcmd), "K3S_URL='https://10.10.10.10:6443'")
    error_message = "the agent doesn't join the first address"
  }
}
