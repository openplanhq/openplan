# The VPC, its egress and the DNS zone. Everything in op-ecs sits on top of
# this, and nothing here changes when openplan is upgraded.

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  # Two zones, because the ALB and RDS's subnet group both refuse fewer.
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.name }
}

# Public: the ALB and the NAT instance, nothing else.
resource "aws_subnet" "public" {
  count = length(local.azs)

  vpc_id            = aws_vpc.this.id
  availability_zone = local.azs[count.index]
  cidr_block        = cidrsubnet(var.vpc_cidr, 4, count.index)

  tags = { Name = "${var.name}-public-${local.azs[count.index]}" }
}

# Private: the ECS instance, every task and RDS. Tasks run in awsvpc mode, and
# an awsvpc task on EC2 never gets a public IP, so its egress has to be NAT.
resource "aws_subnet" "private" {
  count = length(local.azs)

  vpc_id            = aws_vpc.this.id
  availability_zone = local.azs[count.index]
  cidr_block        = cidrsubnet(var.vpc_cidr, 4, count.index + 8)

  tags = { Name = "${var.name}-private-${local.azs[count.index]}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.name}-public" }
}

resource "aws_route_table_association" "public" {
  count = length(aws_subnet.public)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block           = "0.0.0.0/0"
    network_interface_id = aws_instance.nat.primary_network_interface_id
  }

  tags = { Name = "${var.name}-private" }
}

resource "aws_route_table_association" "private" {
  count = length(aws_subnet.private)

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

# A NAT instance rather than a NAT gateway: ~$3 a month against ~$32 plus
# per-GB processing. The cost is one AZ and no managed failover, which a
# single-instance deployment doesn't have anyway.

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

resource "aws_security_group" "nat" {
  name_prefix = "${var.name}-nat-"
  description = "NAT instance: forwards anything from inside the VPC"
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "Traffic to forward"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_instance" "nat" {
  ami                         = data.aws_ssm_parameter.al2023_arm64.value
  instance_type               = var.nat_instance_type
  subnet_id                   = aws_subnet.public[0].id
  vpc_security_group_ids      = [aws_security_group.nat.id]
  associate_public_ip_address = true
  # It forwards packets that aren't addressed to it, which EC2 drops by
  # default.
  source_dest_check = false

  metadata_options {
    http_tokens = "required"
  }

  user_data = <<-EOT
    #!/bin/sh
    set -eu
    dnf install -y iptables-services
    echo 'net.ipv4.ip_forward = 1' > /etc/sysctl.d/90-nat.conf
    sysctl --system
    iface=$(ip -o route show default | awk '{print $5}')
    iptables -t nat -A POSTROUTING -o "$iface" -j MASQUERADE
    iptables-save > /etc/sysconfig/iptables
    systemctl enable --now iptables
  EOT

  # A new AMI is not a reason to replace the box every private subnet routes
  # through.
  lifecycle {
    ignore_changes = [ami]
  }

  tags = { Name = "${var.name}-nat" }
}

# The zone openplan is served from. A new zone answers nothing until the
# registrar for the domain delegates to its name servers (the
# name_servers output), and op-ecs's certificate waits on that delegation.
resource "aws_route53_zone" "this" {
  name = var.domain_name
}
