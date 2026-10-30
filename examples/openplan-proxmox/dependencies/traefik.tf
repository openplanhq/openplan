# Installed here rather than taken from the cluster, so every cluster gets the
# same one; k3s's own is turned off by the cluster template. Pinned to what the
# evaluation harness runs.
resource "helm_release" "traefik" {
  name             = "traefik"
  repository       = "https://traefik.github.io/charts"
  chart            = "traefik"
  version          = "41.7.0"
  namespace        = "traefik"
  create_namespace = true

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
