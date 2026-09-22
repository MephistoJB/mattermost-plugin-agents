#!/bin/bash
set -euo pipefail

MATTERMOST_CONTAINER="${MATTERMOST_CONTAINER:-mattermost}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgresql17}"
MATTERMOST_DB="${MATTERMOST_DB:-mattermost}"
N8N_DB="${N8N_DB:-n8n}"
N8N_DEV_DB="${N8N_DEV_DB:-n8n_dev}"
SOAK_STATUS_SCRIPT="${SOAK_STATUS_SCRIPT:-/mnt/cache/appdata/mattermost/codex-ops/hermes-off-soak-status.sh}"
VOICE_URL="${VOICE_URL:-http://172.17.0.1:8092/health}"
OLLAMA_URL="${OLLAMA_URL:-http://172.17.0.1:11434/api/tags}"
CODEX_HOME="${CODEX_HOME:-/mattermost/data/codex-home}"
CODEX_BIN="${CODEX_BIN:-/mattermost/data/codex-bin/codex}"
AGENT_BOT_USER_ID="${AGENT_BOT_USER_ID:-xhc5jx38npna9qq9y7zxjfyjxh}"
AGENT_USERNAME="${AGENT_USERNAME:-local-transcriber}"
TEST_CHANNELS_CSV="${TEST_CHANNELS_CSV:-codex-hermes-off-test,codex-hermes-off-cloud-test}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

json_escape() {
	node -e 'process.stdout.write(JSON.stringify(process.argv[1] || ""))' "$1"
}

check_command() {
	local name="$1"
	shift
	if "$@" >"$tmpdir/$name.out" 2>"$tmpdir/$name.err"; then
		printf '"%s":{"ok":true}' "$name"
	else
		local err
		err="$(tr '\n' ' ' <"$tmpdir/$name.err" | cut -c1-300)"
		printf '"%s":{"ok":false,"error":%s}' "$name" "$(json_escape "$err")"
	fi
}

psql_at() {
	docker exec "$POSTGRES_CONTAINER" psql -U postgres -d "$1" -P pager=off -F $'\t' -Atc "$2"
}

soak_json="$("$SOAK_STATUS_SCRIPT" 2>/dev/null || printf '{"ready":false,"error":"soak status script failed"}')"

test_channel_names_sql="$(printf '%s' "$TEST_CHANNELS_CSV" | sed "s/,/','/g")"
non_test_usage="$(psql_at "$MATTERMOST_DB" "
with usage as (
  select c.id, c.name, c.displayname, c.type,
         count(*) filter (where p.userid='${AGENT_BOT_USER_ID}') as bot_posts,
         count(*) filter (where lower(p.message) like '%@${AGENT_USERNAME}%') as mentions,
         max(p.createat) as last_post_at
  from posts p
  join channels c on c.id=p.channelid
  where p.deleteat=0 and (p.userid='${AGENT_BOT_USER_ID}' or lower(p.message) like '%@${AGENT_USERNAME}%')
  group by c.id, c.name, c.displayname, c.type
)
select id || '|' || name || '|' || displayname || '|' || type || '|' || bot_posts || '|' || mentions || '|' || last_post_at
from usage
where name not in ('${test_channel_names_sql}')
order by last_post_at desc;
" 2>/dev/null || true)"

test_usage="$(psql_at "$MATTERMOST_DB" "
with usage as (
  select c.id, c.name, c.displayname, c.type,
         count(*) filter (where p.userid='${AGENT_BOT_USER_ID}') as bot_posts,
         count(*) filter (where lower(p.message) like '%@${AGENT_USERNAME}%') as mentions,
         max(p.createat) as last_post_at
  from posts p
  join channels c on c.id=p.channelid
  where p.deleteat=0 and (p.userid='${AGENT_BOT_USER_ID}' or lower(p.message) like '%@${AGENT_USERNAME}%')
  group by c.id, c.name, c.displayname, c.type
)
select id || '|' || name || '|' || displayname || '|' || type || '|' || bot_posts || '|' || mentions || '|' || last_post_at
from usage
where name in ('${test_channel_names_sql}')
order by last_post_at desc;
" 2>/dev/null || true)"

runtime_policies="$(psql_at "$MATTERMOST_DB" "
select scopetype || '|' || scopeid || '|' || runtimetype || '|' || providerid || '|' || model || '|' || allowcloud || '|' || allowlocal
from agents_runtimepolicies
order by scopetype, scopeid;
" 2>/dev/null || true)"

