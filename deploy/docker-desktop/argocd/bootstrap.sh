#!/bin/sh
# Brings up openplan on Docker Desktop's Kubernetes, kept on develop by Argo CD.
# This script makes only what can't live in git: a dedicated mkcert root the
# Mac trusts, the Secrets, and the CoreDNS rewrite. Then it installs Argo CD
# and applies root.yaml; Argo CD installs everything else from apps/.
#
# The Secrets are made here because Argo CD renders charts without `lookup`,
# so passwords generated in a chart would change on every sync, under data
# already stored with them.
#
#   ROOT_REVISION   the git revision root.yaml follows (default: develop)
#   OPENPLAN_CAROOT the dedicated mkcert root (default:
#                   ~/.local/share/mkcert-openplan)
#
# Rerunning is safe: what exists is kept, so keys and passwords never change
# under stored data. teardown.sh undoes all of it.
set -eu

ARGOCD_CHART_VERSION=10.11.6
NAMESPACE=openplan
ROOT_REVISION=${ROOT_REVISION:-develop}
OPENPLAN_CAROOT=${OPENPLAN_CAROOT:-$HOME/.local/share/mkcert-openplan}

here=$(cd "$(dirname "$0")" && pwd)

# Every guard runs before anything changes, so a refusal leaves the cluster
# and the Mac as they were.
context=$(kubectl config current-context)
if [ "$context" != docker-desktop ]; then
	echo "bootstrap.sh: kubectl's context is '$context'; this runs only on docker-desktop." >&2
	exit 1
fi
# Pinned on every call from here: the current context can change in another
# terminal while this runs, and the check above reads it only once.
kubectl() { command kubectl --context docker-desktop "$@"; }
helm() { command helm --kube-context docker-desktop "$@"; }
if ! command -v mkcert >/dev/null 2>&1; then
	echo "bootstrap.sh: needs mkcert (macOS: brew install mkcert)." >&2
	exit 1
fi
# The by-hand setup (../setup.sh) claims the same namespace, Traefik and
# CoreDNS line.
# setup.sh installs Traefik as a Helm release; Argo CD doesn't, so a release
# means the by-hand setup is here.
if helm status traefik -n traefik >/dev/null 2>&1; then
	echo "bootstrap.sh: the by-hand setup (deploy/docker-desktop/setup.sh) is installed; remove it first with deploy/docker-desktop/teardown.sh." >&2
	exit 1
fi

work=$(mktemp -d)
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

kube() { kubectl -n "$NAMESPACE" "$@"; }
secret_exists() { kube get secret "$1" >/dev/null 2>&1; }
secret_value() { kube get secret "$1" -o "jsonpath={.data.$2}" | base64 -d; }
fresh() { openssl rand -hex 32; }

# A root of its own, not the everyday one in `mkcert -CAROOT`: its key goes
# into the cluster, and a separate root can be untrusted on its own.
echo "==> dedicated mkcert root at $OPENPLAN_CAROOT"
mkdir -p "$OPENPLAN_CAROOT"
chmod 700 "$OPENPLAN_CAROOT"
# Creates the root on the first run and asks for the Mac's password to trust
# it; a no-op after.
CAROOT=$OPENPLAN_CAROOT mkcert -install

echo "==> CA Secret for cert-manager"
kubectl create namespace cert-manager --dry-run=client -o yaml | kubectl apply -f - >/dev/null
if kubectl -n cert-manager get secret mkcert-ca >/dev/null 2>&1; then
	echo "kept: mkcert-ca"
else
	kubectl -n cert-manager create secret tls mkcert-ca \
		--cert="$OPENPLAN_CAROOT/rootCA.pem" --key="$OPENPLAN_CAROOT/rootCA-key.pem"
fi

# The Secrets below live in it, so it exists before Argo CD does. Argo CD's
# dependencies Application takes it over from the same manifest.
echo "==> namespace $NAMESPACE"
kubectl apply -f "$here/../dependencies.yaml" -l app.kubernetes.io/component=namespace

# Keep these two Secrets in step with deploy/docker-desktop/setup.sh: both
# read the same dependencies.yaml and values.yaml.
echo "==> dependency passwords"
if secret_exists openplan-dependencies; then
	echo "kept: openplan-dependencies"
else
	cat >"$work/dependencies.env" <<-EOF
		postgres-password=$(fresh)
		dex-db-password=$(fresh)
		temporal-db-password=$(fresh)
		valkey-api-password=$(fresh)
		valkey-executor-password=$(fresh)
		minio-root-user=openplan
		minio-root-password=$(fresh)
		oidc-client-secret=$(fresh)
	EOF
	kube create secret generic openplan-dependencies --from-env-file="$work/dependencies.env"
fi

echo "==> openplan-credentials"
if secret_exists openplan-credentials; then
	echo "kept: openplan-credentials"
else
	kube create secret generic openplan-credentials \
		--from-literal=session-encryption-key="$(fresh)" \
		--from-literal=credential-encryption-key="$(fresh)" \
		--from-literal=database-url="postgres://openplan:$(secret_value openplan-dependencies postgres-password)@postgres:5432/openplan?sslmode=disable" \
		--from-literal=valkey-api-url="redis://api:$(secret_value openplan-dependencies valkey-api-password)@valkey:6379/0" \
		--from-literal=valkey-executor-url="redis://executor:$(secret_value openplan-dependencies valkey-executor-password)@valkey:6379/0"
fi

# The api discovers Dex at https://dex.localhost/dex, the URL the browser
# uses; in a pod this rewrite sends it to Traefik. The same line as setup.sh's.
echo "==> CoreDNS rewrite for dex.localhost"
kubectl -n kube-system get configmap coredns -o 'jsonpath={.data.Corefile}' >"$work/Corefile"
if grep -q 'dex\.localhost' "$work/Corefile"; then
	echo "kept: rewrite already present"
else
	awk '{ print } NR == 1 { print "    rewrite name exact dex.localhost traefik.traefik.svc.cluster.local answer auto" }' \
		"$work/Corefile" >"$work/Corefile.new"
	{ printf 'data:\n  Corefile: |\n'; sed 's/^/    /' "$work/Corefile.new"; } >"$work/coredns-patch.yaml"
	kubectl -n kube-system patch configmap coredns --type merge --patch-file "$work/coredns-patch.yaml" >/dev/null
	kubectl -n kube-system rollout restart deployment/coredns >/dev/null
	kubectl -n kube-system rollout status deployment/coredns --timeout=120s
fi

# Its own Dex, notifications and ApplicationSet controller go unused here.
# Without the keep policy, uninstalling it removes its CRDs too.
echo "==> Argo CD"
helm upgrade --install argocd argo-cd --repo https://argoproj.github.io/argo-helm \
	--version "$ARGOCD_CHART_VERSION" -n argocd --create-namespace \
	--set dex.enabled=false \
	--set notifications.enabled=false \
	--set applicationSet.replicas=0 \
	--set crds.keep=false \
	--wait --timeout 300s >/dev/null

echo "==> root Application, following $ROOT_REVISION"
sed "s|targetRevision: develop|targetRevision: $ROOT_REVISION|" "$here/root.yaml" | kubectl apply -f -

cat <<EOF

Argo CD is installing the rest. Watch it with:

  kubectl -n argocd get applications -w

Once every Application is Synced and Healthy, open https://openplan.localhost
and sign in as admin@openplan.local / admin-local-only.

Argo CD's own UI:

  kubectl -n argocd port-forward svc/argocd-server 8080:443

then https://localhost:8080 as admin, with the password from

  kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d
EOF
