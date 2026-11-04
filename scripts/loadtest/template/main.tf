# The executor load test's template (#586): about 40 resources of the kinds a
# mid-size AWS module holds, applied once and then planned over and over, so
# each measured plan loads the AWS provider and refreshes real state.
#
# Every call goes to Floci, an AWS emulator, never to AWS: the endpoints come
# from AWS_ENDPOINT_URL (provider) and AWS_ENDPOINT_URL_S3 (backend), set as
# the stack's Environment entries. No instances and no autoscaling group:
# Floci runs instances as Docker containers. The S3 bucket needs Floci to
# answer as <account id>.<its host> too (see floci.yaml).

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= 6.71.0"
    }
  }

  backend "s3" {
    bucket                 = "openplan-loadtest"
    key                    = "loadtest.tfstate"
    region                 = "us-east-1"
    use_path_style         = true
    skip_region_validation = true
  }
}

provider "aws" {
  region                  = "us-east-1"
  s3_use_path_style       = true
  skip_metadata_api_check = true
}

data "aws_caller_identity" "current" {}

locals {
  # Every install plans into the one emulated account, so names that must be
  # unique carry a short, stable id of the install's workspace.
  id = substr(sha1(terraform.workspace), 0, 8)
}

resource "aws_vpc" "main" {
  cidr_block           = "10.40.0.0/16"
  enable_dns_hostnames = true
  tags                 = { Name = "lt-${local.id}" }
}

resource "aws_subnet" "main" {
  count             = 6
  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(aws_vpc.main.cidr_block, 8, count.index)
  availability_zone = ["us-east-1a", "us-east-1b", "us-east-1c"][count.index % 3]
  tags              = { Name = "lt-${local.id}-${count.index < 3 ? "public" : "private"}-${count.index % 3}" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
}

resource "aws_route" "internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.main.id
}

resource "aws_route_table_association" "main" {
  count          = 6
  subnet_id      = aws_subnet.main[count.index].id
  route_table_id = count.index < 3 ? aws_route_table.public.id : aws_route_table.private.id
}

resource "aws_security_group" "tier" {
  for_each    = { web = 443, app = 8080, db = 5432, admin = 22 }
  name        = "lt-${local.id}-${each.key}"
  description = "${each.key} tier"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = each.value
    to_port     = each.value
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.main.cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_iam_role" "service" {
  for_each = toset(["web", "app", "worker"])
  name     = "lt-${local.id}-${each.key}"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_policy" "service" {
  for_each = aws_iam_role.service
  name     = "lt-${local.id}-${each.key}-artifacts"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject"]
      Resource = "${aws_s3_bucket.artifacts.arn}/${each.key}/*"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "service" {
  for_each   = aws_iam_role.service
  role       = each.value.name
  policy_arn = aws_iam_policy.service[each.key].arn
}

resource "aws_iam_role_policy" "logs" {
  for_each = aws_iam_role.service
  name     = "logs"
  role     = each.value.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource = "arn:aws:logs:us-east-1:${data.aws_caller_identity.current.account_id}:log-group:/lt/${each.key}:*"
    }]
  })
}

resource "aws_iam_instance_profile" "web" {
  name = "lt-${local.id}-web"
  role = aws_iam_role.service["web"].name
}

resource "aws_s3_bucket" "artifacts" {
  bucket = "lt-${local.id}-artifacts"
}

resource "aws_s3_bucket_versioning" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "artifacts" {
  bucket                  = aws_s3_bucket.artifacts.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_launch_template" "web" {
  name                   = "lt-${local.id}-web"
  image_id               = "ami-0123456789abcdef0"
  instance_type          = "t3.micro"
  vpc_security_group_ids = [aws_security_group.tier["web"].id]
  iam_instance_profile {
    name = aws_iam_instance_profile.web.name
  }
}
