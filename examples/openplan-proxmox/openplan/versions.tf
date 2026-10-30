terraform {
  # 1.10 is the first with S3's own state lock (use_lockfile), which R2 needs
  # since it has no DynamoDB.
  required_version = ">= 1.10"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 3.4"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 3.3"
    }
  }
}
