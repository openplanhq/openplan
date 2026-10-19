variable "region" {
  description = "Region of the state bucket. Need not match where openplan runs."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Prefix for the bucket name and the Project tag."
  type        = string
  default     = "openplan"
}
