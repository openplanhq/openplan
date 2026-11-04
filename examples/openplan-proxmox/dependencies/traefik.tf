# Its own resource rather than Helm's create_namespace, which a destroy would
# leave behind.
resource "kubernetes_namespace_v1" "traefik" {
  metadata {
    name = "traefik"
  }
}

# Installed here rather than taken from the cluster, so every cluster gets the
# same one; k3s's own is turned off by the cluster template. Pinned to what the
# evaluation harness runs.
resource "helm_release" "traefik" {
  name       = "traefik"
  repository = "https://traefik.github.io/charts"
  chart      = "traefik"
  version    = "41.7.0"
  namespace  = kubernetes_namespace_v1.traefik.metadata[0].name

  # Only the Ingress provider: openplan's chart and Dex's Ingress are standard
  # Ingresses, so the IngressRoute CRDs aren't needed, and none outlive a
  # destroy (Helm never deletes CRDs).
  skip_crds = true

  values = [yamlencode({
    providers = { kubernetesCRD = { enabled = false } }
    ports = {
      web = {
        # Over HTTP the browser drops the Secure sign-in cookies, so sign-in
        # needs HTTPS.
        http = { redirections = { entryPoint = { to = "websecure", scheme = "https" } } }
      }
    }
    resources = {
      requests = { cpu = "200m", memory = "128Mi" }
      limits   = { cpu = "200m", memory = "128Mi" }
    }
  })]
}
