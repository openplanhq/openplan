#!/bin/sh
# Lints and renders the Helm chart, then checks that an install missing a
# required value stops at template time and names the key, instead of
# rendering a Deployment that crash-loops. Helm's lint only warns about a
# missing required value, so this checks it through template, one key at a
# time.
#
# CI's helm job and `make check` both run this, so they can't disagree about
# whether the chart passes. POSIX sh: `make check` runs it in the alpine/helm
# image, which has no bash.
set -eu

chart=deploy/helm/openplan
# ci/values.yaml fills the required values with placeholders; without them
# the templates that need them cannot render.
values=$chart/ci/values.yaml

helm lint --strict "$chart" -f "$values"
helm template openplan "$chart" -f "$values" >/dev/null

# Each Secret reference is required twice over: its name and its key.
refs="postgres.urlSecret valkey.api.urlSecret valkey.executor.urlSecret s3.accessKeyIDSecret s3.secretAccessKeySecret oidc.clientSecretSecret api.sessionEncryptionKeySecret api.credentialEncryptionKeySecret"
keys="temporal.address s3.bucket s3.region oidc.issuerURL oidc.clientID api.rootSubject ingress.host ingress.tlsSecretName"
for ref in $refs; do keys="$keys $ref.name $ref.key"; done
for key in $keys; do
  if out=$(helm template openplan "$chart" -f "$values" --set "$key=" 2>&1); then
    echo "helm template succeeded with $key empty"
    exit 1
  fi
  if ! printf '%s\n' "$out" | grep -qF "$key is required"; then
    echo "helm template failed with $key empty but did not name it:"
    printf '%s\n' "$out"
    exit 1
  fi
done
