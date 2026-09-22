#!/bin/bash
set -euo pipefail

LOG="${HERMES_OFF_SOAK_LOG:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-soak.log}"
START_ISO="${HERMES_OFF_SOAK_START_ISO:-2026-08-20T21:34:44+02:00}"
REQUIRED_DAYS="${HERMES_OFF_SOAK_REQUIRED_DAYS:-7}"

if [ ! -f "$LOG" ]; then
	echo "{\"ready\":false,\"reason\":\"missing log\",\"log\":\"$LOG\"}"
	exit 0
fi

node - "$LOG" "$START_ISO" "$REQUIRED_DAYS" <<'NODE'
const fs = require('fs');
const [logPath, startISO, requiredDaysRaw] = process.argv.slice(2);
const requiredDays = Number(requiredDaysRaw || 7);
const start = new Date(startISO);
const now = new Date();
const entries = [];

for (const line of fs.readFileSync(logPath, 'utf8').split(/\r?\n/)) {
  if (!line.trim()) continue;
  try {
    const entry = JSON.parse(line);
    const ts = new Date(entry.ts);
    if (!Number.isNaN(ts.getTime()) && ts >= start) {
      entries.push({ts, entry});
    }
  } catch {
    // Ignore malformed log lines; the summary exposes failed/missing coverage.
  }
}

const okEntries = entries.filter(({entry}) => entry.status === 'ok');
const failedEntries = entries.filter(({entry}) => entry.status !== 'ok');
const okDays = [...new Set(okEntries.map(({ts}) => ts.toISOString().slice(0, 10)))].sort();
const elapsedDays = Math.max(0, (now.getTime() - start.getTime()) / 86400000);
const ready = elapsedDays >= requiredDays && okDays.length >= requiredDays && failedEntries.length === 0;

console.log(JSON.stringify({
  ready,
  start: start.toISOString(),
  now: now.toISOString(),
  requiredDays,
  elapsedDays: Math.round(elapsedDays * 1000) / 1000,
  okDays,
  okEntryCount: okEntries.length,
  failedEntryCount: failedEntries.length,
  lastEntry: entries.length ? entries[entries.length - 1].entry : null,
}));
NODE
