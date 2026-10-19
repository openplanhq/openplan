# One ECS cluster on one EC2 instance. Every task runs on it; there is no
# Fargate, because the executor needs capabilities, security options and
# sysctls that Fargate refuses (see README), and keeping everything on one
# instance is the cheapest layout ECS allows.

resource "aws_ecs_cluster" "this" {
  name = local.name
}

data "aws_ssm_parameter" "ecs_ami" {
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/arm64/recommended/image_id"
}

resource "aws_iam_role" "instance" {
  name_prefix = "${local.name}-instance-"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

# What the ECS agent needs, and Session Manager for a shell without SSH or an
# open port. No task can reach these credentials: IMDS is blocked for awsvpc
# tasks below.
resource "aws_iam_role_policy_attachment" "instance" {
  for_each = toset([
    "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role",
    "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
  ])

  role       = aws_iam_role.instance.name
  policy_arn = each.value
}

resource "aws_iam_instance_profile" "instance" {
  name_prefix = "${local.name}-instance-"
  role        = aws_iam_role.instance.name
}

resource "aws_security_group" "instance" {
  name_prefix = "${local.name}-instance-"
  description = "ECS container instance: outbound only"
  vpc_id      = local.network.vpc_id

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

resource "aws_launch_template" "instance" {
  name_prefix   = "${local.name}-"
  image_id      = data.aws_ssm_parameter.ecs_ami.value
  instance_type = var.instance_type

  iam_instance_profile {
    arn = aws_iam_instance_profile.instance.arn
  }

  vpc_security_group_ids = [aws_security_group.instance.id]

  # The executor runs hostile Terraform, and the instance role's credentials
  # are one HTTP request away on IMDS. Hop limit 1 keeps IMDS to processes on
  # the host itself; ECS_AWSVPC_BLOCK_IMDS below drops it for every awsvpc
  # task outright.
  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      volume_size = 40
      volume_type = "gp3"
      encrypted   = true
    }
  }

  # ECS_SELINUX_CAPABLE: the agent rejects dockerSecurityOptions unless the
  # instance registers as SELinux- or AppArmor-capable, and the executor
  # needs no-new-privileges and seccomp:unconfined.
  user_data = base64encode(<<-EOT
    #!/bin/sh
    cat >> /etc/ecs/ecs.config <<'EOF'
    ECS_CLUSTER=${aws_ecs_cluster.this.name}
    ECS_AWSVPC_BLOCK_IMDS=true
    ECS_SELINUX_CAPABLE=true
    ECS_ENABLE_CONTAINER_METADATA=true
    EOF
  EOT
  )

  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${local.name}-ecs" }
  }
}

# min = max = 1: not for scaling, but so a dead instance is replaced and its
# tasks come back on their own.
resource "aws_autoscaling_group" "instance" {
  name_prefix         = "${local.name}-ecs-"
  min_size            = 1
  max_size            = 1
  desired_capacity    = 1
  vpc_zone_identifier = local.network.private_subnet_ids

  launch_template {
    id      = aws_launch_template.instance.id
    version = aws_launch_template.instance.latest_version
  }

  # A new launch template version (a new AMI, a new instance type) rolls the
  # instance. There is only one, so that is downtime: expected, for this
  # layout.
  instance_refresh {
    strategy = "Rolling"
    preferences {
      min_healthy_percentage = 0
    }
  }

  tag {
    key                 = "AmazonECSManaged"
    value               = "true"
    propagate_at_launch = true
  }
}

# Private DNS for the one address that crosses tasks: the executor reaches
# Temporal and Valkey in the control task as control.<name>.internal.
resource "aws_service_discovery_private_dns_namespace" "this" {
  name = "${local.name}.internal"
  vpc  = local.network.vpc_id
}

resource "aws_service_discovery_service" "control" {
  name = "control"

  dns_config {
    namespace_id   = aws_service_discovery_private_dns_namespace.this.id
    routing_policy = "MULTIVALUE"

    dns_records {
      type = "A"
      ttl  = 10
    }
  }
}

locals {
  control_dns = "control.${aws_service_discovery_private_dns_namespace.this.name}"
}

resource "aws_cloudwatch_log_group" "this" {
  name              = "/${local.name}"
  retention_in_days = var.log_retention_days
}
