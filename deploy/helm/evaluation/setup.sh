#!/bin/sh
# EVALUATION ONLY. Prepares Docker Desktop's Kubernetes for the openplan chart
# in production mode: an ingress controller, a local CA and certificate for
# openplan.localhost and dex.localhost, fresh keys and passwords, a CoreDNS
# rewrite so pods reach Dex at the browser's issuer, and the dependencies in
# dependencies.yaml. Then:
#
#   helm install openplan <chart> -n openplan -f deploy/helm/evaluation/values.yaml
#
# Rerunning is safe: Secrets that exist are kept, so keys and passwords never
# change under data that was sealed or stored with them. teardown.sh undoes
# all of it.
set -eu

# Pinned, so a rerun installs what was evaluated. Keep in step with
# teardown.sh, which deletes by the same manifest.
INGRESS_NGINX_MANIFEST=https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.15.1/deploy/static/provider/cloud/deploy.yaml
NAMESPACE=openplan
# The sub Dex issues for admin@openplan.local, from the userID in
# dependencies.yaml and the "local" connector; the compose value.
ROOT_SUBJECT=CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs

here=$(cd "$(dirname "$0")" && pwd)

# This script rewrites cluster DNS and installs cluster-wide objects; aimed at
# any cluster but the local one, that is damage, not an evaluation.
context=$(kubectl config current-context)
if [ "$context" != docker-desktop ]; then
	echo "setup.sh: kubectl's context is '$context'; this harness runs only on docker-desktop." >&2
	exit 1
fi

# Key material is written here and nowhere else, and removed on exit. The CA's
# private key goes with it: nothing can be signed by this CA after setup.
work=$(mktemp -d)
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

kube() { kubectl -n "$NAMESPACE" "$@"; }
secret_exists() { kube get secret "$1" >/dev/null 2>&1; }
secret_value() { kube get secret "$1" -o "jsonpath={.data.$2}" | base64 -d; }
fresh() { openssl rand -hex 32; }

echo "==> ingress-nginx"
kubectl apply -f "$INGRESS_NGINX_MANIFEST" >/dev/null
kubectl -n ingress-nginx rollout status deployment/ingress-nginx-controller --timeout=300s

echo "==> namespace $NAMESPACE"
kubectl apply -f "$here/dependencies.yaml" -l app.kubernetes.io/component=namespace

echo "==> TLS certificate and CA"
if secret_exists openplan-tls; then
	echo "kept: openplan-tls and openplan-trusted-ca"
else
	cat >"$work/openssl.cnf" <<-'EOF'
		[req]
		distinguished_name = dn
		[dn]
		[ca]
		basicConstraints = critical, CA:TRUE, pathlen:0
		keyUsage = critical, keyCertSign, cRLSign
		subjectKeyIdentifier = hash
		[leaf]
		basicConstraints = critical, CA:FALSE
		keyUsage = critical, digitalSignature, keyEncipherment
		extendedKeyUsage = serverAuth
		subjectAltName = DNS:openplan.localhost, DNS:dex.localhost
	EOF
	openssl req -x509 -new -nodes -newkey rsa:2048 -days 365 \
		-subj "/CN=openplan evaluation CA" \
		-config "$work/openssl.cnf" -extensions ca \
		-keyout "$work/ca.key" -out "$work/ca.crt" 2>/dev/null
	# 365 days: browsers and macOS refuse longer-lived server certificates.
	openssl req -new -nodes -newkey rsa:2048 \
		-subj "/CN=openplan.localhost" -config "$work/openssl.cnf" \
		-keyout "$work/tls.key" -out "$work/tls.csr" 2>/dev/null
	openssl x509 -req -days 365 -in "$work/tls.csr" \
		-CA "$work/ca.crt" -CAkey "$work/ca.key" -CAcreateserial \
		-extfile "$work/openssl.cnf" -extensions leaf \
		-out "$work/tls.crt" 2>/dev/null
	# The CA Secret first: openplan-tls is what marks this step done.
	kube create secret generic openplan-trusted-ca --from-file=ca.crt="$work/ca.crt"
	kube create secret tls openplan-tls --cert="$work/tls.crt" --key="$work/tls.key"
fi

echo "==> dependency passwords"
if secret_exists evaluation-dependencies; then
	echo "kept: evaluation-dependencies"
else
	cat >"$work/dependencies.env" <<-EOF
		postgres-password=$(fresh)
		dex-db-password=$(fresh)
		temporal-db-password=$(fresh)
		valkey-api-password=$(fresh)
		valkey-executor-password=$(fresh)
		minio-root-password=$(fresh)
		oidc-client-secret=$(fresh)
	EOF
	kube create secret generic evaluation-dependencies --from-env-file="$work/dependencies.env"