n8n_hermes_count="$(psql_at "$N8N_DB" "select count(*) from workflow_entity where lower(name) like '%hermes%' or lower(nodes::text) like '%hermes%' or lower(connections::text) like '%hermes%';" 2>/dev/null || echo unknown)"
n8n_dev_hermes_count="$(psql_at "$N8N_DEV_DB" "select count(*) from workflow_entity where lower(name) like '%hermes%' or lower(nodes::text) like '%hermes%' or lower(connections::text) like '%hermes%';" 2>/dev/null || echo unknown)"
docker_hermes_count="$(docker ps -a --format '{{.Names}} {{.Image}} {{.Command}}' | grep -ci hermes || true)"
process_hermes_count="$(
	ps ax -o comm=,args= |
		awk 'BEGIN { count=0 } /[Hh][Ee][Rr][Mm][Ee][Ss]/ && $0 !~ /hermes-off-/ && $0 !~ /hermesAdapterReady/ { count++ } END { print count }'
)"
mattermost_env_hermes_count="$(docker exec "$MATTERMOST_CONTAINER" /bin/sh -lc 'env | grep -ci hermes || true' 2>/dev/null || echo unknown)"

checks_json="$(
	printf '{'
	check_command mattermost_shell docker exec "$MATTERMOST_CONTAINER" /bin/sh -lc 'echo SHELL_OK'
	printf ','
	check_command codex_app_server docker exec -e CODEX_HOME="$CODEX_HOME" "$MATTERMOST_CONTAINER" "$CODEX_BIN" debug app-server send-message-v2 'Reply exactly PREFLIGHT_OK'
	printf ','
	check_command voice_adapter curl -fsS "$VOICE_URL"
	printf ','
	check_command ollama curl -fsS "$OLLAMA_URL"
	printf '}'
)"

node - "$soak_json" "$checks_json" "$non_test_usage" "$test_usage" "$runtime_policies" "$n8n_hermes_count" "$n8n_dev_hermes_count" "$docker_hermes_count" "$process_hermes_count" "$mattermost_env_hermes_count" <<'NODE'
const [
  soakRaw,
  checksRaw,
  nonTestUsageRaw,
  testUsageRaw,
  policiesRaw,
  n8nHermesCountRaw,
  n8nDevHermesCountRaw,
  dockerHermesCountRaw,
  processHermesCountRaw,
  mattermostEnvHermesCountRaw,
] = process.argv.slice(2);

function parseRows(raw, fields) {
  return String(raw || '').split(/\r?\n/).filter(Boolean).map((line) => {
    const values = line.split('|');
    return Object.fromEntries(fields.map((field, index) => [field, values[index] ?? '']));
  });
}

const soak = JSON.parse(soakRaw);
const checks = JSON.parse(checksRaw);
const nonTestUsage = parseRows(nonTestUsageRaw, ['channelID', 'name', 'displayName', 'type', 'botPosts', 'mentions', 'lastPostAt']);
const testUsage = parseRows(testUsageRaw, ['channelID', 'name', 'displayName', 'type', 'botPosts', 'mentions', 'lastPostAt']);
const runtimePolicies = parseRows(policiesRaw, ['scopeType', 'scopeID', 'runtimeType', 'providerID', 'model', 'allowCloud', 'allowLocal']);
const hermes = {
  dockerCount: Number(dockerHermesCountRaw),
  processCount: Number(processHermesCountRaw),
  mattermostEnvCount: Number(mattermostEnvHermesCountRaw),
  n8nWorkflowCount: Number(n8nHermesCountRaw),
  n8nDevWorkflowCount: Number(n8nDevHermesCountRaw),
};
const activeHermesSurface = Object.values(hermes).some((value) => Number.isFinite(value) ? value > 0 : true);
const infraReady = Object.values(checks).every((check) => check.ok);
const productionChannelsReady = nonTestUsage.length === 0;
const hermesAdapterReady = !activeHermesSurface;
const ready = Boolean(soak.ready && infraReady && productionChannelsReady && hermesAdapterReady);

console.log(JSON.stringify({
  ready,
  generatedAt: new Date().toISOString(),
  infraReady,
  soak,
  productionChannelsReady,
  nonTestUsage,
  testUsage,
  runtimePolicies,
  hermesAdapterReady,
  hermes,
  checks,
}, null, 2));
process.exit(ready ? 0 : 2);
NODE
