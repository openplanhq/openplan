# bpg checks that a file ID looks like Proxmox's storage:content/file.
mock_provider "proxmox" {
  mock_resource "proxmox_download_file" {
    defaults = { id = "local:import/claude-debian-13.qcow2" }
  }

  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/claude-user-data.yaml" }
  }
}

mock_provider "tls" {}

variables {
  node_name                    = "pve"
  address                      = "10.10.10.20"
  ssh_authorized_key           = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest owner"
  proxmox_ssh_private_key_data = "a2V5"
  git_user_name                = "Test Owner"
  git_user_email               = "owner@example.invalid"
  github_token                 = "github_pat_11TEST_abc"
  syncthing_peer               = "AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH"
}

run "vm_shape" {
  assert {
    condition     = proxmox_virtual_environment_vm.claude.name == "claude"
    error_message = "the VM must be named after var.name"
  }

  assert {
    condition     = proxmox_virtual_environment_vm.claude.cpu[0].cores == 4 && proxmox_virtual_environment_vm.claude.cpu[0].units == 50
    error_message = "4 cores at CPU weight 50, so the cluster wins when they compete"
  }

  assert {
    condition     = proxmox_virtual_environment_vm.claude.memory[0].dedicated == 8192 && proxmox_virtual_environment_vm.claude.memory[0].floating == 0
    error_message = "8 GiB, fixed"
  }

  assert {
    condition     = proxmox_virtual_environment_vm.claude.disk[0].size == 100
    error_message = "100 GiB of disk, for Docker images and build caches"
  }

  assert {
    condition     = proxmox_virtual_environment_vm.claude.initialization[0].ip_config[0].ipv4[0].address == "10.10.10.20/24"
    error_message = "the address takes network_cidr's prefix"
  }
}

run "files_carry_name" {
  assert {
    condition     = proxmox_download_file.image.file_name == "claude-debian-13.qcow2"
    error_message = "the image's file name must carry var.name, or two stacks on one host share and fight over it"
  }

  assert {
    condition     = proxmox_virtual_environment_file.user_data.source_raw[0].file_name == "claude-user-data.yaml"
    error_message = "the user-data snippet's file name must carry var.name"
  }

  assert {
    condition     = yamldecode(proxmox_virtual_environment_file.meta_data.source_raw[0].data)["instance-id"] == "claude"
    error_message = "a fixed instance-id, or cloud-init re-runs on the next boot after any change"
  }
}

run "user_data_is_cloud_config" {
  assert {
    condition     = startswith(proxmox_virtual_environment_file.user_data.source_raw[0].data, "#cloud-config\n")
    error_message = "cloud-init reads a user-data file as config only when its first line is #cloud-config"
  }

  assert {
    condition     = yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).users[0].ssh_authorized_keys[0] == var.ssh_authorized_key
    error_message = "the key must reach debian's authorized_keys intact"
  }
}

# sessions/ reaches the VM with the automation key and these outputs.
run "automation_key_authorized" {
  assert {
    condition = yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).users[0].ssh_authorized_keys == [
      var.ssh_authorized_key, trimspace(tls_private_key.automation.public_key_openssh),
    ]
    error_message = "debian must authorize the maintainer's key and the automation key, nothing else"
  }

  assert {
    condition     = output.user == "debian" && output.setup_repository == "openplanhq/ai-os"
    error_message = "sessions/ needs the VM's user and setup repository from its outputs"
  }
}

run "address_is_gateway" {
  command = plan

  variables {
    address = "10.10.10.1"
  }

  expect_failures = [var.address]
}

run "address_outside_network" {
  command = plan

  variables {
    address = "10.10.11.5"
  }

  expect_failures = [var.address]
}

run "address_is_broadcast" {
  command = plan

  variables {
    address = "10.10.10.255"
  }

  expect_failures = [var.address]
}
