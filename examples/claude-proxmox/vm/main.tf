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
    # make and Docker for openplan's `make check`, which runs everything else
    # in containers; the rest for the setup. docker.io only recommends its CLI
    # and buildx, and the executor's Dockerfile needs buildx for TARGETARCH.
    packages = ["qemu-guest-agent", "git", "gh", "jq", "curl", "tmux", "make", "docker.io", "docker-cli", "docker-buildx"]

    # The tokens are written after debian exists (defer), straight into its
    # home: a new parent directory would be root's.
    write_files = [
      {
        path        = "/usr/local/lib/claude-proxmox/setup.sh"
        permissions = "0755"
        content = templatefile("${path.module}/cloud-init/setup.sh.tftpl", {
          user             = local.user
          git_user_name    = var.git_user_name
          git_user_email   = var.git_user_email
          setup_repository = var.setup_repository
          repositories     = var.repositories
        })
      },
      {
        path        = "/home/${local.user}/.github-token"
        content     = var.github_token
        owner       = "${local.user}:${local.user}"
        permissions = "0600"
        defer       = true
      },
      {
        path        = "/home/${local.user}/.claude-env"
        content     = "export CLAUDE_CODE_OAUTH_TOKEN='${var.claude_code_oauth_token}'\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
        owner       = "${local.user}:${local.user}"
        permissions = "0600"
        defer       = true
      },
    ]

    # The setup runs as debian once it can use Docker and has a systemd
    # manager of its own, which bootstrap.sh's vault-sync timer needs; lingering
    # keeps that manager up with nobody logged in.
    runcmd = [
      "systemctl enable --now qemu-guest-agent",
      "usermod -aG docker ${local.user}",
      "loginctl enable-linger ${local.user}",
      "systemctl start user@$(id -u ${local.user}).service",
      "runuser -u ${local.user} -- env HOME=/home/${local.user} XDG_RUNTIME_DIR=/run/user/$(id -u ${local.user}) /usr/local/lib/claude-proxmox/setup.sh",
    ]
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
