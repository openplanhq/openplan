# The endpoint, API token, TLS check and SSH user come from the stack's
# credentials as the provider's own variables: PROXMOX_VE_ENDPOINT,
# PROXMOX_VE_API_TOKEN, PROXMOX_VE_INSECURE and PROXMOX_VE_SSH_USERNAME.
provider "proxmox" {
  # prod and bench are separate runs, so the provider's own lock can't stop
  # both taking Proxmox's next free VM ID at once.
  random_vm_ids = true

  ssh {
    agent       = false
    private_key = base64decode(var.proxmox_ssh_private_key_data)
  }
}
