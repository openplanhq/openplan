# What network/ created, found by its tags rather than read from its state.
# That keeps op-ecs independent of where network keeps its state, and needs
# only Describe/List permissions, where reading network's state file would
# need access to everything in it.
#
# name and region must be the values network/ was applied with.

data "aws_vpc" "this" {
  tags = {
    Name    = var.name
    Project = var.name
  }
}

data "aws_subnets" "public" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.this.id]
  }

  tags = { Tier = "public" }
}

data "aws_subnets" "private" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.this.id]
  }

  tags = { Tier = "private" }
}

data "aws_route53_zone" "this" {
  name         = var.domain_name
  private_zone = false
}

locals {
  # The shape network/'s outputs had, so the rest of this module reads the
  # same either way. Sorted, because the lookups return IDs in no fixed order
  # and an unsorted list would show as a change on every plan.
  network = {
    name               = var.name
    region             = var.region
    domain_name        = var.domain_name
    vpc_id             = data.aws_vpc.this.id
    vpc_cidr           = data.aws_vpc.this.cidr_block
    public_subnet_ids  = sort(data.aws_subnets.public.ids)
    private_subnet_ids = sort(data.aws_subnets.private.ids)
    zone_id            = data.aws_route53_zone.this.zone_id
  }

  name   = local.network.name
  domain = local.network.domain_name
}
