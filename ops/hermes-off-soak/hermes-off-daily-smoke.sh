#!/bin/bash
set -euo pipefail

LOG="${HERMES_OFF_SOAK_LOG:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-soak.log}"
MATTERMOST_CONTAINER="${MATTERMOST_CONTAINER:-mattermost}"
VOICE_URL="${VOICE_URL:-http://172.17.0.1:8092/health}"
OLLAMA_URL="${OLLAMA_URL:-http://172.17.0.1:11434/api/tags}"
CODEX_HOME="${CODEX_HOME:-/mattermost/data/codex-home}"
CODEX_BIN="${CODEX_BIN:-/mattermost/data/codex-bin/codex}"

timestamp="$(date -Is)"
status="ok"
details=()

check() {
	local name="$1"
	shift
	if "$@" >/tmp/hermes-off-soak-check.out 2>/tmp/hermes-off-soak-check.err; then
		details+=("\"$name\":\"ok\"")
	else
		status="failed"
		local err
		err="$(tr '\n' ' ' </tmp/hermes-off-soak-check.err | cut -c1-240)"
		details+=("\"$name\":\"failed:${err//\"/\\\"}\"")
	fi
}

check mattermost_shell docker exec "$MATTERMOST_CONTAINER" /bin/sh -lc 'echo SHELL_OK'
check codex_app_server docker exec -e CODEX_HOME="$CODEX_HOME" "$MATTERMOST_CONTAINER" "$CODEX_BIN" debug app-server send-message-v2 'Reply exactly SOAK_OK'
check voice_adapter curl -fsS "$VOICE_URL"
check ollama curl -fsS "$OLLAMA_URL"

printf '{"ts":"%s","status":"%s",%s}\n' "$timestamp" "$status" "$(IFS=,; echo "${details[*]}")" >>"$LOG"

if [ "$status" != "ok" ]; then
	exit 1
fi
