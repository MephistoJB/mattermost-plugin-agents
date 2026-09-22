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

func TestRuntimeApprovalStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	approval := &agentruntime.RuntimeApproval{
		RuntimeSessionID:   "session-1",
		ExternalApprovalID: "provider-approval-1",
		SubagentRunID:      "subagent-1",
		RequestPayload:     []byte(`{"tool":"shell"}`),
		RequestedBy:        "user-1",
		ExpiresAt:          12345,
	}
	require.NoError(t, s.CreateRuntimeApproval(approval))
	require.NotEmpty(t, approval.ID)
	assert.Equal(t, agentruntime.ApprovalStatusPending, approval.Status)

	got, err := s.GetRuntimeApproval(approval.ID)
	require.NoError(t, err)
	assert.Equal(t, "session-1", got.RuntimeSessionID)
	assert.Equal(t, "provider-approval-1", got.ExternalApprovalID)
	assert.JSONEq(t, `{"tool":"shell"}`, string(got.RequestPayload))

	listed, err := s.ListRuntimeApprovals(RuntimeApprovalFilter{
		RuntimeSessionID: "session-1",
		Status:           agentruntime.ApprovalStatusPending,
		RequestedBy:      "user-1",
		Limit:            10,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, approval.ID, listed[0].ID)

	decision := agentruntime.RuntimeApprovalDecision{
		ApprovalID: approval.ID,
		UserID:     "user-1",
		Decision:   agentruntime.ApprovalDecisionAccept,
		Scope:      "session",
		Reason:     "approved",
	}
	require.NoError(t, s.UpdateRuntimeApprovalDecision(approval.ID, decision, agentruntime.ApprovalStatusAccepted))
	got, err = s.GetRuntimeApproval(approval.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.ApprovalStatusAccepted, got.Status)
	assert.Equal(t, "session", got.DecisionScope)
	assert.Equal(t, "approved", got.DecisionReason)

	_, err = s.GetRuntimeApproval("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuntimeApprovalNotFound))
}

func TestExpireRuntimeApprovalsOnlyExpiresDuePendingApprovals(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	now := int64(2000)
	due := &agentruntime.RuntimeApproval{
		RuntimeSessionID: "session-1",
		RequestedBy:      "user-1",
		ExpiresAt:        now,
	}
	future := &agentruntime.RuntimeApproval{
		RuntimeSessionID: "session-1",
		RequestedBy:      "user-1",
		ExpiresAt:        now + 1,
	}
	decided := &agentruntime.RuntimeApproval{
		RuntimeSessionID: "session-1",
		Status:           agentruntime.ApprovalStatusAccepted,
		RequestedBy:      "user-1",
		ExpiresAt:        now,
	}
	withoutExpiry := &agentruntime.RuntimeApproval{
		RuntimeSessionID: "session-1",
		RequestedBy:      "user-1",
	}
	require.NoError(t, s.CreateRuntimeApproval(due))
	require.NoError(t, s.CreateRuntimeApproval(future))
	require.NoError(t, s.CreateRuntimeApproval(decided))
	require.NoError(t, s.CreateRuntimeApproval(withoutExpiry))

	expired, err := s.ExpireRuntimeApprovals(now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), expired)

	gotDue, err := s.GetRuntimeApproval(due.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.ApprovalStatusExpired, gotDue.Status)
	assert.Equal(t, now, gotDue.UpdatedAt)

	gotFuture, err := s.GetRuntimeApproval(future.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.ApprovalStatusPending, gotFuture.Status)
	gotDecided, err := s.GetRuntimeApproval(decided.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.ApprovalStatusAccepted, gotDecided.Status)
	gotWithoutExpiry, err := s.GetRuntimeApproval(withoutExpiry.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.ApprovalStatusPending, gotWithoutExpiry.Status)
}
