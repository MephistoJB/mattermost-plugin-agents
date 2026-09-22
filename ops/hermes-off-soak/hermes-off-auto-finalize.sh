#!/bin/bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-/root/Documents/Programmierung/mattermost-plugin-agents-codex}"
ENV_FILE="${ENV_FILE:-/root/.codex/env}"
LOG_FILE="${LOG_FILE:-/root/Documents/Codex/2026-08-18/ich-habe-eine-fork-von-mattermost/hermes-off-auto-finalize.log}"
LOCK_FILE="${LOCK_FILE:-/tmp/hermes-off-auto-finalize.lock}"
FINALIZE_SCRIPT="${FINALIZE_SCRIPT:-$REPO_DIR/ops/hermes-off-soak/finalize-hermes-off-cutover.sh}"
GO_PATH="${GO_PATH:-/tmp/go1.26.6-1787067914-97967/go/bin}"

timestamp() {
	date -Is
}

log() {
	printf '%s %s\n' "$(timestamp)" "$*" >>"$LOG_FILE"
}

(
	flock -n 9 || exit 0
	cd "$REPO_DIR"

	if [ -f "$ENV_FILE" ]; then
		set -a
		# shellcheck disable=SC1090
		. "$ENV_FILE"
		set +a
	fi

	if [ -z "${MATTERMOST_TOKEN:-}" ] && [ -z "${MM_AGENTS_MATTERMOST_TOKEN:-}" ]; then
		log 'status=error reason=missing_mattermost_token'
		exit 1
	fi

	set +e
	output="$(
		APPLY=1 \
		MM_AGENTS_MATTERMOST_TOKEN="${MM_AGENTS_MATTERMOST_TOKEN:-${MATTERMOST_TOKEN:-}}" \
		PATH="$GO_PATH:$PATH" \
		"$FINALIZE_SCRIPT" 2>&1
	)"
	rc=$?
	set -e

	if [ "$rc" -eq 0 ]; then
		log 'status=finalized'
		printf '%s\n' "$output" >>"$LOG_FILE"
		exit 0
	fi

	if printf '%s\n' "$output" | grep -q 'Hermes-off preflight is not ready'; then
		summary="$(printf '%s\n' "$output" | grep '^preflight=' | tail -n 1 || true)"
		log "status=not_ready ${summary}"
		exit 0
	fi

	log "status=error rc=$rc"
	printf '%s\n' "$output" >>"$LOG_FILE"
	exit "$rc"
) 9>"$LOCK_FILE"
