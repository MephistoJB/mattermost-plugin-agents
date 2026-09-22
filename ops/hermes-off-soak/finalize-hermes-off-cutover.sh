#!/bin/bash
set -euo pipefail

MATTERMOST_URL="${MATTERMOST_URL:-${MM_AGENTS_MATTERMOST_URL:-http://100.97.50.7:8065}}"
MATTERMOST_USER_ID="${MATTERMOST_USER_ID:-${MM_AGENTS_MATTERMOST_USER_ID:-8rzr4dktkbr5xn9a78h7s8xejr}}"
MATTERMOST_TOKEN="${MATTERMOST_TOKEN:-${MM_AGENTS_MATTERMOST_TOKEN:-}}"
PREFLIGHT_HOST="${PREFLIGHT_HOST:-root@192.168.1.16}"
PREFLIGHT_SCRIPT="${PREFLIGHT_SCRIPT:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-cutover-preflight.sh}"
PREFLIGHT_OUTPUT="${PREFLIGHT_OUTPUT:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-cutover-preflight-last.json}"
APPLY="${APPLY:-0}"

if [ -z "$MATTERMOST_TOKEN" ]; then
	echo "MATTERMOST_TOKEN or MM_AGENTS_MATTERMOST_TOKEN is required" >&2
	exit 1
fi

MATTERMOST_URL="${MATTERMOST_URL%/}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

ssh "$PREFLIGHT_HOST" "$PREFLIGHT_SCRIPT > '$PREFLIGHT_OUTPUT'; rc=\$?; cat '$PREFLIGHT_OUTPUT'; exit \$rc" >"$tmpdir/preflight.json" || preflight_rc=$?
preflight_rc="${preflight_rc:-0}"

summary="$(node - "$tmpdir/preflight.json" <<'NODE'
const fs = require('fs');
const p = process.argv[2];
const j = JSON.parse(fs.readFileSync(p, 'utf8'));
console.log(JSON.stringify({
  ready: j.ready,
  infraReady: j.infraReady,
  soakReady: j.soak?.ready,
  productionChannelsReady: j.productionChannelsReady,
  hermesAdapterReady: j.hermesAdapterReady,
  elapsedDays: j.soak?.elapsedDays,
  okDays: j.soak?.okDays,
  failedEntryCount: j.soak?.failedEntryCount,
}));
NODE
)"
echo "preflight=$summary"

ready="$(node -e 'const j=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")); console.log(j.ready ? "1" : "0")' "$tmpdir/preflight.json")"
if [ "$ready" != "1" ]; then
	echo "Hermes-off preflight is not ready; not closing soak gate." >&2
	exit "${preflight_rc:-2}"
fi

if [ "$APPLY" != "1" ]; then
	echo "Preflight is ready. Re-run with APPLY=1 to close the soak checklist gate and run final require-ready smoke."
	exit 0
fi

detail="$(node - "$tmpdir/preflight.json" <<'NODE'
const fs = require('fs');
const j = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const okDays = (j.soak?.okDays || []).join(', ');
console.log(`OK: 7-day Hermes-Off soak completed. Preflight reported ready=true, infraReady=${j.infraReady}, productionChannelsReady=${j.productionChannelsReady}, hermesAdapterReady=${j.hermesAdapterReady}, elapsedDays=${j.soak?.elapsedDays}, okDays=${okDays}, failedEntryCount=${j.soak?.failedEntryCount}. Last preflight JSON: /mnt/cache/appdata/mattermost/codex-ops/hermes-off-cutover-preflight-last.json`);
NODE
)"

node -e 'console.log(JSON.stringify({status:"ok", detail: process.argv[1]}))' "$detail" >"$tmpdir/soak-ok.json"
curl -fsS -X PUT \
	-H "Authorization: Bearer $MATTERMOST_TOKEN" \
	-H "Mattermost-User-Id: $MATTERMOST_USER_ID" \
	-H 'Content-Type: application/json' \
	--data-binary @"$tmpdir/soak-ok.json" \
	"$MATTERMOST_URL/plugins/mattermost-ai/admin/runtime/hermes-off-checklist/test_channel_soak" >/dev/null

MM_AGENTS_HERMES_OFF_E2E_SMOKE=1 \
MM_AGENTS_HERMES_OFF_REQUIRE_READY=1 \
MM_AGENTS_MATTERMOST_URL="$MATTERMOST_URL" \
MM_AGENTS_MATTERMOST_TOKEN="$MATTERMOST_TOKEN" \
MM_AGENTS_MATTERMOST_USER_ID="$MATTERMOST_USER_ID" \
go test ./server -run TestRealMattermostHermesOffChecklistSmoke -v

echo "Hermes-off cutover checklist is ready."
