# Two tasks, both awsvpc, both on the one instance:
#
#   control   db-init, valkey, temporal, dex, api, web: one network
#             namespace, so they reach each other on 127.0.0.1 the way they
#             reach each other by service name under docker compose.
#   executor  runs tenant Terraform in gVisor sandboxes. Its own ENI and
#             security group, which lets it reach Temporal and Valkey and
#             nothing else inside the VPC.
#
# Why one control task rather than a service each: an awsvpc task takes an ENI,
# and a t4g instance has three (one its own), so two tasks is the ceiling.
# Bridge mode would lift it, but then each service needs Service Connect's
# Envoy sidecar to find the others. The cost of one task is that changing any
# of its containers restarts all of them.

locals {
  images = {
    api      = "ghcr.io/openplanhq/openplan-api:${var.openplan_version}"
    executor = "ghcr.io/openplanhq/openplan-executor:${var.openplan_version}"
    web      = "ghcr.io/openplanhq/openplan-web:${var.openplan_version}"
    dex      = "ghcr.io/dexidp/dex:v2.45.1"
    temporal = "temporalio/auto-setup:1.28.1"
    valkey   = "valkey/valkey:8.1.10-alpine"
    postgres = "postgres:16-alpine"
  }

  admin_email = coalesce(var.admin_email, "admin@${local.domain}")

  dex_config = templatefile("${path.module}/templates/dex.yaml.tftpl", {
    domain      = local.domain
    db_host     = local.db_host
    admin_email = local.admin_email
    admin_hash  = base64encode(random_password.admin.bcrypt_hash)
  })

  # The sub Dex issues for the static user in dex.yaml.tftpl: a protobuf of its
  # userID and the connector ID "local", base64url. Fixed because both inputs
  # are; it matches docker-compose.release.yaml's for the same reason.
  root_subject = "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"

  # Shared by the api and the executor.
  artifact_env = [
    { name = "ARTIFACT_STORE_KIND", value = "s3" },
    { name = "S3_BUCKET", value = aws_s3_bucket.artifacts.bucket },
    { name = "S3_REGION", value = local.network.region },
  ]
  artifact_secrets = [
    { name = "S3_ACCESS_KEY_ID", valueFrom = aws_ssm_parameter.secret["s3-access-key-id"].arn },
    { name = "S3_SECRET_ACCESS_KEY", valueFrom = aws_ssm_parameter.secret["s3-secret-access-key"].arn },
  ]
}

# Only the task execution role exists: the ECS agent uses it to pull secrets
# and write logs. Neither task gets a task role, so nothing running inside one,
# a hostile template least of all, holds AWS credentials beyond the S3 keys.
resource "aws_iam_role" "execution" {
  name_prefix = "${local.name}-exec-"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "execution_secrets" {
  name = "ssm-secrets"
  role = aws_iam_role.execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameters"]
      Resource = [for p in aws_ssm_parameter.secret : p.arn]
    }]
  })
}

resource "aws_security_group" "control" {
  name_prefix = "${local.name}-control-"
  description = "Control task: the ALB on its three ports, the executor on Temporal and Valkey"
  vpc_id      = local.network.vpc_id

  dynamic "ingress" {
    for_each = toset([5173, 8081, 5556, 5558])

    content {
      description     = "ALB"
      from_port       = ingress.value
      to_port         = ingress.value
      protocol        = "tcp"
      security_groups = [aws_security_group.alb.id]
    }
  }

  ingress {
    description     = "Temporal from the executor"
    from_port       = 7233
    to_port         = 7233
    protocol        = "tcp"
    security_groups = [aws_security_group.executor.id]
  }

  ingress {
    description     = "Valkey from the executor"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.executor.id]
  }

  # RDS, image pulls, and the api's own requests to the issuer, which go out
  # through NAT and back in through the ALB.
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

