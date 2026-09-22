// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package codexruntime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartTurnExecutesCodexJSONAndMapsEvents(t *testing.T) {
	exe := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$@" > "$CODEX_ARGS_FILE"
printf '{"type":"reasoning_delta","delta":"thinking"}\n'
printf '{"type":"agent_message_delta","delta":"hello"}\n'
printf '{"type":"tool_call_started","message":"ls"}\n'
printf '{"type":"completed"}\n'
`)
	argsFile := filepath.Join(t.TempDir(), "args")

	r := New(Options{CommandPath: exe, ExtraArgs: []string{"--skip-git-repo-check"}})
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	events, err := r.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:            "session-1",
			RuntimeType:   agentruntime.RuntimeTypeCodex,
			ProviderID:    "codex",
			Model:         "gpt-5-codex",
			WorkspacePath: t.TempDir(),
		},
		Prompt: "do it",
	})
	require.NoError(t, err)

	got := collectEvents(events)
	require.Len(t, got, 4)
	assert.Equal(t, agentruntime.EventTypeReasoningDelta, got[0].Type)
	assert.Equal(t, "thinking", got[0].Text)
	assert.Equal(t, agentruntime.EventTypeTextDelta, got[1].Type)
	assert.Equal(t, "hello", got[1].Text)
	assert.Equal(t, agentruntime.EventTypeToolCallStarted, got[2].Type)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[3].Type)

	rawArgs, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(rawArgs), "exec\n--json\n-m\ngpt-5-codex")
	assert.Contains(t, string(rawArgs), "--skip-git-repo-check")
	assert.Contains(t, string(rawArgs), "do it")

	status, err := r.GetStatus(context.Background(), "session-1")
	require.NoError(t, err)
	assert.Equal(t, agentruntime.SessionStatusCompleted, status.Status)
}

func TestStartTurnEmitsSanitizedError(t *testing.T) {
	exe := fakeCodex(t, `#!/bin/sh
printf 'provider failed with sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOP\n' >&2
exit 7
`)
	r := New(Options{CommandPath: exe})

	events, err := r.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		Prompt:  "fail",
	})
	require.NoError(t, err)

	got := collectEvents(events)
	require.Len(t, got, 1)
	assert.Equal(t, agentruntime.EventTypeError, got[0].Type)
	assert.NotContains(t, got[0].Text, "sk-proj-abcdefghijklmnopqrstuvwxyz")

	status, err := r.GetStatus(context.Background(), "session-1")
	require.NoError(t, err)
	assert.Equal(t, agentruntime.SessionStatusFailed, status.Status)
	assert.NotContains(t, status.LastError, "sk-proj-abcdefghijklmnopqrstuvwxyz")
}

func TestStopTurnCancelsActiveCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake is unix-only")
	}
	exe := fakeCodex(t, `#!/bin/sh
sleep 10
`)
	r := New(Options{CommandPath: exe})

	events, err := r.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		Prompt:  "wait",
	})
	require.NoError(t, err)
	require.NoError(t, r.StopTurn(context.Background(), "session-1"))

	select {
	case event, ok := <-events:
		require.True(t, ok)
		assert.Equal(t, agentruntime.EventTypeCancelled, event.Type)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cancellation event")
	}
}

func TestResumeSessionDoesNotRequireInMemoryStatus(t *testing.T) {
	exe := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$@" > "$CODEX_ARGS_FILE"
printf '{"type":"completed"}\n'
`)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("CODEX_ARGS_FILE", argsFile)

	r := New(Options{CommandPath: exe})
	events, err := r.ResumeSession(context.Background(), "codex-session-1")
	require.NoError(t, err)
	got := collectEvents(events)
	require.Len(t, got, 2)
	assert.Equal(t, agentruntime.EventTypeStatus, got[0].Type)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[1].Type)

	rawArgs, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(rawArgs), "exec\nresume\n--json")
	assert.Contains(t, string(rawArgs), "codex-session-1")
}

func TestParseJSONLineFallsBackToText(t *testing.T) {
	event, emit := parseJSONLine("session-1", []byte("plain text"))
	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeTextDelta, event.Type)
	assert.Equal(t, "plain text", event.Text)
}

func TestParseJSONLineMapsUsage(t *testing.T) {
	event, emit := parseJSONLine("session-1", []byte(`{"type":"usage","usage":{"input_tokens":5,"output_tokens":2}}`))

	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeUsage, event.Type)
	assert.JSONEq(t, `{"type":"usage","usage":{"input_tokens":5,"output_tokens":2}}`, string(event.Payload))
}