fi

# The chart's two Secrets, built from the passwords above. Keys are the
# variables' names; the chart sets HTTP_ADDRESS, OPENPLAN_ENVIRONMENT,
# OPENPLAN_PUBLIC_URL and the run directories itself. The executor gets no
# database, no keys and no OIDC secret: secrets reach it sealed per run.
echo "==> openplan-api-env and openplan-executor-env"
s3_password=$(secret_value evaluation-dependencies minio-root-password)
cat >"$work/s3.env" <<-EOF
	ARTIFACT_STORE_KIND=s3
	S3_BUCKET=openplan-artifacts
	S3_REGION=us-east-1
	S3_ENDPOINT=http://minio:9000
	S3_FORCE_PATH_STYLE=true
	S3_ACCESS_KEY_ID=openplan
	S3_SECRET_ACCESS_KEY=$s3_password
	TEMPORAL_ADDRESS=temporal:7233
EOF
if secret_exists openplan-api-env; then
	echo "kept: openplan-api-env"
else
	cat "$work/s3.env" - >"$work/api.env" <<-EOF
		DATABASE_URL=postgres://openplan:$(secret_value evaluation-dependencies postgres-password)@postgres:5432/openplan?sslmode=disable
		VALKEY_URL=redis://api:$(secret_value evaluation-dependencies valkey-api-password)@valkey:6379/0
		CREDENTIAL_ENCRYPTION_KEY=$(fresh)
		SESSION_ENCRYPTION_KEY=$(fresh)
		OIDC_ISSUER_URL=https://dex.localhost/dex
		OIDC_CLIENT_ID=openplan-api
		OIDC_CLIENT_SECRET=$(secret_value evaluation-dependencies oidc-client-secret)
		OPENPLAN_ROOT_SUBJECT=$ROOT_SUBJECT
	EOF
	kube create secret generic openplan-api-env --from-env-file="$work/api.env"
fi
if secret_exists openplan-executor-env; then
	echo "kept: openplan-executor-env"
else
	cat "$work/s3.env" - >"$work/executor.env" <<-EOF
		VALKEY_URL=redis://executor:$(secret_value evaluation-dependencies valkey-executor-password)@valkey:6379/0
	EOF
	kube create secret generic openplan-executor-env --from-env-file="$work/executor.env"
fi

# The api discovers Dex at https://dex.localhost/dex while it boots, the same
# URL the browser uses. In the browser that is loopback, where Docker Desktop
# publishes ingress-nginx; in a pod, this rewrite sends it to the controller's
# Service. answer auto renames the reply back, or a resolver that checks the
# answer's name (Go's does) would discard it.
echo "==> CoreDNS rewrite for dex.localhost"
kubectl -n kube-system get configmap coredns -o 'jsonpath={.data.Corefile}' >"$work/Corefile"
if grep -q 'dex\.localhost' "$work/Corefile"; then
	echo "kept: rewrite already present"
else
	awk '{ print } NR == 1 { print "    rewrite name exact dex.localhost ingress-nginx-controller.ingress-nginx.svc.cluster.local answer auto" }' \
		"$work/Corefile" >"$work/Corefile.new"
	{ printf 'data:\n  Corefile: |\n'; sed 's/^/    /' "$work/Corefile.new"; } >"$work/coredns-patch.yaml"
	kubectl -n kube-system patch configmap coredns --type merge --patch-file "$work/coredns-patch.yaml" >/dev/null
	kubectl -n kube-system rollout restart deployment/coredns >/dev/null
	kubectl -n kube-system rollout status deployment/coredns --timeout=120s
fi

echo "==> dependencies"
kubectl apply -f "$here/dependencies.yaml"
kube wait --for=condition=complete job/postgres-init job/minio-bucket --timeout=300s
kube wait --for=condition=available deployment --all --timeout=300s

cat <<EOF

Ready. Next:

  helm install openplan deploy/helm/openplan -n $NAMESPACE -f deploy/helm/evaluation/values.yaml

Then open https://openplan.localhost and sign in as admin@openplan.local /
admin-local-only. To stop the browser's certificate warning, trust the local
CA (macOS; remove it again from Keychain Access when you're done):

  kubectl -n $NAMESPACE get secret openplan-trusted-ca -o 'jsonpath={.data.ca\.crt}' | base64 -d > openplan-evaluation-ca.crt
  security add-trusted-cert -r trustRoot -k ~/Library/Keychains/login.keychain-db openplan-evaluation-ca.crt
EOF
