// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package supervisororchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStore struct {
	supervisorRuns []agentruntime.SupervisorRun
	subagentRuns   []agentruntime.SubagentRun
}

func (s *testStore) CreateSupervisorRun(run *agentruntime.SupervisorRun) error {
	if run.ID == "" {
		run.ID = "supervisor-1"
	}
	s.supervisorRuns = append(s.supervisorRuns, *run)
	return nil
}

func (s *testStore) UpdateSupervisorRunStatus(id, status, finalResultPostID string) error {
	for i := range s.supervisorRuns {
		if s.supervisorRuns[i].ID == id {
			s.supervisorRuns[i].Status = status
			s.supervisorRuns[i].FinalResultPostID = finalResultPostID
			return nil
		}
	}
	return errors.New("missing supervisor run")
}

func (s *testStore) CreateSubagentRun(run *agentruntime.SubagentRun) error {
	if run.ID == "" {
		run.ID = "subagent-1"
	}
	s.subagentRuns = append(s.subagentRuns, *run)
	return nil
}

func (s *testStore) UpdateSubagentRunResult(id, status string, result json.RawMessage, summary string) error {
	for i := range s.subagentRuns {
		if s.subagentRuns[i].ID == id {
			s.subagentRuns[i].Status = status
			s.subagentRuns[i].Result = result
			s.subagentRuns[i].Summary = summary
			return nil
		}
	}
	return errors.New("missing subagent run")
}

type testRuntime struct {
	events chan agentruntime.RuntimeEvent
	req    agentruntime.SubagentRequest
	err    error
}

func (r *testRuntime) StartTurn(context.Context, agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	return nil, nil
}

func (r *testRuntime) StartSubagent(_ context.Context, req agentruntime.SubagentRequest) (<-chan agentruntime.RuntimeEvent, error) {
	r.req = req
	if r.err != nil {
		return nil, r.err
	}
	return r.events, nil
}

func (r *testRuntime) StopTurn(context.Context, string) error {
	return nil
}

func (r *testRuntime) ResumeSession(context.Context, string) (<-chan agentruntime.RuntimeEvent, error) {
	return nil, nil
}

func (r *testRuntime) GetStatus(context.Context, string) (agentruntime.RuntimeStatus, error) {
	return agentruntime.RuntimeStatus{}, nil
}

func (r *testRuntime) ListSessions(context.Context, agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	return nil, nil
}

func (r *testRuntime) SubmitApproval(context.Context, agentruntime.RuntimeApprovalDecision) error {
	return nil
}

func TestStartRunPersistsSupervisorRun(t *testing.T) {
	store := &testStore{}
	orchestrator := New(Options{Store: store})

	run, err := orchestrator.StartRun(StartRunRequest{
		RuntimeSessionID:         "runtime-session-1",
		MattermostConversationID: "conversation-1",
		SupervisorAgentID:        "agent-1",
		Objective:                "replace Hermes",
		CreatedBy:                "user-1",
	})
	require.NoError(t, err)
	require.NotNil(t, run)

	require.Len(t, store.supervisorRuns, 1)
	assert.Equal(t, agentruntime.RunStatusRunning, store.supervisorRuns[0].Status)
	assert.Equal(t, "replace Hermes", store.supervisorRuns[0].Objective)
}

func TestStartSubagentPersistsRunAndTerminalResult(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "done"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, Payload: json.RawMessage(`{"ok":true}`)}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{}
	orchestrator := New(Options{
		Store: store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := orchestrator.StartSubagent(context.Background(), SubagentSpec{
		SupervisorRunID: "supervisor-1",
		Role:            "tester",
		Title:           "Run tests",
		Prompt:          "test this",
		Session: agentruntime.RuntimeSession{
			ID:            "session-1",
			RuntimeType:   agentruntime.RuntimeTypeLocal,
			ProviderID:    "ollama",
			Model:         "gpt-oss:20b",
			WorkspacePath: "/workspace",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	gotEvents := []agentruntime.RuntimeEvent{}
	for event := range result.Events {
		gotEvents = append(gotEvents, event)
	}
	require.Len(t, gotEvents, 2)
	require.Len(t, store.subagentRuns, 1)
	assert.Equal(t, store.subagentRuns[0].ID, runtime.req.SubagentRunID)
	assert.Equal(t, store.subagentRuns[0].ID, runtime.req.Session.ID)
	assert.Equal(t, agentruntime.RunStatusCompleted, store.subagentRuns[0].Status)
	assert.Equal(t, "done", store.subagentRuns[0].Summary)
	assert.JSONEq(t, `{"ok":true}`, string(store.subagentRuns[0].Result))
}

func TestStartSubagentMarksWaitingApproval(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "need shell"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeApprovalRequested, Payload: json.RawMessage(`{"approval_id":"provider-approval-1"}`)}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{}
	orchestrator := New(Options{
		Store: store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := orchestrator.StartSubagent(context.Background(), SubagentSpec{
		SupervisorRunID: "supervisor-1",
		Role:            "tester",
		Prompt:          "test this",
		Session: agentruntime.RuntimeSession{
			ID:          "session-1",
			RuntimeType: agentruntime.RuntimeTypeLocal,
		},
	})
	require.NoError(t, err)

	gotEvents := []agentruntime.RuntimeEvent{}
	for event := range result.Events {
		gotEvents = append(gotEvents, event)
	}

	require.Len(t, gotEvents, 2)
	require.Len(t, store.subagentRuns, 1)
	assert.Equal(t, agentruntime.EventTypeApprovalRequested, gotEvents[1].Type)
	assert.Equal(t, store.subagentRuns[0].ID, gotEvents[1].SubagentRunID)
	assert.Equal(t, agentruntime.RunStatusWaitingApproval, store.subagentRuns[0].Status)
	assert.Equal(t, "need shell", store.subagentRuns[0].Summary)
	assert.JSONEq(t, `{"approval_id":"provider-approval-1"}`, string(store.subagentRuns[0].Result))
}

func TestStartSubagentBlocksDisallowedRuntime(t *testing.T) {
	orchestrator := New(Options{
		Store: &testStore{},
		Guard: func(runtimeType agentruntime.RuntimeType) error {
			if runtimeType == agentruntime.RuntimeTypeCodex {
				return agentruntime.ErrCloudNotAllowed
			}
			return nil
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: &testRuntime{},
		},
	})

	_, err := orchestrator.StartSubagent(context.Background(), SubagentSpec{
		SupervisorRunID: "supervisor-1",
		Role:            "coder",
		Prompt:          "change code",
		Session: agentruntime.RuntimeSession{
			RuntimeType: agentruntime.RuntimeTypeCodex,
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, agentruntime.ErrCloudNotAllowed))
}

func TestStartSubagentMarksFailedWhenRuntimeStartFails(t *testing.T) {
	store := &testStore{}
	orchestrator := New(Options{
		Store: store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: &testRuntime{err: errors.New("boom")},
		},
	})

	_, err := orchestrator.StartSubagent(context.Background(), SubagentSpec{
		SupervisorRunID: "supervisor-1",
		Role:            "tester",
		Prompt:          "run tests",
		Session: agentruntime.RuntimeSession{
			RuntimeType: agentruntime.RuntimeTypeLocal,
		},
	})
	require.Error(t, err)

	require.Len(t, store.subagentRuns, 1)
	assert.Equal(t, agentruntime.RunStatusFailed, store.subagentRuns[0].Status)
	assert.Contains(t, string(store.subagentRuns[0].Result), "boom")
}
