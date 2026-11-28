output "address" {
  description = "The VM's address on the bridge."
  value       = var.address
}

output "ssh" {
  description = "How a session reaches the VM."
  value       = "ssh ${local.user}@${var.address}"
}

# sessions/ reads the rest from this stack's state.
output "user" {
  description = "The user the VM's sessions run as."
  value       = local.user
}

output "setup_repository" {
  description = "The repository holding the Claude setup, whose claude/repos/ entries sessions/ points each clone at."
  value       = var.setup_repository
}

output "automation_private_key" {
  description = "The key sessions/ connects to the VM with."
  value       = tls_private_key.automation.private_key_openssh
  sensitive   = true
}
