output "region" {
  value = var.region
}

output "name" {
  value = var.name
}

output "vpc_id" {
  value = aws_vpc.this.id
}

output "vpc_cidr" {
  value = aws_vpc.this.cidr_block
}

output "public_subnet_ids" {
  value = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  value = aws_subnet.private[*].id
}

output "zone_id" {
  value = aws_route53_zone.this.zone_id
}

output "domain_name" {
  value = aws_route53_zone.this.name
}

output "name_servers" {
  description = "Set these as the domain's NS records at its registrar (or parent zone) before applying op-ecs."
  value       = aws_route53_zone.this.name_servers
}