# No ingress at all. Egress is open because templates need the internet
# (providers, modules, the clouds they manage); what stops it reaching the rest
# of the VPC is every other security group admitting it only where named.
resource "aws_security_group" "executor" {
  name_prefix = "${local.name}-executor-"
  description = "Executor: outbound only"
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

locals {
  log_config = {
    for c in ["db-init", "valkey", "temporal", "dex", "api", "web", "executor"] : c => {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.this.name
        awslogs-region        = local.network.region
        awslogs-stream-prefix = c
      }
    }
  }

  # valkey, temporal and dex wait for this; the api waits for them.
  after_db_init = [{ containerName = "db-init", condition = "SUCCESS" }]
}

resource "aws_ecs_task_definition" "control" {
  family                   = "${local.name}-control"
  requires_compatibilities = ["EC2"]
  network_mode             = "awsvpc"
  execution_role_arn       = aws_iam_role.execution.arn

  container_definitions = jsonencode([
    {
      name             = "db-init"
      image            = local.images.postgres
      essential        = false
      memory           = 64
      command          = ["sh", "-c", file("${path.module}/templates/init-databases.sh")]
      logConfiguration = local.log_config["db-init"]
      environment = [
        { name = "PGHOST", value = local.db_host },
        { name = "PGUSER", value = aws_db_instance.this.username },
        { name = "PGDATABASE", value = "postgres" },
        { name = "PGSSLMODE", value = "require" },
      ]
      secrets = [
        { name = "PGPASSWORD", valueFrom = aws_ssm_parameter.secret["db-admin-password"].arn },
        { name = "OPENPLAN_DB_PASSWORD", valueFrom = aws_ssm_parameter.secret["db-openplan-password"].arn },
        { name = "DEX_DB_PASSWORD", valueFrom = aws_ssm_parameter.secret["db-dex-password"].arn },
        { name = "TEMPORAL_DB_PASSWORD", valueFrom = aws_ssm_parameter.secret["db-temporal-password"].arn },
      ]
    },

    # The ACL is docker-compose.yaml's, one --user per process: the executor
    # can append to a log stream and set its expiry, and cannot read one.
    # Through sh so the passwords come from the environment rather than being
    # written into the task definition.
    {
      name              = "valkey"
      image             = local.images.valkey
      memoryReservation = 320
      portMappings      = [{ containerPort = 6379 }]
      logConfiguration  = local.log_config["valkey"]
      command = ["sh", "-c", join(" ", [
        "exec valkey-server",
        "--maxmemory 256mb --maxmemory-policy noeviction --save '' --appendonly no",
        "--user default off",
        "--user api on \">$VALKEY_API_PASSWORD\" '~runlog:*' +ping +xread +xrange +xrevrange +xadd +expire",
        "--user executor on \">$VALKEY_EXECUTOR_PASSWORD\" '~runlog:*' +ping +xadd +expire",
      ])]
      secrets = [
        { name = "VALKEY_API_PASSWORD", valueFrom = aws_ssm_parameter.secret["valkey-api-password"].arn },
        { name = "VALKEY_EXECUTOR_PASSWORD", valueFrom = aws_ssm_parameter.secret["valkey-exec-password"].arn },
      ]
      healthCheck = {
        command  = ["CMD-SHELL", "valkey-cli --no-auth-warning --user api --pass \"$VALKEY_API_PASSWORD\" ping"]
        interval = 10
        timeout  = 5
        retries  = 6
      }
    },

    # auto-setup applies Temporal's schema on boot, which makes it a
    # development image. Good enough for an example; a real install runs
    # temporalio/server and migrates with temporalio/admin-tools.
    {
      name              = "temporal"
      image             = local.images.temporal
      memoryReservation = 1024
      portMappings      = [{ containerPort = 7233 }]
      dependsOn         = local.after_db_init
      logConfiguration  = local.log_config["temporal"]
      environment = [
        { name = "DB", value = "postgres12" },
        { name = "DB_PORT", value = "5432" },
        { name = "POSTGRES_SEEDS", value = local.db_host },
        { name = "POSTGRES_USER", value = "temporal" },
        # db-init created both databases; the temporal role can't create one.
        { name = "SKIP_DB_CREATE", value = "true" },
        # TLS to RDS, for the schema tool (POSTGRES_*) and the server (SQL_*).
        # No host verification: the RDS CA bundle isn't in the image.
        { name = "POSTGRES_TLS_ENABLED", value = "true" },
        { name = "POSTGRES_TLS_DISABLE_HOST_VERIFICATION", value = "true" },
        { name = "SQL_TLS_ENABLED", value = "true" },
        { name = "SQL_HOST_VERIFICATION", value = "false" },
        # The image binds to the hostname's address by default, which isn't
        # 127.0.0.1, where the api in this same task dials it. With 0.0.0.0 it
        # broadcasts its ENI address, which is what the executor dials.
        { name = "BIND_ON_IP", value = "0.0.0.0" },
        { name = "TEMPORAL_ADDRESS", value = "127.0.0.1:7233" },
      ]
      secrets = [
        { name = "POSTGRES_PWD", valueFrom = aws_ssm_parameter.secret["db-temporal-password"].arn },
      ]
      healthCheck = {
        command     = ["CMD", "temporal", "operator", "cluster", "health", "--address", "127.0.0.1:7233"]
        interval    = 10
        timeout     = 5
        retries     = 12
        startPeriod = 60
      }
    },

    {
      name              = "dex"
      image             = local.images.dex
      memoryReservation = 128
      portMappings      = [{ containerPort = 5556 }, { containerPort = 5558 }]
      dependsOn         = local.after_db_init
      logConfiguration  = local.log_config["dex"]
      command = ["sh", "-c",
        "printf '%s' \"$DEX_CONFIG\" > /tmp/dex.yaml && exec dex serve /tmp/dex.yaml"
      ]
      environment = [
        { name = "DEX_CONFIG", value = local.dex_config },
      ]
      secrets = [
        { name = "DEX_DB_PASSWORD", valueFrom = aws_ssm_parameter.secret["db-dex-password"].arn },
        { name = "OIDC_CLIENT_SECRET", valueFrom = aws_ssm_parameter.secret["oidc-client-secret"].arn },
      ]
      healthCheck = {
        command  = ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:5558/healthz/ready"]
        interval = 10
        timeout  = 5
        retries  = 12
      }
    },

    {
      name              = "api"
      image             = local.images.api
      memoryReservation = 512
      portMappings      = [{ containerPort = 8081 }]
      logConfiguration  = local.log_config["api"]
      # The api discovers the issuer while it starts, and an unreachable one is
      # a boot failure. Dex healthy is necessary but not sufficient: the api
      # reaches it through the ALB, whose target can lag a few seconds, so the
      # restart policy covers the gap.
      dependsOn = concat(local.after_db_init, [
        { containerName = "temporal", condition = "HEALTHY" },
        { containerName = "dex", condition = "HEALTHY" },
        { containerName = "valkey", condition = "HEALTHY" },
      ])
      restartPolicy = { enabled = true, restartAttemptPeriod = 60 }
      environment = concat(local.artifact_env, [
        { name = "HTTP_ADDRESS", value = ":8081" },
        { name = "TEMPORAL_ADDRESS", value = "127.0.0.1:7233" },
        # Refuses the published development keys and plain-http URLs.
        { name = "OPENPLAN_ENVIRONMENT", value = "production" },
        # Baked into the web image at build time, so it must match.
        { name = "OPENPLAN_TENANT_ID", value = "tenant_123" },
        { name = "OIDC_ISSUER_URL", value = "https://${local.domain}/dex" },
        { name = "OIDC_CLIENT_ID", value = "openplan-api" },
        { name = "OPENPLAN_PUBLIC_URL", value = "https://${local.domain}" },
        { name = "OPENPLAN_TERRAFORM_TIMEOUT", value = "45m" },
        { name = "OPENPLAN_ROOT_SUBJECT", value = local.root_subject },
      ])
      secrets = concat(local.artifact_secrets, [
        { name = "DATABASE_URL", valueFrom = aws_ssm_parameter.secret["database-url"].arn },
        { name = "VALKEY_URL", valueFrom = aws_ssm_parameter.secret["valkey-api-url"].arn },
        { name = "CREDENTIAL_ENCRYPTION_KEY", valueFrom = aws_ssm_parameter.secret["credential-key"].arn },
        { name = "SESSION_ENCRYPTION_KEY", valueFrom = aws_ssm_parameter.secret["session-key"].arn },
        { name = "OIDC_CLIENT_SECRET", valueFrom = aws_ssm_parameter.secret["oidc-client-secret"].arn },
      ])
    },

    {
      name              = "web"
      image             = local.images.web
      memoryReservation = 64
      portMappings      = [{ containerPort = 5173 }]
      logConfiguration  = local.log_config["web"]
    },
  ])
}

