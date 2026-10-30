# The real random and tls providers, which leave generated values unknown at
# plan, as a fresh install's first plan sees them. kubernetes_manifest refuses
# a manifest that isn't wholly known at plan, so every one must be.
mock_provider "kubernetes" {}
mock_provider "helm" {}

variables {
  environment                  = "prod"
  kube_host                    = "https://10.10.10.10:6443"
  kube_cluster_ca_data         = "Y2E="
  kube_client_certificate_data = "Y2VydA=="
  kube_client_key_data         = "a2V5"
  dex_admin_email              = "admin@example.com"
  dex_admin_password_hash      = "$2y$10$abcdefghijklmnopqrstuuJ8kGq6Y0b8lJcEoYQyqzP6hVbB7m0yC"
}

run "manifests_known_at_first_plan" {
  command = plan

  assert {
    condition     = alltrue([for m in kubernetes_manifest.dependency : m.manifest.kind != ""])
    error_message = "a manifest is unknown at the first plan"
  }

  assert {
    condition     = output.root_subject != ""
    error_message = "root_subject is unknown at the first plan"
  }
}
