locals {
  prefix = split("/", var.network_cidr)[1]
  user   = "debian"

  # Built in HCL and encoded rather than templated as YAML, so no value, a
  # token or a script, can break the file.
  cloud_config = {
    hostname = var.name
    users = [{
      name                = local.user
      groups              = ["sudo"]
      shell               = "/bin/bash"
      sudo                = "ALL=(ALL) NOPASSWD:ALL"
      ssh_authorized_keys = [var.ssh_authorized_key]
    }]
    package_update = true
    packages       = ["qemu-guest-agent"]
    runcmd         = ["systemctl enable --now qemu-guest-agent"]
  }
  user_data = "#cloud-config\n${yamlencode(local.cloud_config)}"
}

# Downloaded once and never again: overwrite off, so a newer image upstream
# doesn't replace the disk the VM was imported from. The name is in the file
# name, so this stack and the cluster's never share or delete each other's.
resource "proxmox_download_file" "image" {
  node_name    = var.node_name
  datastore_id = var.image_datastore
  content_type = "import"
  url          = var.image_url
  file_name    = "${var.name}-debian-13.qcow2"
  overwrite    = false
  # A file left by a destroy that failed halfway is taken over, not refused.
  overwrite_unmanaged = true
}

# Uploaded over SSH: Proxmox's API takes no snippets. It holds the VM's
# tokens, so it's readable by root only.
#
# Cloud-init runs once, at the VM's first boot, so this only ever matters to a
# new VM. A change to it must not touch the running one: a new file gets a new
# ID, and user_data_file_id forces the VM's replacement, which would take any
# unpushed work with it. So a rotated token or a changed setup reaches a new VM
# only; rebuilding is a destroy and an apply.
resource "proxmox_virtual_environment_file" "user_data" {
  node_name    = var.node_name
  datastore_id = var.snippets_datastore
  content_type = "snippets"
  file_mode    = "0600"

  lifecycle {
    ignore_changes = [source_raw]
  }

  source_raw {
    file_name = "${var.name}-user-data.yaml"
    data      = local.user_data
  }
}

# A fixed instance-id. Without one, Proxmox derives it from the cloud-init
# content, so after any change the next boot would look like a new machine to
# cloud-init and re-run it over the VM's work. It must be there from the first
# apply: adding meta-data later replaces the VM.
resource "proxmox_virtual_environment_file" "meta_data" {
  node_name    = var.node_name
  datastore_id = var.snippets_datastore
  content_type = "snippets"

  source_raw {
    file_name = "${var.name}-meta-data.yaml"
    data = yamlencode({
      "instance-id"    = var.name
      "local-hostname" = var.name
    })
  }
}

resource "proxmox_virtual_environment_vm" "claude" {
  name      = var.name
  node_name = var.node_name
  tags      = ["claude"]
  on_boot   = true
  started   = true
  # A destroy is a rebuild; don't wait out a graceful stop.
  stop_on_destroy = true

  # A hung agent would otherwise stall every refresh for bpg's 15 minutes.
  agent {
    enabled = true
    timeout = "5m"
  }

  cpu {
    cores = var.cores
    type  = "host"
    units = var.cpu_units
  }

  # Ballooning off: a balloon would take memory back from under a build or a
  # test run.
  memory {
    dedicated = var.memory_mib
    floating  = 0
  }

  operating_system {
    type = "l26"
  }

  disk {
    datastore_id = var.vm_datastore
    import_from  = proxmox_download_file.image.id
    interface    = "virtio0"
    size         = var.disk_gib
    discard      = "on"
    iothread     = true
  }

  network_device {
    bridge = var.bridge
  }

  initialization {
    datastore_id      = var.vm_datastore
    user_data_file_id = proxmox_virtual_environment_file.user_data.id
    meta_data_file_id = proxmox_virtual_environment_file.meta_data.id

    ip_config {
      ipv4 {
        address = "${var.address}/${local.prefix}"
        gateway = var.gateway
      }
    }

    dns {
      servers = var.dns_servers
    }
  }
}
