// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package store

import (
	"errors"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorAndSubagentRunsStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	supervisor := &agentruntime.SupervisorRun{
		RuntimeSessionID:         "runtime-session-1",
		MattermostConversationID: "conversation-1",
		RootTaskID:               "root-post-1",
		SupervisorAgentID:        "agent-1",
		Objective:                "replace Hermes",
		CreatedBy:                "user-1",
	}
	require.NoError(t, s.CreateSupervisorRun(supervisor))
	require.NotEmpty(t, supervisor.ID)
	assert.Equal(t, agentruntime.RunStatusPending, supervisor.Status)
	assert.JSONEq(t, `{}`, string(supervisor.Plan))

	gotSupervisor, err := s.GetSupervisorRun(supervisor.ID)
	require.NoError(t, err)
	assert.Equal(t, supervisor.Objective, gotSupervisor.Objective)

	listedSupervisors, err := s.ListSupervisorRuns(SupervisorRunFilter{
		MattermostConversationID: "conversation-1",
		CreatedBy:                "user-1",
		Limit:                    5,
	})
	require.NoError(t, err)
	require.Len(t, listedSupervisors, 1)
	assert.Equal(t, supervisor.ID, listedSupervisors[0].ID)

	listedSupervisors, err = s.ListSupervisorRuns(SupervisorRunFilter{
		MattermostConversationID: "conversation-1",
		CreatedBy:                "other-user",
	})
	require.NoError(t, err)
	require.Empty(t, listedSupervisors)

	subagent := &agentruntime.SubagentRun{
		SupervisorRunID: supervisor.ID,
		Role:            "researcher",
		Title:           "Hermes capability gap",
		Prompt:          "Find missing Mattermost capabilities.",
		RuntimeType:     agentruntime.RuntimeTypeLocal,
		ProviderID:      "ollama",
		Model:           "gpt-oss:20b",
		WorkspacePath:   "/workspace/project",
		Status:          agentruntime.RunStatusRunning,
	}
	require.NoError(t, s.CreateSubagentRun(subagent))
	require.NotEmpty(t, subagent.ID)
	assert.NotZero(t, subagent.StartedAt)

	subagents, err := s.ListSubagentRuns(supervisor.ID)
	require.NoError(t, err)
	require.Len(t, subagents, 1)
	assert.Equal(t, "researcher", subagents[0].Role)

	require.NoError(t, s.UpdateSubagentRunResult(
		subagent.ID,
		agentruntime.RunStatusCompleted,
		[]byte(`{"ok":true}`),
		"done",
	))
	subagents, err = s.ListSubagentRuns(supervisor.ID)
	require.NoError(t, err)
	require.Len(t, subagents, 1)
	assert.Equal(t, agentruntime.RunStatusCompleted, subagents[0].Status)
	assert.Equal(t, "done", subagents[0].Summary)
	assert.JSONEq(t, `{"ok":true}`, string(subagents[0].Result))
	assert.NotZero(t, subagents[0].FinishedAt)

	require.NoError(t, s.UpdateSupervisorRunStatus(supervisor.ID, agentruntime.RunStatusCompleted, "final-post-1"))
	gotSupervisor, err = s.GetSupervisorRun(supervisor.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.RunStatusCompleted, gotSupervisor.Status)
	assert.Equal(t, "final-post-1", gotSupervisor.FinalResultPostID)

	_, err = s.GetSupervisorRun("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSupervisorRunNotFound))

	err = s.UpdateSubagentRunResult("missing", agentruntime.RunStatusFailed, nil, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSubagentRunNotFound))
}
