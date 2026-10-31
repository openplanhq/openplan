#!/bin/sh
# Undoes bootstrap.sh: everything Argo CD installed, the Secrets, Argo CD
# itself, the CRDs and the CoreDNS rewrite. Stacks and stored credentials go
# with the openplan namespace. Safe to run twice, or after a bootstrap that
# stopped halfway.
#
# The dedicated mkcert root stays trusted by the Mac: removing it from the
# system trust store needs your password, so the last lines say how.
set -eu

OPENPLAN_CAROOT=${OPENPLAN_CAROOT:-$HOME/.local/share/mkcert-openplan}

context=$(kubectl config current-context)
if [ "$context" != docker-desktop ]; then
	echo "teardown.sh: kubectl's context is '$context'; this runs only on docker-desktop." >&2
	exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The finalizers on the root and on each child make this delete everything
# they installed, cluster-wide objects included, before the Application goes.
# Argo CD must still be running for that, so it comes first.
echo "==> Argo CD Applications"
if kubectl get crd applications.argoproj.io >/dev/null 2>&1; then
	kubectl -n argocd delete application openplan-docker-desktop --ignore-not-found --wait --timeout=600s
fi

# What no Application owned: the Secrets bootstrap.sh made, and the
# namespaces CreateNamespace left behind.
echo "==> namespaces"
kubectl delete namespace openplan traefik cert-manager --ignore-not-found --wait --timeout=300s

echo "==> Argo CD"
helm uninstall argocd -n argocd --ignore-not-found --wait --timeout 300s >/dev/null
kubectl delete namespace argocd --ignore-not-found --wait --timeout=300s

# Both charts drop their CRDs here (keep is off), but a teardown of a
# half-finished bootstrap may find some an Application never got to delete.
echo "==> CRDs"
crds=$(kubectl get crd -o name | grep -E '\.(cert-manager\.io|argoproj\.io)$' || true)
if [ -n "$crds" ]; then
	# shellcheck disable=SC2086 # one name per word
	kubectl delete $crds --wait --timeout=120s
else
	echo "absent"
fi

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

cat <<EOF

Done. The dedicated mkcert root is still trusted by this Mac. To remove it:

  CAROOT="$OPENPLAN_CAROOT" mkcert -uninstall && rm -rf "$OPENPLAN_CAROOT"
EOF