func TestParseJSONLinePreservesExternalSessionID(t *testing.T) {
	event, emit := parseJSONLine("runtime-session-1", []byte(`{"type":"status","session_id":"codex-session-1"}`))

	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeStatus, event.Type)
	assert.Equal(t, "runtime-session-1", event.SessionID)
	assert.Equal(t, "codex-session-1", event.ExternalSessionID)
}

func TestParseJSONLineMapsApprovalEvents(t *testing.T) {
	requested, emit := parseJSONLine("runtime-session-1", []byte(`{"type":"approval_requested","session_id":"codex-session-1","approval_id":"provider-approval-1"}`))
	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeApprovalRequested, requested.Type)
	assert.Equal(t, "runtime-session-1", requested.SessionID)
	assert.Equal(t, "codex-session-1", requested.ExternalSessionID)
	assert.JSONEq(t, `{"type":"approval_requested","session_id":"codex-session-1","approval_id":"provider-approval-1"}`, string(requested.Payload))

	resolved, emit := parseJSONLine("runtime-session-1", []byte(`{"type":"approval_resolved","session_id":"codex-session-1","approval_id":"provider-approval-1","decision":"accept"}`))
	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeApprovalResolved, resolved.Type)
	assert.Equal(t, "runtime-session-1", resolved.SessionID)
	assert.Equal(t, "codex-session-1", resolved.ExternalSessionID)
}

func TestPromptWithAttachmentsListsImportedLocalPaths(t *testing.T) {
	prompt := promptWithAttachments("Summarize input.", []agentruntime.RuntimeAttachment{
		{LocalPath: "/workspace/runtime-attachments/input.txt"},
		{Name: "ignored-without-path"},
	})

	assert.Contains(t, prompt, "Summarize input.")
	assert.Contains(t, prompt, "Runtime attachments imported into the workspace:")
	assert.Contains(t, prompt, "/workspace/runtime-attachments/input.txt")
	assert.NotContains(t, prompt, "ignored-without-path")
}

func TestParseJSONLineMapsFileCreatedEvent(t *testing.T) {
	event, emit := parseJSONLine("session-1", []byte(`{"type":"file_created","path":"/workspace/report.txt","fileName":"report.txt"}`))

	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeFileCreated, event.Type)
	assert.JSONEq(t, `{"path":"/workspace/report.txt","fileName":"report.txt"}`, string(event.Payload))
}

func TestSubmitApprovalReportsUnsupportedForCodexCLI(t *testing.T) {
	err := New(Options{}).SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: "approval-1",
		UserID:     "user-1",
		Decision:   agentruntime.ApprovalDecisionAccept,
	})

	require.ErrorIs(t, err, agentruntime.ErrApprovalUnsupported)
}

func TestAppServerRuntimeForwardsApprovalDecision(t *testing.T) {
	exe := fakeCodex(t, `#!/bin/sh
i=0
while IFS= read -r line; do
  i=$((i + 1))
  printf '%s\n' "$line" >> "$CODEX_MESSAGES_FILE"
  if [ "$i" -eq 3 ]; then
    printf '{"id":2,"result":{"thread":{"id":"thread-1"}}}\n'
  fi
  if [ "$i" -eq 4 ]; then
    printf '{"method":"item/commandExecution/requestApproval","id":7,"params":{"threadId":"thread-1","turnId":"turn-1","itemId":"approval-item-1","command":"make test"}}\n'
  fi
  case "$line" in
    *'"id":7'*)
      printf '{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"approved"}}\n'
      printf '{"method":"turn/completed","params":{"threadId":"thread-1","turnId":"turn-1","status":"completed"}}\n'
      exit 0
      ;;
  esac
done
`)
	messagesFile := filepath.Join(t.TempDir(), "messages")
	t.Setenv("CODEX_MESSAGES_FILE", messagesFile)

	r := NewAppServer(Options{CommandPath: exe})
	events, err := r.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:            "session-1",
			RuntimeType:   agentruntime.RuntimeTypeCodex,
			ProviderID:    "codex-app-server",
			Model:         "gpt-5-codex",
			WorkspacePath: t.TempDir(),
		},
		Prompt: "run tests",
	})
	require.NoError(t, err)

	got := []agentruntime.RuntimeEvent{}
	for event := range events {
		got = append(got, event)
		if event.Type == agentruntime.EventTypeApprovalRequested {
			assert.Equal(t, "thread-1", event.ExternalSessionID)
			assert.JSONEq(t, `{"method":"item/commandExecution/requestApproval","id":7,"params":{"threadId":"thread-1","turnId":"turn-1","itemId":"approval-item-1","command":"make test"},"approval_id":"approval-item-1","approvalId":"approval-item-1"}`, string(event.Payload))
			require.NoError(t, r.SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
				ApprovalID: "approval-item-1",
				UserID:     "user-1",
				Decision:   agentruntime.ApprovalDecisionAccept,
			}))
		}
	}

	require.GreaterOrEqual(t, len(got), 4)
	assert.Equal(t, agentruntime.EventTypeStatus, got[0].Type)
	assert.Equal(t, agentruntime.EventTypeApprovalRequested, got[1].Type)
	assert.Equal(t, agentruntime.EventTypeTextDelta, got[2].Type)
	assert.Equal(t, "approved", got[2].Text)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[3].Type)

	rawMessages, err := os.ReadFile(messagesFile)
	require.NoError(t, err)
	assert.Contains(t, string(rawMessages), `"method":"initialize"`)
	assert.Contains(t, string(rawMessages), `"method":"thread/start"`)
	assert.Contains(t, string(rawMessages), `"method":"turn/start"`)
	assert.Contains(t, string(rawMessages), `"id":7`)
	assert.Contains(t, string(rawMessages), `"decision":"accept"`)
}

