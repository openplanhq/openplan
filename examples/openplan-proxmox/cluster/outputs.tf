# In the shapes the dependencies and openplan templates take: base64, as
# kubeconfig holds them. They read these from this module's state; the client
# key never has to be copied anywhere.
output "api_url" {
  description = "The cluster's API server."
  value       = "https://${local.server}:6443"
}

output "cluster_ca_data" {
  description = "Base64 of the CA that signed the API server's certificate."
  value       = base64encode(tls_self_signed_cert.server_ca.cert_pem)
}

output "client_certificate_data" {
  description = "Base64 of the cluster-admin client certificate."
  value       = base64encode(tls_locally_signed_cert.admin.cert_pem)
}

output "client_key_data" {
  description = "Base64 of the cluster-admin client key."
  value       = base64encode(tls_private_key.admin.private_key_pem)
  sensitive   = true
}

output "node_addresses" {
  description = "Every node's address, the server first."
  value       = var.addresses
}
