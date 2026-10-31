# One Postgres for every component, as in docker-compose: openplan's own
# database (OpenFGA's tables included), Dex's, and Temporal's two. The
# control task's db-init container creates them (templates/init-databases.sh).
#
# RDS for PostgreSQL 16 sets rds.force_ssl, so every client here connects
# with TLS: sslmode=require for the api and db-init, ssl.mode require for Dex,
# and the POSTGRES_TLS_* / SQL_TLS_* variables for Temporal.

resource "aws_db_subnet_group" "this" {
  name       = local.name
  subnet_ids = local.network.private_subnet_ids
}

resource "aws_security_group" "db" {
  name_prefix = "${local.name}-db-"
  description = "RDS: reachable from the control task only"
  vpc_id      = local.network.vpc_id

  # Not the executor: it runs tenant Terraform, and has no business with any
  # database.
  ingress {
    description     = "Postgres from the control task"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.control.id]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_db_instance" "this" {
  identifier     = local.name
  engine         = "postgres"
  engine_version = "16"
  instance_class = var.db_instance_class

  allocated_storage = 20
  storage_type      = "gp3"
  storage_encrypted = true

  username = "openplan_admin"
  password = random_password.db["admin"].result

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  multi_az               = false

  backup_retention_period = 7
  # An example should come down with `terraform destroy`. A real install
  # flips both of these.
  skip_final_snapshot = true
  deletion_protection = false

  # A minor version bump during the maintenance window, not mid-apply.
  auto_minor_version_upgrade = true
  apply_immediately          = false
}
