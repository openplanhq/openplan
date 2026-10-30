mock_provider "kubernetes" {}
mock_provider "helm" {}
mock_provider "random" {}

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
  image_tag         = "sha-abc"
  root_subject      = "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"
  cluster_workspace = "mtp_acme_prod_cluster_ab12cd34"
  state_bucket      = "openplan-state"
}

run "chart_from_checkout" {
  # The chart ships with no release yet, so it comes from the same checkout
  # as this template: chart and template are always one commit.
  assert {
    condition     = fileexists("${helm_release.openplan.chart}/Chart.yaml") && endswith(helm_release.openplan.chart, "/deploy/helm/openplan")
    error_message = "the release doesn't install the chart from deploy/helm/openplan in this checkout"
  }
}

run "values_point_at_dependencies" {
  assert {
    condition = yamldecode(helm_release.openplan.values[0]) == {
      image    = { tag = "sha-abc" }
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

run "latest_refused" {
  command = plan

  variables {
    image_tag = "latest"
  }

  expect_failures = [var.image_tag]
}

run "empty_tag_refused" {
  command = plan

  variables {
    image_tag = ""
  }

  expect_failures = [var.image_tag]
}

run "no_connection_fails" {
  command = plan

  variables {
    cluster_workspace = null
  }

  expect_failures = [var.kube_host, var.kube_cluster_ca_data, var.kube_client_certificate_data, var.kube_client_key_data]
}
