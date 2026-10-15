#!/bin/sh
# The executor refuses to start when it cannot start sandboxes, before it
# dials Temporal.
set -eu
image="$1"
if output=$(docker run --rm -e TEMPORAL_ADDRESS=unused:7233 --cap-drop ALL --cap-add SETUID --cap-add SETGID --cap-add CHOWN \
    --security-opt no-new-privileges:true "$image" 2>&1); then
  echo "FAIL: the executor started without SYS_ADMIN"; exit 1
fi
case "$output" in
  *"sandbox:"*) echo "ok: refuses to start without what runsc needs" ;;
  *) echo "FAIL: want \"sandbox:\" in: $output"; exit 1 ;;
esac
