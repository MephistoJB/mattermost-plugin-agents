#!/bin/bash
set -euo pipefail

ENV_FILE="${ENV_FILE:-/root/.codex/env}"
LOG_FILE="${LOG_FILE:-/root/Documents/Codex/2026-08-18/ich-habe-eine-fork-von-mattermost/switch-local-llm-to-macserver.log}"
MATTERMOST_USER_ID="${MATTERMOST_USER_ID:-8rzr4dktkbr5xn9a78h7s8xejr}"
LOCAL_AGENT_ID="${LOCAL_AGENT_ID:-zbou3lrsxe0ehgh7aiy1n8giuc}"
LOCAL_AGENT_BOT_USER_ID="${LOCAL_AGENT_BOT_USER_ID:-asbl9eogv2aapjehwagexktocb}"
LOCAL_CHANNEL_ID="${LOCAL_CHANNEL_ID:-idndurujob88dgd6kthipei7wa}"
WORKSPACE_POLICY_ID="${WORKSPACE_POLICY_ID:-1ahp696kji83mkeo6fu1fqzbph}"
MODEL="${MODEL:-gemma4:26b-mlx}"
LOCAL_PROVIDER_ID="${LOCAL_PROVIDER_ID:-local}"
LOCAL_SERVICE_NAME="${LOCAL_SERVICE_NAME:-Macserver Gemma4 MLX OpenAI Compatible}"
LOCAL_SERVICE_URL="${LOCAL_SERVICE_URL:-http://macserver:11434/v1}"
LOCAL_SERVICE_SMOKE_URL="${LOCAL_SERVICE_SMOKE_URL:-$LOCAL_SERVICE_URL}"
LOCAL_STREAMING_TIMEOUT_SECONDS="${LOCAL_STREAMING_TIMEOUT_SECONDS:-600}"
LOCAL_OUTPUT_TOKEN_LIMIT="${LOCAL_OUTPUT_TOKEN_LIMIT:-4096}"
CODEX_PROVIDER_ID="${CODEX_PROVIDER_ID:-codex}"
CODEX_SERVICE_NAME="${CODEX_SERVICE_NAME:-Codex Cloud}"
CODEX_MODEL="${CODEX_MODEL:-gpt-5-codex}"
MATTERMOST_URLS="${MATTERMOST_URLS:-http://100.97.50.7:8065 https://chat.bechara.org http://192.168.1.16:8065}"
SELF_DISABLE_CRON="${SELF_DISABLE_CRON:-1}"

timestamp() {
	date -Is
}

log() {
	printf '%s %s\n' "$(timestamp)" "$*" >>"$LOG_FILE"
}

if [ -f "$ENV_FILE" ]; then
	set -a
	# shellcheck disable=SC1090
	. "$ENV_FILE"
	set +a
fi

TOKEN="${MM_AGENTS_MATTERMOST_TOKEN:-${MATTERMOST_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
	log 'status=error reason=missing_mattermost_token'
	echo 'MATTERMOST_TOKEN or MM_AGENTS_MATTERMOST_TOKEN is required' >&2
	exit 1
fi

if ! curl -fsS --max-time 10 "$LOCAL_SERVICE_SMOKE_URL/models" >/dev/null; then
	if [ "$LOCAL_SERVICE_SMOKE_URL" = "$LOCAL_SERVICE_URL" ]; then
		case "$LOCAL_SERVICE_URL" in
			*://macserver:*)
				LOCAL_SERVICE_SMOKE_URL="${LOCAL_SERVICE_URL/://macserver:/://macserver.bechara.org:}"
				;;
		esac
	fi
fi

if ! curl -fsS --max-time 10 "$LOCAL_SERVICE_SMOKE_URL/models" >/dev/null; then
	log "status=error reason=local_service_unreachable url=$LOCAL_SERVICE_URL"
	echo "Local LLM endpoint is not reachable: $LOCAL_SERVICE_URL" >&2
	exit 1
fi

