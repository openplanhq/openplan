# Artifacts (run logs, saved plans) in S3, replacing the volume the api and
# executor share in docker-compose.
#
# Static keys for now: the S3 adapter doesn't read credentials from the task
# role yet. When it does, drop the IAM user and grant the same policy to the
# api and executor task roles.

resource "aws_s3_bucket" "artifacts" {
  bucket_prefix = "${local.name}-artifacts-"
  # Lets `terraform destroy` remove an example's bucket with logs in it.
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_iam_user" "artifacts" {
  name = "${local.name}-artifacts"
}

resource "aws_iam_user_policy" "artifacts" {
  name = "artifacts"
  user = aws_iam_user.artifacts.name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:ListBucket"]
        Resource = aws_s3_bucket.artifacts.arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
        Resource = "${aws_s3_bucket.artifacts.arn}/*"
      },
    ]
  })
}

resource "aws_iam_access_key" "artifacts" {
  user = aws_iam_user.artifacts.name
}
