variable "name" {
  description = "The VM's name and hostname. It also names every file left on the host."
  type        = string
  default     = "claude"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,15}$", var.name))
    error_message = "name must be lowercase letters, digits and hyphens, starting with a letter, at most 16 characters."
  }
}

variable "node_name" {
  description = "The Proxmox node the VM runs on."
  type        = string
}

variable "vm_datastore" {
  description = "Where the VM's disk and cloud-init drive go."
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
  description = "The Debian cloud image the VM boots from. Downloaded once: a newer upstream image doesn't replace the VM."
  type        = string
  default     = "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2"
}

variable "bridge" {
  description = "The bridge the VM's one NIC joins."
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
  description = "The VM's DNS servers."
  type        = list(string)
  default     = ["1.1.1.1", "9.9.9.9"]
}

variable "address" {
  description = "The VM's address, without a prefix."
  type        = string

  validation {
    condition = (
      can(cidrhost("${var.address}/${split("/", var.network_cidr)[1]}", 0)) &&
      try(cidrhost("${var.address}/${split("/", var.network_cidr)[1]}", 0), "") == cidrhost(var.network_cidr, 0)
    )
    error_message = "address must be inside network_cidr."
  }

  # The network's own address, its broadcast address and the gateway are
  # taken; a VM given one never comes up on the network.
  validation {
    condition     = !contains([cidrhost(var.network_cidr, 0), cidrhost(var.network_cidr, -1), var.gateway], var.address)
    error_message = "address can't be the network's, its broadcast address or the gateway."
  }
}

variable "cores" {
  description = "vCPUs."
  type        = number
  default     = 4
}

variable "memory_mib" {
  description = "Memory, fixed: ballooning is off so a build never loses memory halfway."
  type        = number
  default     = 8192
}

variable "disk_gib" {
  description = "Disk, for the repos, Docker images and build caches."
  type        = number
  default     = 100
}

variable "cpu_units" {
  description = "The VM's CPU weight against the host's other VMs when they compete (cgroup v2 weight, Proxmox's default 100). Below the cluster's, so agents never slow it."
  type        = number
  default     = 50

  validation {
    condition     = var.cpu_units >= 1 && var.cpu_units <= 10000 && floor(var.cpu_units) == var.cpu_units
    error_message = "cpu_units must be a whole number from 1 to 10000."
  }
}

variable "git_user_name" {
  description = "The name on commits made from the VM."
  type        = string
}

variable "git_user_email" {
  description = "The email on commits made from the VM."
  type        = string
}

variable "setup_repository" {
  description = "The GitHub repository, as owner/name, that holds the Claude setup; the VM clones it and runs its bootstrap.sh."
  type        = string
  default     = "openplanhq/ai-os"

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.setup_repository))
    error_message = "setup_repository must be owner/name."
  }
}

# Stack config, not a credential: a device ID isn't secret. The VM shares the
# setup repo with this one device and no other.
variable "syncthing_peer" {
  description = "The laptop's Syncthing device ID (in its Syncthing: Actions, Show ID). It keeps the setup repository's git, and the VM's copy syncs with it."
  type        = string

  validation {
    condition     = can(regex("^[A-Z2-7]{7}(-[A-Z2-7]{7}){7}$", var.syncthing_peer))
    error_message = "syncthing_peer must be a Syncthing device ID: eight groups of seven letters A-Z and digits 2-7, joined by hyphens."
  }
}

# A credential, never stack config: TF_VAR_github_token. It's held to its own
# characters, so one pasted with a line ending is refused here rather than
# failing on the VM. Claude Code takes no token: Remote Control needs the full
# claude.ai login, which is done on the VM.
variable "github_token" {
  description = "A fine-grained GitHub token for the setup repository and the repos sessions/ clones, with read and write on contents, pull requests and issues. Credential TF_VAR_github_token."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = var.github_token != null && can(regex("^[A-Za-z0-9_]+$", var.github_token))
    error_message = "Set the stack credential TF_VAR_github_token to a GitHub token, with no trailing newline or space."
  }
}

variable "ssh_authorized_key" {
  description = "A public key that can SSH into the VM as debian, which is how sessions reach it."
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
