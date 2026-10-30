output "openplan_host" {
  description = "The host openplan's Ingress serves."
  value       = local.openplan_host
}

output "dex_issuer_url" {
  description = "Dex's issuer, for openplan's oidc.issuerURL."
  value       = local.issuer_url
}

output "oidc_client_id" {
  description = "openplan's client in Dex."
  value       = "openplan-api"
}

output "root_subject" {
  description = "The administrator's OIDC subject, for openplan's root_subject."
  value       = local.root_subject
}

output "ca_certificate" {
  description = "This environment's CA, to trust on the machine that opens openplan."
  value       = tls_self_signed_cert.ca.cert_pem
}