if ! curl -fsS --max-time 30 "$LOCAL_SERVICE_SMOKE_URL/chat/completions" \
	-H 'Content-Type: application/json' \
	-d "{\"model\":\"$MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply exactly MACSERVER_READY\"}],\"stream\":false}" |
	node -e 'let s="";process.stdin.on("data",d=>s+=d);process.stdin.on("end",()=>{const j=JSON.parse(s); if (j.model !== process.argv[1]) process.exit(2); const c=String(j.choices?.[0]?.message?.content || ""); if (!c.includes("MACSERVER_READY")) process.exit(3);})' "$MODEL"; then
	log "status=error reason=local_model_smoke_failed model=$MODEL"
	echo "Local LLM model smoke failed: $MODEL" >&2
	exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

for url in $MATTERMOST_URLS; do
	base="${url%/}"
	if ! curl -fsS --max-time 8 "$base/api/v4/system/ping" >/dev/null; then
		log "status=skip reason=mattermost_unreachable url=$base"
		continue
	fi

	set +e
	BASE_URL="$base" TOKEN="$TOKEN" MATTERMOST_USER_ID="$MATTERMOST_USER_ID" \
	LOCAL_AGENT_ID="$LOCAL_AGENT_ID" LOCAL_AGENT_BOT_USER_ID="$LOCAL_AGENT_BOT_USER_ID" \
	LOCAL_CHANNEL_ID="$LOCAL_CHANNEL_ID" WORKSPACE_POLICY_ID="$WORKSPACE_POLICY_ID" \
	LOCAL_PROVIDER_ID="$LOCAL_PROVIDER_ID" LOCAL_SERVICE_NAME="$LOCAL_SERVICE_NAME" \
	LOCAL_SERVICE_URL="$LOCAL_SERVICE_URL" MODEL="$MODEL" \
	LOCAL_STREAMING_TIMEOUT_SECONDS="$LOCAL_STREAMING_TIMEOUT_SECONDS" \
	LOCAL_OUTPUT_TOKEN_LIMIT="$LOCAL_OUTPUT_TOKEN_LIMIT" \
	CODEX_PROVIDER_ID="$CODEX_PROVIDER_ID" CODEX_SERVICE_NAME="$CODEX_SERVICE_NAME" \
	CODEX_MODEL="$CODEX_MODEL" node <<'NODE'
const base = process.env.BASE_URL.replace(/\/$/, '');
const token = process.env.TOKEN;
const userID = process.env.MATTERMOST_USER_ID;
const model = process.env.MODEL;
const serviceID = process.env.LOCAL_PROVIDER_ID;
const serviceName = process.env.LOCAL_SERVICE_NAME;
const serviceURL = process.env.LOCAL_SERVICE_URL;
const streamingTimeoutSeconds = Number(process.env.LOCAL_STREAMING_TIMEOUT_SECONDS || 600);
const outputTokenLimit = Number(process.env.LOCAL_OUTPUT_TOKEN_LIMIT || 4096);
const codexServiceID = process.env.CODEX_PROVIDER_ID;
const codexServiceName = process.env.CODEX_SERVICE_NAME;
const codexModel = process.env.CODEX_MODEL;
const localAgentID = process.env.LOCAL_AGENT_ID;
const localAgentBotUserID = process.env.LOCAL_AGENT_BOT_USER_ID;
const localChannelID = process.env.LOCAL_CHANNEL_ID;
const workspacePolicyID = process.env.WORKSPACE_POLICY_ID;

async function request(path, opts = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 20000);
  try {
    const res = await fetch(`${base}${path}`, {
      ...opts,
      signal: controller.signal,
      headers: {
        Authorization: `Bearer ${token}`,
        'Mattermost-User-Id': userID,
        'Content-Type': 'application/json',
        ...(opts.headers || {}),
      },
    });
    const text = await res.text();
    if (!res.ok) {
      throw new Error(`${opts.method || 'GET'} ${path} ${res.status}: ${text}`);
    }
    return text ? JSON.parse(text) : null;
  } finally {
    clearTimeout(timeout);
  }
}

function localPolicy(scopeType, scopeID) {
  return {
    scopeType,
    scopeID,
    runtimeType: 'local',
    providerID: serviceID,
    model,
    workspacePolicyID,
    approvalPolicyID: '',
    allowCloud: false,
    allowLocal: true,
    cloudBudgetCents: 0,
    cloudBudgetWindow: '',
  };
}