resource "aws_ecs_task_definition" "executor" {
  family                   = "${local.name}-executor"
  requires_compatibilities = ["EC2"]
  network_mode             = "awsvpc"
  execution_role_arn       = aws_iam_role.execution.arn

  container_definitions = jsonencode([{
    name  = "executor"
    image = local.images.executor
    # A hard limit, and every sandbox's memory is inside it: the sandboxes'
    # cgroups nest under this container's.
    memory           = var.executor_max_sessions * var.executor_sandbox_memory + 1024
    logConfiguration = local.log_config["executor"]

    environment = concat(local.artifact_env, [
      { name = "TEMPORAL_ADDRESS", value = "${local.control_dns}:7233" },
      { name = "EXECUTOR_RUN_ROOT", value = "/var/lib/openplan/runs" },
      { name = "EXECUTOR_SANDBOX_STATE", value = "/var/lib/openplan/sandboxes" },
      { name = "EXECUTOR_MAX_SESSIONS", value = tostring(var.executor_max_sessions) },
      { name = "EXECUTOR_SANDBOX_MEMORY", value = tostring(var.executor_sandbox_memory) },
    ])
    secrets = concat(local.artifact_secrets, [
      { name = "VALKEY_URL", valueFrom = aws_ssm_parameter.secret["valkey-exec-url"].arn },
    ])

    # docker-compose.yaml's executor, field for field. Root with these seven
    # capabilities is what runsc needs to start a sandbox per activity; the
    # sandboxes' processes run unprivileged. NET_ADMIN and NET_RAW, with
    # forwarding on, wire each sandbox to this task's network through a
    # bridge and NAT.
    linuxParameters = {
      capabilities = {
        drop = ["ALL"]
        add  = ["SYS_ADMIN", "SETUID", "SETGID", "SETFCAP", "CHOWN", "NET_ADMIN", "NET_RAW"]
      }
      # Ephemeral, so a restarted executor starts with no runs and no
      # sandboxes. exec because providers are programs in the run's
      # workspace. Sizes in MiB, and tmpfs pages count against memory above.
      tmpfs = [
        { containerPath = "/var/lib/openplan/runs", size = 4096, mountOptions = ["exec"] },
        { containerPath = "/var/lib/openplan/sandboxes", size = 64 },
      ]
    }
    # AL2023 has no AppArmor, so compose's apparmor=unconfined has no
    # counterpart. Its cgroup: private is Docker's default on cgroup v2,
    # which AL2023 is.
    dockerSecurityOptions = ["no-new-privileges", "seccomp:unconfined"]
    systemControls = [
      { namespace = "net.ipv4.ip_forward", value = "1" },
    ]
  }])
}

# Both services: one task, and no room on the instance (ENIs, memory) for a
# second copy during a deploy, so the old task stops before the new one starts.

resource "aws_ecs_service" "control" {
  name            = "control"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.control.arn
  desired_count   = 1
  launch_type     = "EC2"

  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100
  # Temporal's first boot applies its schema, and the api waits for it.
  health_check_grace_period_seconds = 300

  network_configuration {
    subnets         = local.network.private_subnet_ids
    security_groups = [aws_security_group.control.id]
  }

  service_registries {
    registry_arn = aws_service_discovery_service.control.arn
  }

  dynamic "load_balancer" {
    for_each = local.target_groups

    content {
      target_group_arn = aws_lb_target_group.this[load_balancer.key].arn
      container_name   = load_balancer.key
      container_port   = load_balancer.value.port
    }
  }

  depends_on = [aws_lb_listener.https]
}

resource "aws_ecs_service" "executor" {
  name            = "executor"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.executor.arn
  desired_count   = 1
  launch_type     = "EC2"

  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  network_configuration {
    subnets         = local.network.private_subnet_ids
    security_groups = [aws_security_group.executor.id]
  }
}
