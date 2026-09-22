// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package codexruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const codexAppServerSmokeEnv = "MM_AGENTS_CODEX_APP_SERVER_SMOKE"
const codexAppServerTurnSmokeEnv = "MM_AGENTS_CODEX_APP_SERVER_TURN_SMOKE"

func TestRealCodexAppServerProtocolSmoke(t *testing.T) {
	if os.Getenv(codexAppServerSmokeEnv) != "1" {
		t.Skipf("set %s=1 to run the real codex app-server protocol smoke test", codexAppServerSmokeEnv)
	}

	commandPath := os.Getenv("MM_AGENTS_CODEX_RUNTIME_COMMAND")
	if commandPath == "" {
		commandPath = "codex"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, commandPath, "app-server", "--stdio") // #nosec G204 -- smoke test uses explicit local binary path.
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	require.NoError(t, cmd.Start())
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	require.NoError(t, appServerSend(stdin, map[string]any{
		"id":     1,
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "mattermost_agents_smoke",
				"title":   "Mattermost Agents Smoke Test",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{"experimentalApi": true},
		},
	}))
	require.NoError(t, appServerSend(stdin, map[string]any{"method": "initialized", "params": map[string]any{}}))
	require.NoError(t, appServerSend(stdin, map[string]any{
		"id":     2,
		"method": "thread/start",
		"params": map[string]any{
			"cwd":               filepath.Clean(t.TempDir()),
			"approvalPolicy":    "on-request",
			"approvalsReviewer": "user",
		},
	}))
	time.Sleep(5 * time.Second)
	require.NoError(t, stdin.Close())

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	initializeResponse := readAppServerResponse(t, scanner, "1")
	require.Nil(t, initializeResponse.Error)
	threadResponse := readAppServerResponse(t, scanner, "2")
	require.Nil(t, threadResponse.Error)
	require.NotEmpty(t, appServerThreadID(threadResponse.Result))
}

func readAppServerResponse(t *testing.T, scanner *bufio.Scanner, id string) appServerMessage {
	t.Helper()
	for scanner.Scan() {
		var msg appServerMessage
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &msg))
		if idString(msg.ID) == id {
			return msg
		}
	}
	require.NoError(t, scanner.Err())
	t.Fatalf("codex app-server exited before response %s", id)
	return appServerMessage{}
}

func TestRealCodexAppServerTurnSmoke(t *testing.T) {
	if os.Getenv(codexAppServerTurnSmokeEnv) != "1" {
		t.Skipf("set %s=1 to run the real codex app-server turn smoke test", codexAppServerTurnSmokeEnv)
	}

	commandPath := os.Getenv("MM_AGENTS_CODEX_RUNTIME_COMMAND")
	if commandPath == "" {
		commandPath = "codex"
	}
	prompt := os.Getenv("MM_AGENTS_CODEX_APP_SERVER_TURN_PROMPT")
	if prompt == "" {
		prompt = "Reply with exactly this text and nothing else: mattermost-codex-smoke"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runtime := NewAppServer(Options{CommandPath: commandPath})
	events, err := runtime.StartTurn(ctx, agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:            "smoke-session",
			RuntimeType:   agentruntime.RuntimeTypeCodex,
			ProviderID:    "codex-app-server",
			WorkspacePath: filepath.Clean(t.TempDir()),
		},
		Prompt: prompt,
	})
	require.NoError(t, err)

	var text string
	var externalThreadID string
	var terminal agentruntime.RuntimeEventType
	for event := range events {
		if event.ExternalSessionID != "" {
			externalThreadID = event.ExternalSessionID
		}
		if event.Type == agentruntime.EventTypeTextDelta {
			text += event.Text
		}
		if event.Type == agentruntime.EventTypeError && event.Err != nil {
			t.Fatalf("codex app-server turn failed: %v", event.Err)
		}
		if isTerminal(event.Type) {
			terminal = event.Type
			break
		}
	}

	require.Equal(t, agentruntime.EventTypeCompleted, terminal)
	require.NotEmpty(t, externalThreadID)
	assert.Contains(t, text, "mattermost-codex-smoke")
}
