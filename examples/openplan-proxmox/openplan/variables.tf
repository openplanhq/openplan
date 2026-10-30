variable "environment" {
  description = "The environment this stack is, such as prod or bench: openplan is served at openplan.<environment>.<domain>."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,15}$", var.environment))
    error_message = "environment must be lowercase letters, digits and hyphens, starting with a letter, at most 16 characters."
  }
}

variable "domain" {
  description = "The domain under which each environment's hosts live, as in the dependencies template."
  type        = string
  default     = "home.arpa"
}

variable "namespace" {
  description = "The namespace the dependencies template made."
  type        = string
  default     = "openplan"
}

variable "image_tag" {
  description = "The openplan images to run: a release (v0.5.0) or a main build (sha-<commit>). Changing it upgrades openplan in place."
  type        = string

  validation {
    condition     = var.image_tag != "" && var.image_tag != "latest" && var.image_tag != "main"
    error_message = "image_tag must name one build, a release or sha-<commit>: latest and main move, so an apply would run whatever they point at that day."
  }
}

variable "root_subject" {
  description = "The administrator's OIDC subject: the dependencies template's root_subject output."
  type        = string

  validation {
    condition     = var.root_subject != ""
    error_message = "root_subject is the dependencies template's root_subject output."
  }
}

variable "executor_replicas" {
  description = "Executors; each runs one Terraform run at a time."
  type        = number
  default     = 1
}

variable "cluster_workspace" {
  description = "The workspace of this stack's cluster template, shown in its run logs (\"Switched to workspace\"). Set, the connection comes from that template's state and no TF_VAR_kube_* credential is needed."
  type        = string
  default     = null
}

variable "state_bucket" {
  description = "The bucket the cluster template's state is in: the one in TF_CLI_ARGS_init."
  type        = string
  default     = null

  validation {
    condition     = var.cluster_workspace == null || var.state_bucket != null
    error_message = "state_bucket is needed to read the cluster template's state: the bucket in TF_CLI_ARGS_init."
  }
}

# The connection is credentials, never stack config:
# set them as stack credentials named TF_VAR_<variable>. The null default keeps
# them out of the stack's form, and the check turns a missing one into a plan
# error that names the credential. Certificates are base64, as kubeconfig
# holds them, because a credential's value is one line and a PEM is several.

variable "kube_host" {
  description = "The cluster's API server URL. Credential TF_VAR_kube_host."
  type        = string
  default     = null

  validation {
    condition     = var.cluster_workspace != null || var.kube_host != null
    error_message = "Set cluster_workspace to read the cluster template's state, or the stack credential TF_VAR_kube_host to the cluster's API server URL."
  }
}

variable "kube_cluster_ca_data" {
  description = "Base64 PEM of the CA that signed the API server's certificate, as kubeconfig's certificate-authority-data. Credential TF_VAR_kube_cluster_ca_data."
  type        = string
  default     = null

  validation {
    condition     = var.cluster_workspace != null || var.kube_cluster_ca_data != null
    error_message = "Set cluster_workspace to read the cluster template's state, or the stack credential TF_VAR_kube_cluster_ca_data to the cluster CA, base64 as in kubeconfig's certificate-authority-data."
  }
}

variable "kube_client_certificate_data" {
  description = "Base64 PEM of a cluster-admin client certificate, as kubeconfig's client-certificate-data. Credential TF_VAR_kube_client_certificate_data."
  type        = string
  default     = null

  validation {
    condition     = var.cluster_workspace != null || var.kube_client_certificate_data != null
    error_message = "Set cluster_workspace to read the cluster template's state, or the stack credential TF_VAR_kube_client_certificate_data to the admin client certificate, base64 as in kubeconfig's client-certificate-data."
  }
}

variable "kube_client_key_data" {
  description = "Base64 PEM of that certificate's key, as kubeconfig's client-key-data. Credential TF_VAR_kube_client_key_data."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = var.cluster_workspace != null || var.kube_client_key_data != null
    error_message = "Set cluster_workspace to read the cluster template's state, or the stack credential TF_VAR_kube_client_key_data to the admin client key, base64 as in kubeconfig's client-key-data."
  }
}

variable "kube_tls_server_name" {
  description = "The name to check the API server's certificate against, when kube_host's address isn't in it (Docker Desktop: kubernetes.docker.internal)."
  type        = string
  default     = null
}
