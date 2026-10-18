#!/bin/sh
# The executor refuses to start when it cannot start sandboxes, or when the
# programs they run couldn't, before it dials Temporal.
set -eu
image="$1"
if output=$(docker run --rm -e TEMPORAL_ADDRESS=unused:7233 -e VALKEY_URL=redis://unused:6379/0 \
    --cap-drop ALL --cap-add SETUID --cap-add SETGID --cap-add CHOWN \
    --security-opt no-new-privileges:true "$image" 2>&1); then
  echo "FAIL: the executor started without SYS_ADMIN"; exit 1
fi
case "$output" in
  *"sandbox:"*) echo "ok: refuses to start without what runsc needs" ;;
  *) echo "FAIL: want \"sandbox:\" in: $output"; exit 1 ;;
esac

# Every privilege, but a run root mounted noexec, as Docker mounts a tmpfs by
# default: every provider would segfault.
if output=$(docker run --rm -e TEMPORAL_ADDRESS=unused:7233 -e VALKEY_URL=redis://unused:6379/0 -e EXECUTOR_RUN_ROOT=/var/lib/openplan/runs \
    --cap-drop ALL --cap-add SYS_ADMIN --cap-add SETUID --cap-add SETGID --cap-add SETFCAP --cap-add CHOWN \
    --cap-add NET_ADMIN --cap-add NET_RAW --sysctl net.ipv4.ip_forward=1 \
    --security-opt no-new-privileges:true --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
    --cgroupns private --tmpfs /var/lib/openplan/runs "$image" 2>&1); then
  echo "FAIL: the executor started on a noexec run root"; exit 1
fi
case "$output" in
  *"noexec"*) echo "ok: refuses a run root that can't run providers" ;;
  *) echo "FAIL: want \"noexec\" in: $output"; exit 1 ;;
esac
