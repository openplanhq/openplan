mock_provider "kubernetes" {}
mock_provider "helm" {}
mock_provider "tls" {}
mock_provider "random" {}

variables {
  environment                  = "prod"
  kube_host                    = "https://10.10.10.10:6443"
  kube_cluster_ca_data         = "Y2E="
  kube_client_certificate_data = "Y2VydA=="
  kube_client_key_data         = "a2V5"
  dex_admin_email              = "admin@example.com"
  dex_admin_password_hash      = "$2y$10$abcdefghijklmnopqrstuuJ8kGq6Y0b8lJcEoYQyqzP6hVbB7m0yC"
}

run "root_subject_matches_dex" {
  # The harness's admin has this userID, and the harness pins the subject Dex
  # issues for it; the module has to compute the same string.
  variables {
    dex_admin_user_id = "7c4b2f0e-3d1a-4e8b-9f6c-2a5d8e1b0c47"
  }

  assert {
    condition     = output.root_subject == "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"
    error_message = "root_subject is not Dex's subject for the admin's userID"
  }
}

run "issuer_url" {
  variables {
    environment = "bench"
  }

  assert {
    condition     = output.dex_issuer_url == "https://dex.bench.home.arpa/dex"
    error_message = "dex_issuer_url is ${output.dex_issuer_url}"
  }

  assert {
    condition     = output.openplan_host == "openplan.bench.home.arpa"
    error_message = "openplan_host is ${output.openplan_host}"
  }
}

run "certificate_names" {
  assert {
    condition     = toset(tls_cert_request.server.dns_names) == toset(["openplan.prod.home.arpa", "dex.prod.home.arpa"])
    error_message = "the certificate does not name exactly openplan's and Dex's hosts"
  }
}

run "privileged_namespace" {
  assert {
    condition     = kubernetes_namespace_v1.openplan.metadata[0].labels["pod-security.kubernetes.io/enforce"] == "privileged"
    error_message = "the namespace must allow the executor's privileged pod"
  }
}

run "every_container_limited" {
  assert {
    condition = length([
      for m in kubernetes_manifest.dependency : m if m.manifest.kind == "Deployment"
    ]) == 5
    error_message = "expected Postgres, Temporal, Dex, Valkey and MinIO Deployments"
  }

  assert {
    condition = alltrue(flatten([
      for m in kubernetes_manifest.dependency : [
        for c in concat(
          try(m.manifest.spec.template.spec.containers, []),
          try(m.manifest.spec.template.spec.initContainers, []),
          ) : (
          c.resources.limits.cpu == c.resources.requests.cpu &&
          c.resources.limits.memory == c.resources.requests.memory
        )
      ] if contains(["Deployment", "Job"], m.manifest.kind)
    ]))
    error_message = "a container's requests and limits differ"
  }
}

run "dns_k3s" {
  assert {
    condition     = length(kubernetes_config_map_v1.coredns_custom) == 1
    error_message = "dns_mode k3s must write coredns-custom"
  }

  assert {
    condition     = strcontains(one(values(kubernetes_config_map_v1.coredns_custom[0].data)), "rewrite name exact dex.prod.home.arpa traefik.traefik.svc.cluster.local answer auto")
    error_message = "coredns-custom does not rewrite Dex's name to Traefik"
  }
}

run "dns_none" {
  variables {
    dns_mode = "none"
  }

  assert {
    condition     = length(kubernetes_config_map_v1.coredns_custom) == 0
    error_message = "dns_mode none must leave CoreDNS alone"
  }
}

run "missing_client_key_fails" {
  command = plan

  variables {
    kube_client_key_data = null
  }

  expect_failures = [var.kube_client_key_data]
}

run "missing_password_hash_fails" {
  command = plan

  variables {
    dex_admin_password_hash = null
  }

  expect_failures = [var.dex_admin_password_hash]
}

run "user_id_stable_per_environment" {
  # Without a userID set, the subject must not change between plans, or the
  # administrator would lose root on the next apply.
  assert {
    condition     = output.root_subject == base64encode(format("\n$%s\u0012\u0005local", uuidv5("url", "https://dex.prod.home.arpa/dex/admin")))
    error_message = "the default userID is not derived from the environment's issuer"
  }
}

run "plaintext_password_refused" {
  command = plan

  variables {
    dex_admin_password_hash = "hunter2"
  }

  expect_failures = [var.dex_admin_password_hash]
}

run "jobs_named_by_content" {
  # A Job's pod template is immutable, so a changed Job must be a new one.
  assert {
    condition = alltrue([
      for key, m in kubernetes_manifest.dependency :
      can(regex("^(postgres-init|minio-bucket)-[0-9a-f]{8}$", m.manifest.metadata.name)) if m.manifest.kind == "Job"
    ])
    error_message = "a Job's name doesn't carry a hash of its spec"
  }

  assert {
    condition = alltrue([
      for m in kubernetes_manifest.dependency :
      try(m.manifest.spec.template.metadata.labels, null) == null if m.manifest.kind == "Job"
    ])
    error_message = "a Job's pod template sets labels, which the API server adds to and the provider then reports as an inconsistent result"
  }
}

run "missing_host_fails" {
  command = plan

  variables {
    kube_host = null
  }

  expect_failures = [var.kube_host]
}

run "missing_ca_fails" {
  command = plan

  variables {
    kube_cluster_ca_data = null
  }

  expect_failures = [var.kube_cluster_ca_data]
}

run "missing_client_certificate_fails" {
  command = plan

  variables {
    kube_client_certificate_data = null
  }

  expect_failures = [var.kube_client_certificate_data]
}

run "bad_environment_fails" {
  command = plan

  variables {
    environment = "Prod"
  }

  expect_failures = [var.environment]
}

run "connection_from_cluster_state" {
  # The cluster template's outputs, read from its state: no kube_* credential
  # needed, and the client key never leaves state.
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
    cluster_workspace            = "mtp_acme_prod_cluster_ab12cd34"
    state_bucket                 = "openplan-state"
    kube_host                    = null
    kube_cluster_ca_data         = null
    kube_client_certificate_data = null
    kube_client_key_data         = null
  }

  assert {
    condition     = local.kube.host == "https://10.10.10.10:6443" && local.kube.client_key == "key"
    error_message = "the connection isn't taken from the cluster's state"
  }
}

run "cluster_workspace_needs_bucket" {
  command = plan

  variables {
    cluster_workspace = "mtp_acme_prod_cluster_ab12cd34"
  }

  expect_failures = [var.state_bucket]
}

run "keys_beside_the_database" {
  # openplan's credential key encrypts what this template's Postgres holds,
  # so the two share one lifetime.
  assert {
    condition     = kubernetes_secret_v1.keys.metadata[0].name == "openplan-keys" && toset(keys(kubernetes_secret_v1.keys.data)) == toset(["session-encryption-key", "credential-encryption-key"])
    error_message = "dependencies must make openplan-keys with both keys"
  }
}
