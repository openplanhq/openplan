output "bucket" {
  value = aws_s3_bucket.state.bucket
}

output "backend_config" {
  description = "The file network/ and op-ecs/ pass to terraform init."
  value       = abspath(local_file.backend_config.filename)
}
