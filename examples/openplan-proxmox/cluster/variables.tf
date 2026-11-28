variable "environment" {
  description = "The environment this cluster is, such as prod or bench. It names the VMs and every file left on the host."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,15}$", var.environment))
    error_message = "environment must be lowercase letters, digits and hyphens, starting with a letter, at most 16 characters."
  }
}

variable "node_name" {
  description = "The Proxmox node the VMs run on."
  type        = string
}

variable "vm_datastore" {
  description = "Where the VMs' disks and cloud-init drives go."
  type        = string
  default     = "local-lvm"
}

variable "image_datastore" {
  description = "Where the downloaded cloud image goes; it needs the import content type."
  type        = string
  default     = "local"
}

variable "snippets_datastore" {
  description = "Where the cloud-init files go; it needs the snippets content type."
  type        = string
  default     = "local"
}

variable "image_url" {
  description = "The Debian cloud image the VMs boot from. Downloaded once: a newer upstream image doesn't replace running VMs."
  type        = string
  default     = "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2"
}

variable "bridge" {
  description = "The bridge the VMs' one NIC joins."
  type        = string
  default     = "vmbr1"
}

variable "network_cidr" {
  description = "The bridge's network."
  type        = string
  default     = "10.10.10.0/24"
}

variable "gateway" {
  description = "The bridge's gateway, the Proxmox host on a NAT bridge."
  type        = string
  default     = "10.10.10.1"
}

variable "dns_servers" {
  description = "The VMs' DNS servers."
  type        = list(string)
  default     = ["1.1.1.1", "9.9.9.9"]
}

variable "addresses" {
  description = "One address per node, without a prefix. The first is the k3s server; the rest are agents."
  type        = list(string)

  validation {
    condition     = length(var.addresses) >= 2
    error_message = "A cluster needs at least two nodes: a server and an agent."
  }

  validation {
    condition = alltrue([
      for address in var.addresses :
      can(cidrhost("${address}/${split("/", var.network_cidr)[1]}", 0)) &&
      try(cidrhost("${address}/${split("/", var.network_cidr)[1]}", 0), "") == cidrhost(var.network_cidr, 0)
    ])
    error_message = "Every address must be inside network_cidr."
  }

  validation {
    condition     = length(distinct(var.addresses)) == length(var.addresses)
    error_message = "Two nodes can't share an address."
  }

  # The network's own address, its broadcast address and the gateway are
  # taken; a node given one never comes up on the network.
  validation {
    condition = length(setintersection(toset(var.addresses), toset([
      cidrhost(var.network_cidr, 0),
      cidrhost(var.network_cidr, -1),
      var.gateway,
    ]))) == 0
    error_message = "No address may be the network's, its broadcast address or the gateway."
  }
}

variable "cores" {
  description = "vCPUs per node."
  type        = number
  default     = 4
}

variable "memory_mib" {
  description = "Memory per node, fixed: ballooning is off so a node never shrinks under its pods."
  type        = number
  default     = 6144
}

variable "disk_gib" {
  description = "Disk per node."
  type        = number
  default     = 40
}

variable "cpu_affinity" {
  description = "The host threads this environment's VMs run on, such as 0-2,6-8, so two environments never share a physical core. Proxmox lets only root@pam set it: empty leaves the VMs unpinned."
  type        = string
  default     = null
}

variable "cpu_units" {
  description = "This environment's CPU weight against the host's other VMs when they compete (cgroup v2 weight, Proxmox's default 100). Give prod more than bench, such as 200 and 50, so a benchmark never slows prod."
  type        = number
  default     = 100

  validation {
    condition     = var.cpu_units >= 1 && var.cpu_units <= 10000 && floor(var.cpu_units) == var.cpu_units
    error_message = "cpu_units must be a whole number from 1 to 10000."
  }
}

variable "k3s_version" {
  description = "The k3s release every node installs, such as v1.36.5+k3s1."
  type        = string
}

variable "ssh_authorized_key" {
  description = "A public key that can SSH into every node as debian, for when one needs a look."
  type        = string
}

# A credential, never stack config: set it as TF_VAR_proxmox_ssh_private_key_data.
# Base64, because a credential's value is one line and a key is several.
variable "proxmox_ssh_private_key_data" {
  description = "Base64 of the private key bpg uses to SSH into the Proxmox host, which it needs to upload cloud-init files. Credential TF_VAR_proxmox_ssh_private_key_data."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = var.proxmox_ssh_private_key_data != null && can(base64decode(var.proxmox_ssh_private_key_data))
    error_message = "Set the stack credential TF_VAR_proxmox_ssh_private_key_data to the base64 of the SSH key for the Proxmox host (base64 < key | tr -d '\\n')."
  }
}
