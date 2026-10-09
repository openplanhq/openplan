#!/bin/sh
# Scenario 5: the executor refuses to start when it cannot keep branches
# apart. Each case must exit non-zero and say why, before dialling Temporal.
set -eu
image="$1"
secure="--cap-drop ALL --cap-add SETUID --cap-add SETGID --cap-add CHOWN --security-opt no-new-privileges:true"

expect_refusal() {
  name="$1"; want="$2"; shift 2
  if output=$(docker run --rm -e TEMPORAL_ADDRESS=unused:7233 "$@" "$image" 2>&1); then
    echo "FAIL: $name: the executor started"; exit 1
  fi
  case "$output" in
    *"$want"*) echo "ok: $name" ;;
    *) echo "FAIL: $name: want \"$want\" in: $output"; exit 1 ;;
  esac
}

# shellcheck disable=SC2086 # $secure is a list of flags
expect_refusal "without no-new-privileges" "no_new_privs" --cap-drop ALL --cap-add SETUID --cap-add SETGID --cap-add CHOWN
expect_refusal "with Docker's default capabilities" "cap_drop"
# shellcheck disable=SC2086
expect_refusal "with 21 sessions" "EXECUTOR_MAX_SESSIONS" $secure -e EXECUTOR_MAX_SESSIONS=21
