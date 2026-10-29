#!/bin/sh
# EVALUATION ONLY. Undoes setup.sh: deletes the openplan namespace (the chart's
# release, the dependencies, their volumes and every Secret with it),
# Traefik, and the CoreDNS rewrite. mkcert's CA stays trusted: it is this
# machine's, not the harness's, and other projects may use it; remove it with
# `mkcert -uninstall`.
set -eu

NAMESPACE=openplan

context=$(kubectl config current-context)
if [ "$context" != docker-desktop ]; then
	echo "teardown.sh: kubectl's context is '$context'; this harness runs only on docker-desktop." >&2
	exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "==> namespace $NAMESPACE"
kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait --timeout=300s

echo "==> Traefik"
helm uninstall traefik -n traefik --ignore-not-found --wait --timeout 300s >/dev/null
kubectl delete namespace traefik --ignore-not-found --wait --timeout=300s

echo "==> CoreDNS rewrite for dex.localhost"
kubectl -n kube-system get configmap coredns -o 'jsonpath={.data.Corefile}' >"$work/Corefile"
if grep -q 'dex\.localhost' "$work/Corefile"; then
	grep -v 'dex\.localhost' "$work/Corefile" >"$work/Corefile.new"
	{ printf 'data:\n  Corefile: |\n'; sed 's/^/    /' "$work/Corefile.new"; } >"$work/coredns-patch.yaml"
	kubectl -n kube-system patch configmap coredns --type merge --patch-file "$work/coredns-patch.yaml" >/dev/null
	kubectl -n kube-system rollout restart deployment/coredns >/dev/null
	kubectl -n kube-system rollout status deployment/coredns --timeout=120s
else
	echo "absent"
fi
