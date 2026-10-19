terraform {
  # 1.10 for S3 native state locking (use_lockfile), so no DynamoDB table.
  required_version = ">= 1.10"

  # Bucket and locking come from ../backend.hcl, which state/ writes:
  # terraform init -backend-config=../backend.hcl
  backend "s3" {
    key    = "network/terraform.tfstate"
    region = "eu-central-1"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
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
