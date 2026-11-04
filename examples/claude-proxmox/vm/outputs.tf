output "address" {
  description = "The VM's address on the bridge."
  value       = var.address
}

output "ssh" {
  description = "How a session reaches the VM."
  value       = "ssh ${local.user}@${var.address}"
}
