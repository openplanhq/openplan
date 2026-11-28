mock_provider "kubernetes" {}
mock_provider "helm" {}

override_data {
  target = data.terraform_remote_state.cluster
  values = {
    outputs = {
      api_url                 = "https://10.10.10.10:6443"
      cluster_ca_data         = "Y2E="
      client_certificate_data = "Y2VydA=="
      client_key_data         = "a2V5"
    }
  }
}

variables {
  environment       = "prod"
  chart_version     = "0.5.0"
  root_subject      = "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"
  cluster_workspace = "mtp_acme_prod_cluster_ab12cd34"
  state_bucket      = "openplan-state"
}

run "release_chart_by_default" {
  assert {
    condition     = helm_release.openplan.repository == "oci://ghcr.io/openplanhq/charts" && helm_release.openplan.chart == "openplan" && helm_release.openplan.version == "0.5.0"
    error_message = "the release doesn't install openplan 0.5.0 from the release charts"
  }
}

run "develop_chart" {
  variables {
    chart_repository = "oci://ghcr.io/openplanhq/charts-develop"
    chart_version    = "0.0.0-develop.146"
  }

  assert {
    condition     = helm_release.openplan.repository == "oci://ghcr.io/openplanhq/charts-develop" && helm_release.openplan.version == "0.0.0-develop.146"
    error_message = "the release doesn't install the develop chart it was given"
  }
}

run "values_point_at_dependencies" {
  assert {
    condition = yamldecode(helm_release.openplan.values[0]) == {
      temporal = { address = "temporal:7233" }
      postgres = { urlSecret = { name = "openplan-credentials", key = "database-url" } }
      valkey = {
        api      = { urlSecret = { name = "openplan-credentials", key = "valkey-api-url" } }
        executor = { urlSecret = { name = "openplan-credentials", key = "valkey-executor-url" } }
      }
      s3 = {
        bucket                = "openplan-artifacts"
        region                = "us-east-1"
        endpoint              = "http://minio:9000"
        forcePathStyle        = true
        accessKeyIDSecret     = { name = "openplan-dependencies", key = "minio-root-user" }
        secretAccessKeySecret = { name = "openplan-dependencies", key = "minio-root-password" }
      }
      oidc = {
        issuerURL          = "https://dex.prod.home.arpa/dex"
        clientID           = "openplan-api"
        clientSecretSecret = { name = "openplan-dependencies", key = "oidc-client-secret" }
      }
      api = {
        rootSubject                   = "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"
        sessionEncryptionKeySecret    = { name = "openplan-keys", key = "session-encryption-key" }
        credentialEncryptionKeySecret = { name = "openplan-keys", key = "credential-encryption-key" }
        trustedCASecret               = "openplan-trusted-ca"
      }
      executor = { replicas = 1 }
      ingress = {
        className     = "traefik"
        host          = "openplan.prod.home.arpa"
        tlsSecretName = "openplan-tls"
      }
    }
    error_message = "the chart's values don't match what the dependencies template made"
  }
}

run "url_output" {
  assert {
    condition     = output.url == "https://openplan.prod.home.arpa"
    error_message = "url is ${output.url}"
  }
}

run "fractional_replicas_refused" {
  command = plan

  variables {
    executor_replicas = 1.5
  }

  expect_failures = [var.executor_replicas]
}

run "version_range_refused" {
  command = plan

  variables {
    chart_version = ">=0.0.0-0"
  }

  expect_failures = [var.chart_version]
}

run "short_version_refused" {
  command = plan

  variables {
    chart_version = "0.5"
  }

  expect_failures = [var.chart_version]
}

run "empty_version_refused" {
  command = plan

  variables {
    chart_version = ""
  }

  expect_failures = [var.chart_version]
}

run "one_missing_credential_fails" {
  command = plan

  variables {
    cluster_workspace            = null
    kube_host                    = "https://10.10.10.10:6443"
    kube_cluster_ca_data         = "Y2E="
    kube_client_certificate_data = "Y2VydA=="
  }

  expect_failures = [var.kube_client_key_data]
}

run "no_connection_fails" {
  command = plan

  variables {
    cluster_workspace = null
  }

  expect_failures = [var.kube_host, var.kube_cluster_ca_data, var.kube_client_certificate_data, var.kube_client_key_data]
}
