output "url" {
  value = "https://${local.domain}"
}

output "admin_email" {
  value = local.admin_email
}

output "admin_password" {
  description = "Password of the static Dex user. Read with: terraform output -raw admin_password"
  value       = random_password.admin.result
  sensitive   = true
}

output "cluster_name" {
  value = aws_ecs_cluster.this.name
}

output "log_group" {
  value = aws_cloudwatch_log_group.this.name
}

output "artifacts_bucket" {
  value = aws_s3_bucket.artifacts.bucket
}