func TestAppServerRuntimeMapsTurnCompletedTerminalStatus(t *testing.T) {
	failed, emit := NewAppServer(Options{}).handleAppServerMessage("session-1", "", []byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"status":"failed","error":{"message":"tool failed"}}}}`))
	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeError, failed.Type)
	assert.Equal(t, "thread-1", failed.ExternalSessionID)
	require.Error(t, failed.Err)
	assert.Contains(t, failed.Err.Error(), "tool failed")

	cancelled, emit := NewAppServer(Options{}).handleAppServerMessage("session-1", "", []byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"status":"interrupted"}}}`))
	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeCancelled, cancelled.Type)
	assert.Equal(t, "thread-1", cancelled.ExternalSessionID)
}

func TestAppServerRuntimeMapsCompletedFileItem(t *testing.T) {
	event, emit := NewAppServer(Options{}).handleAppServerMessage("session-1", "", []byte(`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"file_change","path":"/workspace/report.txt","fileName":"report.txt"}}}`))

	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeFileCreated, event.Type)
	assert.Equal(t, "thread-1", event.ExternalSessionID)
	assert.JSONEq(t, `{"path":"/workspace/report.txt","fileName":"report.txt"}`, string(event.Payload))
}

func TestAppServerRuntimeDoesNotRepeatCompletedAgentMessage(t *testing.T) {
	tests := []struct {
		name     string
		messages []string
		want     []string
	}{
		{
			name: "completed text repeats streamed delta",
			messages: []string{
				`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"Hello"}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"Hello"}}}`,
			},
			want: []string{"Hello"},
		},
		{
			name: "completed text includes final unstreamed suffix",
			messages: []string{
				`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"Hello"}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"Hello world"}}}`,
			},
			want: []string{"Hello", " world"},
		},
		{
			name: "completed text without deltas",
			messages: []string{
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"Hello"}}}`,
			},
			want: []string{"Hello"},
		},
		{
			name: "each completed message resets streamed text",
			messages: []string{
				`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"First"}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"First"}}}`,
				`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"Second"}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"Second"}}}`,
			},
			want: []string{"First", "Second"},
		},
		{
			name: "empty completion resets streamed text",
			messages: []string{
				`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","delta":"First"}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage"}}}`,
				`{"method":"item/completed","params":{"threadId":"thread-1","item":{"type":"agentMessage","text":"Second"}}}`,
			},
			want: []string{"First", "Second"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewAppServer(Options{})
			var got []string
			for _, message := range tt.messages {
				event, emit := r.handleAppServerMessage("session-1", "", []byte(message))
				if emit && event.Type == agentruntime.EventTypeTextDelta {
					got = append(got, event.Text)
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppServerRuntimeMapsWaitingOnApprovalStatus(t *testing.T) {
	event, emit := NewAppServer(Options{}).handleAppServerMessage("session-1", "", []byte(`{"method":"thread/status/changed","params":{"threadId":"thread-1","status":{"active":true,"flags":["waitingOnApproval"]}}}`))

	require.True(t, emit)
	assert.Equal(t, agentruntime.EventTypeStatus, event.Type)
	assert.Equal(t, "thread-1", event.ExternalSessionID)
	assert.Equal(t, string(agentruntime.SessionStatusWaitingApproval), event.Text)
}

func fakeCodex(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake is unix-only")
	}
	path := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	return path
}

func collectEvents(events <-chan agentruntime.RuntimeEvent) []agentruntime.RuntimeEvent {
	got := []agentruntime.RuntimeEvent{}
	for event := range events {
		got = append(got, event)
	}
	return got
}
