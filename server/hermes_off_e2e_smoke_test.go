// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hermesOffE2ESmokeEnv = "MM_AGENTS_HERMES_OFF_E2E_SMOKE"

type hermesOffChecklistSmokeResponse struct {
	Ready  bool                           `json:"ready"`
	Groups []hermesOffChecklistSmokeGroup `json:"groups"`
	Health hermesOffChecklistSmokeHealth  `json:"health"`
}

type hermesOffChecklistSmokeGroup struct {
	Key   string                        `json:"key"`
	Items []hermesOffChecklistSmokeItem `json:"items"`
}

type hermesOffChecklistSmokeItem struct {
	Key      string `json:"key"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Manual   bool   `json:"manual"`
	Detail   string `json:"detail"`
}

type hermesOffChecklistSmokeHealth struct {
	Recovery struct {
		Ready bool `json:"ready"`
	} `json:"recovery"`
}

func TestRealMattermostHermesOffChecklistSmoke(t *testing.T) {
	if os.Getenv(hermesOffE2ESmokeEnv) != "1" {
		t.Skipf("set %s=1 to run against a real Mattermost deployment", hermesOffE2ESmokeEnv)
	}

	mattermostURL := strings.TrimRight(os.Getenv("MM_AGENTS_MATTERMOST_URL"), "/")
	token := os.Getenv("MM_AGENTS_MATTERMOST_TOKEN")
	require.NotEmpty(t, mattermostURL, "MM_AGENTS_MATTERMOST_URL is required")
	require.NotEmpty(t, token, "MM_AGENTS_MATTERMOST_TOKEN is required")

	client := &http.Client{Timeout: 15 * time.Second}
	checklistURL := fmt.Sprintf("%s/plugins/%s/admin/runtime/hermes-off-checklist", mattermostURL, manifest.Id)
	req, err := http.NewRequest(http.MethodGet, checklistURL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var checklist hermesOffChecklistSmokeResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&checklist))
	items := hermesOffChecklistItemsByKey(checklist.Groups)

	for _, key := range []string{
		"codex_runtime_e2e",
		"local_runtime_e2e",
		"migration_agent_chat",
		"migration_reminders_tasks",
		"migration_voice_discussion",
		"migration_approval_resume",
		"migration_supervisor_subagents",
		"migration_policy_privacy",
		"migration_usage_admin_ops",
		"hermes_adapter_disabled",
	} {
		item, ok := items[key]
		require.True(t, ok, "missing Hermes-off checklist item %q", key)
		assert.True(t, item.Required, "Hermes-off checklist item %q must stay required", key)
	}

	if os.Getenv("MM_AGENTS_HERMES_OFF_REQUIRE_READY") != "1" {
		return
	}

	var notReady []string
	for key, item := range items {
		if item.Required && item.Status != "ok" {
			notReady = append(notReady, fmt.Sprintf("%s: %s", key, item.Detail))
		}
	}
	assert.True(t, checklist.Health.Recovery.Ready, "restart recovery must be ready before Hermes is disabled")
	assert.True(t, checklist.Ready, "Hermes-off checklist is not ready: %s", strings.Join(notReady, "; "))
}

func hermesOffChecklistItemsByKey(groups []hermesOffChecklistSmokeGroup) map[string]hermesOffChecklistSmokeItem {
	items := map[string]hermesOffChecklistSmokeItem{}
	for _, group := range groups {
		for _, item := range group.Items {
			items[item.Key] = item
		}
	}
	return items
}
