variable "region" {
  description = "Region network/ was applied in."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "The name network/ was applied with: its resources are found by it."
  type        = string
  default     = "openplan"
}

variable "domain_name" {
  description = "The zone network/ created. openplan is served at its apex."
  type        = string
}

variable "openplan_version" {
  description = "Release tag of the openplan images (ghcr.io/openplanhq/openplan-{api,executor,web}). Needs a release with the gVisor executor."
  type        = string
  default     = "v0.4.0"
}

variable "instance_type" {
  description = "The one ECS container instance every task runs on. ARM (Graviton): the images are multi-arch. It needs 3 ENIs (its own plus one per task), which every t4g size has."
  type        = string
  default     = "t4g.large"
}

variable "db_instance_class" {
  description = "RDS instance class. The smallest Graviton class is plenty for one install."
  type        = string
  default     = "db.t4g.micro"
}

variable "admin_email" {
  description = "Email of the static Dex user openplan's root grant is seeded for. Defaults to admin@<domain>."
  type        = string
  default     = null
}

variable "executor_max_sessions" {
  description = "Runs one executor holds at once. Each gets a sandbox of executor_sandbox_memory MiB, all inside the executor's own memory limit."
  type        = number
  default     = 2
}

variable "executor_sandbox_memory" {
  description = "MiB of memory per run sandbox."
  type        = number
  default     = 1536
}

variable "log_retention_days" {
  type    = number
  default = 14
}
