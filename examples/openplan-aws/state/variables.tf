variable "region" {
  description = "Region of the state bucket. Must match the region in every module's backend block. Need not match where openplan runs."
  type        = string
  default     = "eu-central-1"
}

variable "name" {
  description = "Prefix for the bucket name and the Project tag."
  type        = string
  default     = "openplan"
}
