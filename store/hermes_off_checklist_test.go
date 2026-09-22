// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package store

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHermesOffChecklistStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	states, err := s.ListHermesOffChecklistStates()
	require.NoError(t, err)
	assert.Empty(t, states)

	state := &agentruntime.HermesOffChecklistItemState{
		Key:       "test_channel_soak",
		Status:    agentruntime.ChecklistStatusOK,
		Detail:    "7 day test passed",
		UpdatedBy: "admin-1",
	}
	require.NoError(t, s.UpsertHermesOffChecklistState(state))
	require.NotZero(t, state.UpdatedAt)

	states, err = s.ListHermesOffChecklistStates()
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "test_channel_soak", states[0].Key)
	assert.Equal(t, agentruntime.ChecklistStatusOK, states[0].Status)
	assert.Equal(t, "7 day test passed", states[0].Detail)
	assert.Equal(t, "admin-1", states[0].UpdatedBy)

	state.Status = agentruntime.ChecklistStatusManual
	state.Detail = "needs rerun"
	state.UpdatedBy = "admin-2"
	require.NoError(t, s.UpsertHermesOffChecklistState(state))

	states, err = s.ListHermesOffChecklistStates()
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, agentruntime.ChecklistStatusManual, states[0].Status)
	assert.Equal(t, "needs rerun", states[0].Detail)
	assert.Equal(t, "admin-2", states[0].UpdatedBy)

	byKey := HermesOffChecklistStatesByKey(states)
	assert.Equal(t, states[0], byKey["test_channel_soak"])
}
