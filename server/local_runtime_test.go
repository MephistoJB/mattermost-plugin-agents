// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/config"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLocalRuntimeService(t *testing.T) {
	tests := []struct {
		name    string
		service llm.ServiceConfig
		want    bool
	}{
		{
			name: "localhost openai compatible is local",
			service: llm.ServiceConfig{
				ID:     "ollama",
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://localhost:11434/v1",
			},
			want: true,
		},
		{
			name: "private ip openai compatible is local",
			service: llm.ServiceConfig{
				ID:     "vllm",
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://192.168.1.50:8000/v1",
			},
			want: true,
		},
		{
			name: "tailscale ip openai compatible is local",
			service: llm.ServiceConfig{
				ID:     "tailscale-llm",
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://100.100.238.10:8000/v1",
			},
			want: true,
		},
		{
			name: "single label host openai compatible is local",
			service: llm.ServiceConfig{
				ID:     "centralserver",
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "http://centralserver:8000/v1",
			},
			want: true,
		},
		{
			name: "public openai compatible is not local",
			service: llm.ServiceConfig{
				ID:     "remote",
				Type:   llm.ServiceTypeOpenAICompatible,
				APIURL: "https://api.example.com/v1",
			},
			want: false,
		},
		{
			name: "direct openai service is not local",
			service: llm.ServiceConfig{
				ID:     "openai",
				Type:   llm.ServiceTypeOpenAI,
				APIKey: "sk-test",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalRuntimeService(tt.service))
		})
	}
}

func TestConfiguredRuntimeMapRegistersLocalOpenAICompatibleServices(t *testing.T) {
	runtimes, errs := configuredRuntimeMap(&config.Config{
		Services: []llm.ServiceConfig{
			{
				ID:           "ollama",
				Type:         llm.ServiceTypeOpenAICompatible,
				APIURL:       "http://localhost:11434/v1",
				DefaultModel: "llama3",
			},
			{
				ID:           "remote",
				Type:         llm.ServiceTypeOpenAICompatible,
				APIURL:       "https://api.example.com/v1",
				DefaultModel: "remote-model",
			},
		},
	})

	require.Empty(t, errs)
	require.Contains(t, runtimes, agentruntime.RuntimeTypeCodex)
	require.Contains(t, runtimes, agentruntime.RuntimeTypeLocal)
}

func TestConfiguredLocalRuntimeExecutesOpenAICompatibleProvider(t *testing.T) {
	var gotAuthorization string
	var gotRequest struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		gotAuthorization = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotRequest))
		writeChatCompletionSSE(w, "local answer")
	}))
	defer localServer.Close()

	runtimes, errs := configuredRuntimeMap(&config.Config{
		Services: []llm.ServiceConfig{
			{
				ID:           "local-llm",
				Type:         llm.ServiceTypeOpenAICompatible,
				APIKey:       "local-key",
				APIURL:       localServer.URL + "/v1",
				DefaultModel: "llama3",
			},
		},
	})
	require.Empty(t, errs)
	localRuntime := runtimes[agentruntime.RuntimeTypeLocal]
	require.NotNil(t, localRuntime)

	events, err := localRuntime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:          "session-1",
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local-llm",
			Model:       "llama3",
		},
		Prompt: "replace Hermes locally",
	})
	require.NoError(t, err)

	got := collectRuntimeEvents(events)
	require.Len(t, got, 2)
	assert.Equal(t, agentruntime.EventTypeTextDelta, got[0].Type)
	assert.Equal(t, "local answer", got[0].Text)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[1].Type)
	assert.Equal(t, "Bearer local-key", gotAuthorization)
	assert.Equal(t, "llama3", gotRequest.Model)
	require.Len(t, gotRequest.Messages, 1)
	assert.Equal(t, "user", gotRequest.Messages[0].Role)
	assert.Equal(t, "replace Hermes locally", gotRequest.Messages[0].Content)
	assert.True(t, gotRequest.Stream)
}

func TestConfiguredLocalRuntimeDoesNotUseRemoteOpenAICompatibleProvider(t *testing.T) {
	var localCalls int
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		localCalls++
		writeChatCompletionSSE(w, "local answer")
	}))
	defer localServer.Close()

	runtimes, errs := configuredRuntimeMap(&config.Config{
		Services: []llm.ServiceConfig{
			{
				ID:           "local-llm",
				Type:         llm.ServiceTypeOpenAICompatible,
				APIKey:       "local-key",
				APIURL:       localServer.URL + "/v1",
				DefaultModel: "llama3",
			},
			{
				ID:           "cloud-compatible",
				Type:         llm.ServiceTypeOpenAICompatible,
				APIURL:       "https://api.example.com/v1",
				DefaultModel: "cloud-model",
			},
		},
	})
	require.Empty(t, errs)
	localRuntime := runtimes[agentruntime.RuntimeTypeLocal]
	require.NotNil(t, localRuntime)

	events, err := localRuntime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:          "session-1",
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local-llm",
			Model:       "llama3",
		},
		Prompt: "sensitive local-only work",
	})
	require.NoError(t, err)
	got := collectRuntimeEvents(events)
	require.Len(t, got, 2)
	assert.Equal(t, "local answer", got[0].Text)
	assert.Equal(t, 1, localCalls)

	_, err = localRuntime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:          "session-2",
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "cloud-compatible",
			Model:       "cloud-model",
		},
		Prompt: "must not leave local runtime",
	})
	require.ErrorIs(t, err, agentruntime.ErrRuntimeUnavailable)
	assert.Equal(t, 1, localCalls)
}

func writeChatCompletionSSE(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\n", content)
	fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func collectRuntimeEvents(events <-chan agentruntime.RuntimeEvent) []agentruntime.RuntimeEvent {
	got := []agentruntime.RuntimeEvent{}
	for event := range events {
		got = append(got, event)
	}
	return got
}
