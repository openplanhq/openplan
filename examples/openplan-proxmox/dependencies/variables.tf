variable "environment" {
  description = "The environment this stack is, such as prod or bench. It names the hosts: openplan.<environment>.<domain>."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,15}$", var.environment))
    error_message = "environment must be lowercase letters, digits and hyphens, starting with a letter, at most 16 characters."
  }
}

variable "domain" {
  description = "The domain under which each environment's hosts live. home.arpa is reserved for home networks."
  type        = string
  default     = "home.arpa"
}

variable "namespace" {
  description = "The namespace openplan and its dependencies run in."
  type        = string
  default     = "openplan"
}

variable "dns_mode" {
  description = "How pods reach Dex at the browser's name: k3s writes k3s's coredns-custom ConfigMap; none leaves CoreDNS alone, for a cluster whose CoreDNS has no import hook (Docker Desktop), where the rewrite is made by hand."
  type        = string
  default     = "k3s"

  validation {
    condition     = contains(["k3s", "none"], var.dns_mode)
    error_message = "dns_mode must be k3s or none."
  }
}

variable "dex_admin_email" {
  description = "The email the first administrator signs in to Dex with."
  type        = string
}

variable "dex_admin_user_id" {
  description = "The administrator's Dex userID, which fixes their OIDC subject (root_subject). Empty derives a stable one from the issuer."
  type        = string
  default     = null

  validation {
    condition     = var.dex_admin_user_id == null || can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.dex_admin_user_id))
    error_message = "dex_admin_user_id must be a lowercase UUID."
  }
}

# The connection and the password hash are credentials, never stack config:
# set them as stack credentials named TF_VAR_<variable>. The null default keeps
# them out of the stack's form, and the check turns a missing one into a plan
# error that names the credential. Certificates are base64, as kubeconfig
# holds them, because a credential's value is one line and a PEM is several.

variable "dex_admin_password_hash" {
  description = "bcrypt hash of the administrator's Dex password. Credential TF_VAR_dex_admin_password_hash."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = var.dex_admin_password_hash != null
    error_message = "Set the stack credential TF_VAR_dex_admin_password_hash to a bcrypt hash of the administrator's password."
  }

  # A plaintext password would pass Dex's config load and crash-loop Dex, and
  # the apply would sit out its rollout timeout before saying so.
  validation {
    condition     = var.dex_admin_password_hash == null || can(regex("^\\$2[aby]\\$[0-9]{2}\\$[./A-Za-z0-9]{53}$", var.dex_admin_password_hash))
    error_message = "TF_VAR_dex_admin_password_hash must be a bcrypt hash, not the password: htpasswd -BnC 10 admin | cut -d: -f2"
  }
}

variable "kube_host" {
  description = "The cluster's API server URL. Credential TF_VAR_kube_host."
  type        = string
  default     = null

  validation {
    condition     = var.kube_host != null
    error_message = "Set the stack credential TF_VAR_kube_host to the cluster's API server URL."
  }
}

variable "kube_cluster_ca_data" {
  description = "Base64 PEM of the CA that signed the API server's certificate, as kubeconfig's certificate-authority-data. Credential TF_VAR_kube_cluster_ca_data."
  type        = string
  default     = null

  validation {
    condition     = var.kube_cluster_ca_data != null
    error_message = "Set the stack credential TF_VAR_kube_cluster_ca_data to the cluster CA, base64 as in kubeconfig's certificate-authority-data."
  }
}

variable "kube_client_certificate_data" {
  description = "Base64 PEM of a cluster-admin client certificate, as kubeconfig's client-certificate-data. Credential TF_VAR_kube_client_certificate_data."
  type        = string
  default     = null

  validation {
    condition     = var.kube_client_certificate_data != null
    error_message = "Set the stack credential TF_VAR_kube_client_certificate_data to the admin client certificate, base64 as in kubeconfig's client-certificate-data."
  }
}

variable "kube_client_key_data" {
  description = "Base64 PEM of that certificate's key, as kubeconfig's client-key-data. Credential TF_VAR_kube_client_key_data."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = var.kube_client_key_data != null
    error_message = "Set the stack credential TF_VAR_kube_client_key_data to the admin client key, base64 as in kubeconfig's client-key-data."
  }
}

variable "kube_tls_server_name" {
  description = "The name to check the API server's certificate against, when kube_host's address isn't in it (Docker Desktop: kubernetes.docker.internal)."
  type        = string
  default     = null
}
