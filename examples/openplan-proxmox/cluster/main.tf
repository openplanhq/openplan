locals {
  prefix = split("/", var.network_cidr)[1]
  server = var.addresses[0]
  names  = [for index, _ in var.addresses : "openplan-${var.environment}-${index}"]
}

# Downloaded once and never again: overwrite off, so a newer image upstream
# doesn't replace the disks the VMs were imported from. The environment is in
# the name, so two environments on one host never share or delete it.
resource "proxmox_download_file" "image" {
  node_name    = var.node_name
  datastore_id = var.image_datastore
  content_type = "import"
  url          = var.image_url
  file_name    = "openplan-${var.environment}-debian-13.qcow2"
  overwrite    = false
}

# Uploaded over SSH: Proxmox's API takes no snippets. The server's holds the
# cluster's CA keys on the host's disk, where the host's root already owns
# every VM anyway.
resource "proxmox_virtual_environment_file" "cloud_init" {
  count = length(var.addresses)

  node_name    = var.node_name
  datastore_id = var.snippets_datastore
  content_type = "snippets"

  source_raw {
    file_name = "${local.names[count.index]}-user-data.yaml"
    data = count.index == 0 ? templatefile("${path.module}/cloud-init/server.yaml.tftpl", {
      hostname           = local.names[count.index]
      ssh_authorized_key = var.ssh_authorized_key
      server_ca_cert     = tls_self_signed_cert.server_ca.cert_pem
      server_ca_key      = tls_private_key.server_ca.private_key_pem
      client_ca_cert     = tls_self_signed_cert.client_ca.cert_pem
      client_ca_key      = tls_private_key.client_ca.private_key_pem
      k3s_version        = var.k3s_version
      token              = random_password.join_token.result
      address            = var.addresses[count.index]
      }) : templatefile("${path.module}/cloud-init/agent.yaml.tftpl", {
      hostname           = local.names[count.index]
      ssh_authorized_key = var.ssh_authorized_key
      k3s_version        = var.k3s_version
      token              = random_password.join_token.result
      server             = local.server
      address            = var.addresses[count.index]
    })
  }
}

resource "proxmox_virtual_environment_vm" "node" {
  count = length(var.addresses)

  name      = local.names[count.index]
  node_name = var.node_name
  tags      = ["openplan", var.environment]
  on_boot   = true
  started   = true

  agent {
    enabled = true
  }

  cpu {
    cores    = var.cores
    type     = "host"
    affinity = var.cpu_affinity
  }

  # Ballooning off: Kubernetes schedules against the memory a node reports,
  # and a balloon would take it back from under the pods.
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
    user_data_file_id = proxmox_virtual_environment_file.cloud_init[count.index].id

    ip_config {
      ipv4 {
        address = "${var.addresses[count.index]}/${local.prefix}"
        gateway = var.gateway
      }
    }

    dns {
      servers = var.dns_servers
    }
  }
}

# The apply ends only once the API answers, with the admin certificate this
# module made, so a cluster that never came up fails here rather than in the
# next template.
data "http" "api_ready" {
  url             = "https://${local.server}:6443/readyz"
  ca_cert_pem     = tls_self_signed_cert.server_ca.cert_pem
  client_cert_pem = tls_locally_signed_cert.admin.cert_pem
  client_key_pem  = tls_private_key.admin.private_key_pem

  retry {
    attempts     = 60
    min_delay_ms = 5000
    max_delay_ms = 10000
  }

  depends_on = [proxmox_virtual_environment_vm.node]

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "k3s's API on ${local.server} never answered /readyz; look at the server VM's console or cloud-init log."
    }
  }
}
