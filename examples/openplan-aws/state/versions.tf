terraform {
  # 1.10 for S3 native state locking (use_lockfile), so no DynamoDB table.
  required_version = ">= 1.10"

  # This module creates the bucket its own state lives in, so the first apply
  # can't use it: Terraform refuses even to plan against an uninitialized
  # backend. The first apply runs under a backend_override.tf that switches to
  # local state, and the state then migrates into the bucket (README, step 1).
  # After that it's like the other two modules:
  # terraform init -backend-config=../backend.hcl
  backend "s3" {
    key = "state/terraform.tfstate"
  }

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
