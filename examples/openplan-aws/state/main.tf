# The bucket network/ and op-ecs/ keep their Terraform state in. op-ecs's
# state holds every secret openplan runs with (database passwords, the
# credential encryption key), so this bucket is as sensitive as they are.

resource "aws_s3_bucket" "state" {
  # Bucket names are global; the prefix keeps two installs from colliding.
  bucket_prefix = "${var.name}-tfstate-"

  # No force_destroy: `terraform destroy` here fails while any state is left,
  # which is the point. Tear down op-ecs and network first.
}

# Every write keeps the previous version, so a bad apply or a corrupted state
# can be rolled back by restoring an object version.
resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket = aws_s3_bucket.state.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "state" {
  bucket = aws_s3_bucket.state.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# State carries secrets, so refuse it over plain HTTP.
resource "aws_s3_bucket_policy" "state" {
  bucket = aws_s3_bucket.state.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.state.arn, "${aws_s3_bucket.state.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.state]
}

# The backend settings all three modules share, so none needs the bucket's
# generated name typed in: `terraform init -backend-config=../backend.hcl`.
# Each module sets its own key, and the region, in its backend block.
resource "local_file" "backend_config" {
  filename        = "${path.module}/../backend.hcl"
  file_permission = "0644"
  content         = <<-EOT
    bucket       = "${aws_s3_bucket.state.bucket}"
    encrypt      = true
    use_lockfile = true
  EOT
}
