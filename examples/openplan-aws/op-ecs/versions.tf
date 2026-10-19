terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

# network/ is applied first, from the sibling directory, with local state.
# Switch both to a remote backend before more than one person applies this.
data "terraform_remote_state" "network" {
  backend = "local"

  config = {
    path = "${path.module}/../network/terraform.tfstate"
  }
}

provider "aws" {
  region = data.terraform_remote_state.network.outputs.region

  default_tags {
    tags = {
      Project = data.terraform_remote_state.network.outputs.name
    }
  }
}
