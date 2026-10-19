#!/usr/bin/env bash
# Replays the workflow histories a release records against the workflows in
# this checkout. Temporal replays a running workflow against whatever code the
# worker has now, so a history that no longer replays is a run that release
# would have in flight when an install upgrades, failed halfway. See "Changing
# a workflow" in CONTRIBUTING.md.
#
#   scripts/workflow-replay.sh [ref]
#
# ref is the code that records, checked out on its own; it defaults to the
# last release tag reachable from HEAD. The replay runs against the working
# tree. Nothing recorded is kept: the histories live in a temporary directory.
#
# Needs a Temporal server at TEMPORAL_ADDRESS (default localhost:7233), such as
# Compose's: `docker compose up -d --wait temporal`.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
ref=${1:-$(git -C "$root" describe --tags --abbrev=0 --match 'v[0-9]*' HEAD)}

work=$(mktemp -d)
cleanup() {
	git -C "$root" worktree remove --force "$work/source" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT

git -C "$root" worktree add --quiet --detach "$work/source" "$ref"

# A release from before the recorder has no histories to give, and was
# allowed to need a wipe on upgrade anyway.
if [[ ! -f "$work/source/internal/workflows/history_record_test.go" ]]; then
	echo "$ref has no workflow history recorder: nothing to replay"
	exit 0
fi

echo "recording workflow histories at $ref"
(cd "$work/source" && OPENPLAN_RECORD_HISTORIES="$work/histories" \
	go test ./internal/workflows/ -run '^TestRecordHistories$' -count=1)

echo "replaying them against the working tree"
(cd "$root" && OPENPLAN_REPLAY_HISTORIES="$work/histories" \
	go test ./internal/workflows/ -run '^TestReplayRecordedHistories$' -count=1 -v)
