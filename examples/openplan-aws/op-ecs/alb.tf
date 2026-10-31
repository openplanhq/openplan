# One hostname, three backends, split by path:
#
#   /dex/*            -> dex   (the issuer is https://<domain>/dex)
#   /v1/*, /healthz   -> api
#   everything else   -> web   (the built SPA)
#
# Routing /v1 here rather than through web's nginx matters: that nginx proxies
# to api:8081 through Docker's embedded resolver, which exists only under
# docker compose.

resource "aws_acm_certificate" "this" {
  domain_name       = local.domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for o in aws_acm_certificate.this.domain_validation_options : o.domain_name => o
  }

  zone_id = local.network.zone_id
  name    = each.value.resource_record_name
  type    = each.value.resource_record_type
  records = [each.value.resource_record_value]
  ttl     = 300
}

# Waits until ACM sees the record, which needs the zone delegated (see the
# network/ name_servers output). An undelegated zone makes this time out.
resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

resource "aws_security_group" "alb" {
  name_prefix = "${local.name}-alb-"
  description = "ALB: HTTPS from anywhere"
  vpc_id      = local.network.vpc_id

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTP, redirected to HTTPS"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "To the control task"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [local.network.vpc_cidr]
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_lb" "this" {
  name               = local.name
  load_balancer_type = "application"
  subnets            = local.network.public_subnet_ids
  security_groups    = [aws_security_group.alb.id]

  # A followed run's live output is one long response, idle between lines of
  # tofu output; the 60s default would cut it during a slow apply.
  idle_timeout = 900

  drop_invalid_header_fields = true
}

locals {
  # port: where the container listens. health_port/health_path: what the ALB
  # probes, which for Dex is its telemetry listener, not the issuer.
  target_groups = {
    web = { port = 5173, health_port = 5173, health_path = "/" }
    api = { port = 8081, health_port = 8081, health_path = "/healthz" }
    dex = { port = 5556, health_port = 5558, health_path = "/healthz/ready" }
  }
}

resource "aws_lb_target_group" "this" {
  for_each = local.target_groups

  name_prefix = substr(each.key, 0, 6)
  port        = each.value.port
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = local.network.vpc_id

  # One task, so draining only delays the replacement.
  deregistration_delay = 10

  health_check {
    port                = each.value.health_port
    path                = each.value.health_path
    matcher             = "200"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.this.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["web"].arn
  }
}

resource "aws_lb_listener_rule" "dex" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 10

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["dex"].arn
  }

  condition {
    path_pattern {
      values = ["/dex", "/dex/*"]
    }
  }
}

resource "aws_lb_listener_rule" "api" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 20

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["api"].arn
  }

  condition {
    path_pattern {
      values = ["/v1/*", "/healthz"]
    }
  }
}

resource "aws_route53_record" "app" {
  zone_id = local.network.zone_id
  name    = local.domain
  type    = "A"

  alias {
    name                   = aws_lb.this.dns_name
    zone_id                = aws_lb.this.zone_id
    evaluate_target_health = false
  }
}
