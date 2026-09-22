#!/bin/bash
set -euo pipefail

CONTAINER="${MATTERMOST_CONTAINER:-mattermost}"
BUSYBOX="${BUSYBOX_STATIC:-/share/Docker/mattermost/data/codex-busybox-static}"
LOG="${CODEX_SHELL_GUARD_LOG:-/share/Docker/mattermost/data/codex-shell-guard.log}"

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
	exit 0
fi
if [ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || true)" != "true" ]; then
	exit 0
fi
if docker exec "$CONTAINER" /bin/sh -lc 'test -x /bin/sh && echo ok' >/dev/null 2>&1; then
	exit 0
fi
if [ ! -x "$BUSYBOX" ]; then
	echo "$(date -Is) missing busybox static binary: $BUSYBOX" >>"$LOG"
	exit 1
fi

docker cp "$BUSYBOX" "$CONTAINER:/bin/busybox"
docker exec -u 0 "$CONTAINER" /bin/busybox ln -s /bin/busybox /bin/sh
docker exec "$CONTAINER" /bin/sh -lc 'echo SHELL_OK' >/dev/null
echo "$(date -Is) repaired /bin/sh in $CONTAINER" >>"$LOG"