(async () => {
  const cfg = await request('/plugins/mattermost-ai/admin/config');
  if (!Array.isArray(cfg.services)) {
    cfg.services = [];
  }
  let svc = cfg.services.find((s) => s.id === serviceID);
  if (!svc) {
    svc = {id: serviceID, type: 'openaicompatible'};
    cfg.services.push(svc);
  }
  svc.name = serviceName;
  svc.type = svc.type || 'openaicompatible';
  svc.defaultModel = model;
  svc.apiURL = serviceURL;
  svc.url = serviceURL;
  svc.apiKey = svc.apiKey || 'local';
  svc.streamingTimeoutSeconds = streamingTimeoutSeconds;
  svc.outputTokenLimit = outputTokenLimit;
  svc.useResponsesAPI = false;

  if (codexServiceID) {
    let codexSvc = cfg.services.find((s) => s.id === codexServiceID);
    if (!codexSvc) {
      codexSvc = {id: codexServiceID, type: 'openai-codex'};
      cfg.services.push(codexSvc);
    }
    codexSvc.name = codexServiceName || codexSvc.name || 'Codex Cloud';
    codexSvc.type = 'openai-codex';
    codexSvc.defaultModel = codexModel || codexSvc.defaultModel || 'gpt-5-codex';
    codexSvc.outputTokenLimit = codexSvc.outputTokenLimit || 8192;
    codexSvc.useResponsesAPI = false;
  }
  await request('/plugins/mattermost-ai/admin/config', {method: 'PUT', body: JSON.stringify(cfg)});

  await request(`/plugins/mattermost-ai/admin/runtime/policies/agent/${encodeURIComponent(localAgentBotUserID)}`, {
    method: 'PUT',
    body: JSON.stringify(localPolicy('agent', localAgentBotUserID)),
  });
  await request(`/plugins/mattermost-ai/admin/runtime/policies/channel/${encodeURIComponent(localChannelID)}`, {
    method: 'PUT',
    body: JSON.stringify(localPolicy('channel', localChannelID)),
  });

  const agent = await request(`/plugins/mattermost-ai/agents/${encodeURIComponent(localAgentID)}`);
  agent.serviceID = serviceID;
  agent.model = model;
  agent.displayName = 'Local LLM';
  agent.username = 'local-llm';
  agent.supervisorMode = false;
  agent.customInstructions = 'Visible local LLM agent. Use macserver gemma4:26b-mlx by default and keep sensitive work out of cloud runtimes.';
  await request(`/plugins/mattermost-ai/agents/${encodeURIComponent(localAgentID)}`, {
    method: 'PUT',
    body: JSON.stringify(agent),
  });

  const services = await request('/plugins/mattermost-ai/services');
  const policies = await request('/plugins/mattermost-ai/admin/runtime/policies');
  const localService = services.find((s) => s.id === serviceID);
  const codexService = services.find((s) => s.id === codexServiceID);
  const localPolicies = policies.filter((p) => p.providerID === serviceID && p.runtimeType === 'local');
  console.log(JSON.stringify({
    base,
    service: localService && {id: localService.id, name: localService.name, defaultModel: localService.defaultModel},
    codexService: codexService && {id: codexService.id, name: codexService.name, defaultModel: codexService.defaultModel},
    policyCount: localPolicies.length,
    model,
  }));
})().catch((error) => {
  console.error(error.message || String(error));
  process.exit(1);
});
NODE
	rc=$?
	set -e

	if [ "$rc" -eq 0 ]; then
		log "status=updated url=$base model=$MODEL service_url=$LOCAL_SERVICE_URL"
		if [ "$SELF_DISABLE_CRON" = "1" ]; then
			(crontab -l 2>/dev/null | grep -v 'switch-local-llm-to-macserver.sh') | crontab - || true
			log 'status=cron_removed'
		fi
		exit 0
	fi
	log "status=error url=$base rc=$rc"
done

echo 'Mattermost is currently unreachable on all configured URLs.' >&2
exit 2
