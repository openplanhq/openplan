#!/usr/bin/env bash
# Runs what CI runs (.github/workflows/ci.yml), on a Mac or on Linux, with
# nothing installed but Docker. Iterate locally, push once it passes, and
# leave GitHub to check the PR when it leaves draft.
#
#   scripts/check.sh      (or `make check`)
#
# Every job runs in a Linux container: cmd/executor builds only for Linux, and
# the containers pin the Go, Node and Helm that CI uses rather than whatever the
# host has. Nothing is written to the checkout. The web and site builds work on
# a copy, so the host's node_modules (built for macOS) are never touched.
#
# Not covered: workflow-replay.yml, which needs Temporal and a release checkout;
# run `make workflow-replay` when a change touches internal/workflows. And the
# images are built for this machine's architecture, arm64 on a Mac, where CI
# builds amd64.
set -euo pipefail

cd "$(dirname "$0")/.."

go_image=golang:$(sed -n 's/^toolchain go//p' go.mod)
# The same Node as CI's web and site jobs, and the same Helm as its helm job.
node_image=node:22
helm_image=alpine/helm:4.1.1

# Module, build and npm caches outlive the run, so the second run is fast. They
# sit in a host directory, not a named volume, so the containers can run as the
# host user: some tests skip or fail as root.
cache=${OPENPLAN_CHECK_CACHE:-$HOME/.cache/openplan-check}
mkdir -p "$cache/gomod" "$cache/gocache" "$cache/npm"

# A Postgres of its own on a network of its own, like CI's service container:
# the run never touches the Compose database, and two runs never share one.
run=openplan-check-$$
cleanup() {
  docker rm -f "$run-pg" >/dev/null 2>&1 || true
  docker network rm "$run" >/dev/null 2>&1 || true
}
trap cleanup EXIT

step() { printf '\n==> %s\n' "$*"; }

as_user=(--rm --user "$(id -u):$(id -g)" -e HOME=/tmp)

in_go() {
  # -buildvcs=false: in a git worktree .git points outside the mount, and the
  # Go build would fail reading VCS status it doesn't need.
  docker run "${as_user[@]}" --network "$run" \
    -v "$PWD:/src:ro" -w /src \
    -v "$cache/gomod:/cache/gomod" -v "$cache/gocache:/cache/gocache" \
    -e GOMODCACHE=/cache/gomod -e GOCACHE=/cache/gocache \
    -e GOFLAGS=-buildvcs=false -e GOTOOLCHAIN=local \
    -e OPENPLAN_POSTGRES_TEST_DSN="postgres://openplan:openplan@$run-pg:5432/openplan_test?sslmode=disable" \
    "$go_image" "$@"
}

# npm ci and the build run in a copy under /tmp: the checkout is mounted
# read-only and its node_modules and dist are left out. The whole repository is
# copied, not just the one directory, because the site imports web's theme.
in_node() {
  local dir=$1
  shift
  docker run "${as_user[@]}" \
    -v "$PWD:/src:ro" \
    -v "$cache/npm:/cache/npm" -e npm_config_cache=/cache/npm \
    "$node_image" sh -c '
      set -e
      mkdir /tmp/repo
      tar -C /src --exclude=node_modules --exclude=dist --exclude=.git --exclude=.worktrees -cf - . | tar -C /tmp/repo -xf -
      cd /tmp/repo/'"$dir"'
      '"$*"
}

docker network create "$run" >/dev/null
# Throwaway credentials, the same as CI's service database.
docker run -d --name "$run-pg" --network "$run" \
  -e POSTGRES_USER=openplan -e POSTGRES_PASSWORD=openplan -e POSTGRES_DB=openplan_test \
  postgres:16-alpine >/dev/null

# Fastest first, so a broken chart or build fails in seconds, not after the
# Go tests.
step "helm (lint, template)"
docker run --rm -v "$PWD:/src:ro" -w /src --entrypoint sh "$helm_image" scripts/helm-check.sh

step "site (build)"
in_node site 'npm ci && npm run build'

step "web (test, build)"
in_node web 'npm ci && npm test && npm run build'

step "backend (go build, vet, test)"
until docker exec "$run-pg" pg_isready -U openplan -d openplan_test >/dev/null 2>&1; do sleep 1; done
in_go sh -c 'go build ./... && go vet ./... && go test ./...'

step "lint (golangci-lint)"
in_go go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...

step "vulncheck (govulncheck)"
in_go go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

# make sandbox-test rebuilds its two images under the same tags, which leaves
# the previous build behind untagged. Remove those, and only those: a blanket
# `docker image prune` would also take other projects' images.
step "sandbox (executor image, #245)"
old_images=$(docker image inspect -f '{{.Id}}' openplan-executor:sandbox openplan-sandbox-test 2>/dev/null || true)
make sandbox-test
for id in $old_images; do
  # Still tagged means the build reused it; rmi refuses one a container uses.
  if [ -z "$(docker image inspect -f '{{join .RepoTags ""}}' "$id" 2>/dev/null)" ]; then
    docker rmi "$id" >/dev/null 2>&1 || true
  fi
done

step "all checks passed"
