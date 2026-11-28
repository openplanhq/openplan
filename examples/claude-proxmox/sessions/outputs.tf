output "services" {
  description = "Each repo's Remote Control service. The real unit name escapes the repo, as systemd-escape does on the VM."
  value       = { for repo in var.repositories : repo => "claude-rc@${repo}" }
}
