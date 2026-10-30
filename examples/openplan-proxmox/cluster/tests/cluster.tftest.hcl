# bpg checks that a file ID looks like Proxmox's storage:content/file.
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
# The cluster's API isn't there in a test; pretend it answered.
mock_provider "http" {
  mock_data "http" {
    defaults = { status_code = 200 }
  }
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

run "one_vm_per_address" {
  assert {
    condition     = length(proxmox_virtual_environment_vm.node) == 2
    error_message = "expected one VM per address"
  }
}

run "names_carry_environment" {
  assert {
    condition     = [for vm in proxmox_virtual_environment_vm.node : vm.name] == ["openplan-prod-0", "openplan-prod-1"]
    error_message = "VM names must be openplan-<environment>-<index>"
  }

  assert {
    condition     = strcontains(proxmox_download_file.image.file_name, "prod")
    error_message = "the image's file name must carry the environment, or two environments share and fight over it"
  }

  assert {
    condition     = alltrue([for f in proxmox_virtual_environment_file.cloud_init : strcontains(f.source_raw[0].file_name, "prod")])
    error_message = "every cloud-init snippet's file name must carry the environment"
  }
}

run "ballooning_off_and_pinned" {
  assert {
    condition = alltrue([
      for vm in proxmox_virtual_environment_vm.node :
      vm.memory[0].floating == 0 && vm.memory[0].dedicated == 6144 && vm.cpu[0].affinity == "0-2,6-8"
    ])
    error_message = "every VM needs fixed memory (no ballooning) and the environment's CPU affinity"
  }
}

run "only_the_server_gets_the_cas" {
  assert {
    condition     = strcontains(proxmox_virtual_environment_file.cloud_init[0].source_raw[0].data, "/var/lib/rancher/k3s/server/tls/client-ca.key")
    error_message = "the server's cloud-init must place the client CA before k3s first starts"
  }

  assert {
    condition     = !strcontains(proxmox_virtual_environment_file.cloud_init[1].source_raw[0].data, "BEGIN")
    error_message = "an agent's cloud-init must carry no key or certificate"
  }

  assert {
    condition     = strcontains(proxmox_virtual_environment_file.cloud_init[0].source_raw[0].data, "--disable traefik")
    error_message = "k3s's own Traefik must be off: the dependencies template installs its own"
  }
}

run "api_url_is_first_address" {
  assert {
    condition     = output.api_url == "https://10.10.10.10:6443"
    error_message = "api_url is ${output.api_url}"
  }
}

run "admin_is_cluster_admin" {
  assert {
    condition     = tls_cert_request.admin.subject[0].organization == "system:masters" && tls_cert_request.admin.subject[0].common_name == "openplan-admin"
    error_message = "the admin certificate must be in system:masters"
  }
}

run "address_outside_network_fails" {
  command = plan

  variables {
    addresses = ["192.168.29.50", "10.10.10.11"]
  }

  expect_failures = [var.addresses]
}

run "one_address_fails" {
  command = plan

  variables {
    addresses = ["10.10.10.10"]
  }

  expect_failures = [var.addresses]
}

run "bad_environment_fails" {
  command = plan

  variables {
    environment = "Prod"
  }

  expect_failures = [var.environment]
}

run "missing_ssh_key_fails" {
  command = plan

  variables {
    proxmox_ssh_private_key_data = null
  }

  expect_failures = [var.proxmox_ssh_private_key_data]
}

run "instance_id_fixed_per_node" {
  # Proxmox derives cloud-init's instance-id from the content unless one is
  # given, so any later change would make a reboot re-run cloud-init.
  assert {
    condition = alltrue([
      for i, f in proxmox_virtual_environment_file.meta_data :
      yamldecode(f.source_raw[0].data)["instance-id"] == "openplan-prod-${i}"
    ])
    error_message = "every node needs a fixed instance-id"
  }

  assert {
    condition     = length(proxmox_virtual_environment_file.meta_data) == 2
    error_message = "expected one meta-data snippet per node"
  }
}

run "affinity_optional" {
  # Proxmox lets only root@pam set affinity, so the template must work
  # without it.
  variables {
    cpu_affinity = null
  }

  assert {
    condition     = alltrue([for vm in proxmox_virtual_environment_vm.node : vm.cpu[0].affinity == null])
    error_message = "no affinity must be set when cpu_affinity is null"
  }
}

run "duplicate_address_fails" {
  command = plan

  variables {
    addresses = ["10.10.10.10", "10.10.10.10"]
  }

  expect_failures = [var.addresses]
}

run "gateway_address_fails" {
  command = plan

  variables {
    addresses = ["10.10.10.1", "10.10.10.11"]
  }

  expect_failures = [var.addresses]
}

run "network_address_fails" {
  command = plan

  variables {
    addresses = ["10.10.10.0", "10.10.10.11"]
  }

  expect_failures = [var.addresses]
}

run "broadcast_address_fails" {
  command = plan

  variables {
    addresses = ["10.10.10.10", "10.10.10.255"]
  }

  expect_failures = [var.addresses]
}

