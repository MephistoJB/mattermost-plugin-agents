// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/config"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/stretchr/testify/require"
)

func TestRealLocalRuntimeOpenAICompatibleSmoke(t *testing.T) {
	if os.Getenv("MM_AGENTS_LOCAL_RUNTIME_SMOKE") != "1" {
		t.Skip("set MM_AGENTS_LOCAL_RUNTIME_SMOKE=1 to run against a real local OpenAI-compatible runtime")
	}
	apiURL := os.Getenv("MM_AGENTS_LOCAL_RUNTIME_API_URL")
	model := os.Getenv("MM_AGENTS_LOCAL_RUNTIME_MODEL")
	require.NotEmpty(t, apiURL, "MM_AGENTS_LOCAL_RUNTIME_API_URL is required")
	require.NotEmpty(t, model, "MM_AGENTS_LOCAL_RUNTIME_MODEL is required")

	providerID := os.Getenv("MM_AGENTS_LOCAL_RUNTIME_PROVIDER_ID")
	if providerID == "" {
		providerID = "local-smoke"
	}
	expected := os.Getenv("MM_AGENTS_LOCAL_RUNTIME_EXPECTED")
	if expected == "" {
		expected = "mattermost-local-runtime-smoke"
	}

	runtimes, errs := configuredRuntimeMap(&config.Config{
		Services: []llm.ServiceConfig{
			{
				ID:           providerID,
				Type:         llm.ServiceTypeOpenAICompatible,
				APIKey:       os.Getenv("MM_AGENTS_LOCAL_RUNTIME_API_KEY"),
				APIURL:       apiURL,
				DefaultModel: model,
			},
		},
	})
	require.Empty(t, errs)
	localRuntime := runtimes[agentruntime.RuntimeTypeLocal]
	require.NotNil(t, localRuntime)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	events, err := localRuntime.StartTurn(ctx, agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:          "local-smoke-session",
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  providerID,
			Model:       model,
		},
		Prompt: "Reply exactly with " + expected,
	})
	require.NoError(t, err)

	var text strings.Builder
	completed := false
	for event := range events {
		switch event.Type {
		case agentruntime.EventTypeTextDelta:
			text.WriteString(event.Text)
		case agentruntime.EventTypeCompleted:
			completed = true
		case agentruntime.EventTypeError:
			if event.Err != nil {
				require.NoError(t, event.Err)
			}
			require.FailNow(t, "local runtime returned error event", event.Text)
		}
	}
	require.True(t, completed, "local runtime did not complete")
	require.Contains(t, text.String(), expected)
}
