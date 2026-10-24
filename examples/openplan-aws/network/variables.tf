variable "region" {
  description = "AWS region everything is created in."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Prefix for resource names and the Project tag."
  type        = string
  default     = "openplan"
}

variable "domain_name" {
  description = "The hosted zone to create. openplan is served at its apex, e.g. openplan.example.com."
  type        = string
}

variable "vpc_cidr" {
  description = "VPC address range. Split into two public and two private /20s."
  type        = string
  default     = "10.40.0.0/16"
}

variable "nat_instance_type" {
  description = "The NAT instance. Its bandwidth caps every executor download, so size up if provider installs crawl."
  type        = string
  default     = "t4g.nano"
}
