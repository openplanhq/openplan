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
  # A file left by a destroy that failed halfway is taken over, not refused.
  overwrite_unmanaged = true
}

# Uploaded over SSH: Proxmox's API takes no snippets. The server's holds the
# cluster's CA keys on the host's disk, readable by root only.
#
# Cloud-init runs once, at a node's first boot, so these only ever matter to a
# new node. A change to them must not touch a running one: a new file gets a
# new ID, and the VM's user_data_file_id forces its replacement, which would
# take prod's nodes and their data with it. So a changed template or input
# (k3s_version, ssh_authorized_key) reaches new nodes only; upgrading k3s on
# running nodes is a separate step.
resource "proxmox_virtual_environment_file" "cloud_init" {
  count = length(var.addresses)

  node_name    = var.node_name
  datastore_id = var.snippets_datastore
  content_type = "snippets"
  file_mode    = "0600"

  lifecycle {
    ignore_changes = [source_raw]
  }

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

# A fixed instance-id per node. Without one, Proxmox derives it from the
# cloud-init content, so after any change the next boot would look like a new
# machine to cloud-init and re-run it: reinstall k3s at whatever version is
# set by then, and rewrite the CA files. It must be there from the first apply:
# adding meta-data later replaces the VM.
resource "proxmox_virtual_environment_file" "meta_data" {
  count = length(var.addresses)

  node_name    = var.node_name
  datastore_id = var.snippets_datastore
  content_type = "snippets"

  source_raw {
    file_name = "${local.names[count.index]}-meta-data.yaml"
    data = yamlencode({
      "instance-id"    = local.names[count.index]
      "local-hostname" = local.names[count.index]
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
  # Shut down rather than wait out a graceful stop: bench is destroyed after
  # every session.
  stop_on_destroy = true

  # A hung agent would otherwise stall every refresh for bpg's 15 minutes.
  agent {
    enabled = true
    timeout = "5m"
  }

  cpu {
    cores    = var.cores
    type     = "host"
    affinity = var.cpu_affinity
    units    = var.cpu_units
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
    meta_data_file_id = proxmox_virtual_environment_file.meta_data[count.index].id

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

# Whether the API answers, with the admin certificate this module made. A
# check, not a data source with a postcondition: a failed check warns, so a
# cluster that's down can still be planned and destroyed through openplan.
# The apply still waits through the retries before it says so.
check "api_ready" {
  data "http" "api_ready" {
    url                = "https://${local.server}:6443/readyz"
    ca_cert_pem        = tls_self_signed_cert.server_ca.cert_pem
    client_cert_pem    = tls_locally_signed_cert.admin.cert_pem
    client_key_pem     = tls_private_key.admin.private_key_pem
    request_timeout_ms = 5000

    retry {
      attempts     = 36
      min_delay_ms = 5000
      max_delay_ms = 10000
    }

    depends_on = [proxmox_virtual_environment_vm.node]
  }

  assert {
    condition     = data.http.api_ready.status_code == 200
    error_message = "k3s's API on ${local.server} doesn't answer /readyz; look at the server VM's console or its cloud-init log."
  }
}
