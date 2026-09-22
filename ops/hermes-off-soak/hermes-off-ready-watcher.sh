#!/bin/bash
set -euo pipefail

PREFLIGHT_SCRIPT="${PREFLIGHT_SCRIPT:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-cutover-preflight.sh}"
PREFLIGHT_OUTPUT="${PREFLIGHT_OUTPUT:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-cutover-preflight-last.json}"
READY_FILE="${READY_FILE:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-ready.flag}"
WATCH_LOG="${WATCH_LOG:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-ready-watcher.log}"

timestamp="$(date -Is)"
if "$PREFLIGHT_SCRIPT" >"$PREFLIGHT_OUTPUT"; then
	status="ready"
else
	status="not_ready"
fi

summary="$(
	node - "$PREFLIGHT_OUTPUT" <<'NODE'
const fs = require('fs');
const path = process.argv[2];
try {
  const j = JSON.parse(fs.readFileSync(path, 'utf8'));
  process.stdout.write(JSON.stringify({
    ready: j.ready,
    infraReady: j.infraReady,
    soakReady: j.soak?.ready,
    productionChannelsReady: j.productionChannelsReady,
    hermesAdapterReady: j.hermesAdapterReady,
    elapsedDays: j.soak?.elapsedDays,
    okDays: j.soak?.okDays,
    failedEntryCount: j.soak?.failedEntryCount,
  }));
} catch (error) {
  process.stdout.write(JSON.stringify({ready: false, error: String(error.message || error)}));
}
NODE
)"

printf '{"ts":"%s","status":"%s","summary":%s}\n' "$timestamp" "$status" "$summary" >>"$WATCH_LOG"

if [ "$status" = "ready" ]; then
	printf 'Hermes-Off preflight ready at %s\n%s\n' "$timestamp" "$summary" >"$READY_FILE"
fi
