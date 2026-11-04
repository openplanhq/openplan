# The api discovers Dex at the browser's URL while it boots. In a pod that
# name has to land on Traefik's Service; answer auto renames the reply back,
# or a resolver that checks the answer's name (Go's does) discards it. k3s
# imports *.override from this ConfigMap into its CoreDNS. Docker Desktop's
# CoreDNS has no such hook, and writing its whole Corefile from here would
# delete it on destroy, so there the rewrite is made by hand (dns_mode none).
resource "kubernetes_config_map_v1" "coredns_custom" {
  count = var.dns_mode == "k3s" ? 1 : 0

  metadata {
    name      = "coredns-custom"
    namespace = "kube-system"
  }

  data = {
    "openplan-${var.environment}.override" = "rewrite name exact ${local.dex_host} traefik.traefik.svc.cluster.local answer auto\n"
  }
}
