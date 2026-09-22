// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package store

import (
	"errors"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeSessionStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	session := &agentruntime.RuntimeSession{
		MattermostConversationID: "conversation-1",
		ServerID:                 "server-1",
		TeamID:                   "team-1",
		ChannelID:                "channel-1",
		RootPostID:               "root-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
		RuntimeType:              agentruntime.RuntimeTypeCodex,
		ProviderID:               "codex",
		Model:                    "gpt-5-codex",
		WorkspacePath:            "/workspace/project",
		ExternalSessionID:        "codex-session-1",
		CreatedAt:                time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC).UnixMilli(),
	}

	require.NoError(t, s.CreateRuntimeSession(session))
	require.NotEmpty(t, session.ID)
	assert.Equal(t, agentruntime.SessionStatusIdle, session.Status)
	assert.NotZero(t, session.CreatedAt)
	assert.NotZero(t, session.UpdatedAt)

	got, err := s.GetRuntimeSession(session.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, session.MattermostConversationID, got.MattermostConversationID)
	assert.Equal(t, session.ServerID, got.ServerID)
	assert.Equal(t, session.TeamID, got.TeamID)
	assert.Equal(t, session.AgentID, got.AgentID)
	assert.Equal(t, agentruntime.RuntimeTypeCodex, got.RuntimeType)
	assert.JSONEq(t, `{}`, string(got.Metadata))

	byConversation, err := s.GetRuntimeSessionByConversationAgent("conversation-1", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, session.ID, byConversation.ID)

	listed, err := s.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		ServerID: "server-1",
		TeamID:   "team-1",
		UserID:   "user-1",
		Status:   agentruntime.SessionStatusIdle,
		Limit:    1,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, session.ID, listed[0].ID)

	listed, err = s.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{TeamID: "other-team"})
	require.NoError(t, err)
	assert.Empty(t, listed)

	listed, err = s.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		TeamID:        "team-1",
		CreatedAtFrom: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC).UnixMilli(),
	})
	require.NoError(t, err)
	assert.Empty(t, listed)

	require.NoError(t, s.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, "boom"))
	updated, err := s.GetRuntimeSession(session.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.SessionStatusFailed, updated.Status)
	assert.Equal(t, "boom", updated.LastError)
	assert.GreaterOrEqual(t, updated.UpdatedAt, got.UpdatedAt)

	require.NoError(t, s.UpdateRuntimeSessionMetadata(session.ID, []byte(`{"usage":{"input_tokens":10,"output_tokens":4,"duration_ms":250}}`)))
	withUsage, err := s.GetRuntimeSession(session.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"usage":{"input_tokens":10,"output_tokens":4,"duration_ms":250}}`, string(withUsage.Metadata))

	require.NoError(t, s.UpdateRuntimeSessionExternalSessionID(session.ID, "codex-session-2"))
	withExternalID, err := s.GetRuntimeSession(session.ID)
	require.NoError(t, err)
	assert.Equal(t, "codex-session-2", withExternalID.ExternalSessionID)

	err = s.UpdateRuntimeSessionStatus("missing", agentruntime.SessionStatusCancelled, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimeSessionNotFound))

	err = s.UpdateRuntimeSessionMetadata("missing", []byte(`{}`))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimeSessionNotFound))

	err = s.UpdateRuntimeSessionExternalSessionID("missing", "codex-session-2")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimeSessionNotFound))
}

func TestRuntimePolicyStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	policy := &agentruntime.RuntimePolicy{
		ScopeType:         agentruntime.PolicyScopeChannel,
		ScopeID:           "channel-1",
		RuntimeType:       agentruntime.RuntimeTypeLocal,
		ProviderID:        "ollama",
		Model:             "gpt-oss:20b",
		AllowLocal:        true,
		CloudBudgetCents:  250,
		CloudBudgetWindow: "monthly",
		CreatedBy:         "admin-1",
		UpdatedBy:         "admin-1",
	}
	require.NoError(t, s.UpsertRuntimePolicy(policy))
	require.NotEmpty(t, policy.ID)
	require.NotZero(t, policy.CreatedAt)
	firstID := policy.ID
	firstCreatedAt := policy.CreatedAt

	policies, err := s.ListRuntimePolicies()
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, policies[0].RuntimeType)
	assert.Equal(t, "ollama", policies[0].ProviderID)
	assert.Equal(t, int64(250), policies[0].CloudBudgetCents)
	assert.Equal(t, "monthly", policies[0].CloudBudgetWindow)

	got, err := s.GetRuntimePolicy(agentruntime.PolicyScopeChannel, "channel-1")
	require.NoError(t, err)
	assert.Equal(t, policy.ID, got.ID)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, got.RuntimeType)

	policy.RuntimeType = agentruntime.RuntimeTypeCodex
	policy.ProviderID = "codex"
	policy.Model = "gpt-5-codex"
	policy.AllowCloud = true
	policy.CloudBudgetCents = 500
	policy.CloudBudgetWindow = "daily"
	policy.UpdatedBy = "admin-2"
	require.NoError(t, s.UpsertRuntimePolicy(policy))
	assert.Equal(t, firstID, policy.ID)
	assert.Equal(t, firstCreatedAt, policy.CreatedAt)

	policies, err = s.ListRuntimePolicies()
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, firstID, policies[0].ID)
	assert.Equal(t, agentruntime.RuntimeTypeCodex, policies[0].RuntimeType)
	assert.Equal(t, "admin-2", policies[0].UpdatedBy)
	assert.True(t, policies[0].AllowCloud)
	assert.Equal(t, int64(500), policies[0].CloudBudgetCents)
	assert.Equal(t, "daily", policies[0].CloudBudgetWindow)

	_, err = s.GetRuntimePolicy(agentruntime.PolicyScopeThread, "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimePolicyNotFound))
}
