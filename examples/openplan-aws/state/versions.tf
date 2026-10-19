# The one module on local state: it creates the bucket every other module
# keeps its state in, so it can't keep its own there before it exists. Its
# state holds only the bucket, no secrets; if lost, `terraform import` the
# bucket back.
terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project = var.name
    }
  }
}
