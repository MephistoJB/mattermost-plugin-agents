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

func TestWorkspacePolicyStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	policy := &agentruntime.WorkspacePolicy{
		Name:                 "Project roots",
		AllowedRoots:         []byte(`["/workspace/project"]`),
		DefaultWorkspacePath: "/workspace/project",
		Mode:                 agentruntime.WorkspaceModeAskWrite,
		NetworkMode:          agentruntime.WorkspaceNetworkModeAsk,
		ShellMode:            agentruntime.WorkspaceShellModeDisabled,
	}
	require.NoError(t, s.CreateWorkspacePolicy(policy))
	require.NotEmpty(t, policy.ID)
	require.NotZero(t, policy.CreatedAt)
	assert.JSONEq(t, `["/workspace/project"]`, string(policy.AllowedRoots))

	got, err := s.GetWorkspacePolicy(policy.ID)
	require.NoError(t, err)
	assert.Equal(t, policy.Name, got.Name)
	assert.Equal(t, agentruntime.WorkspaceModeAskWrite, got.Mode)
	assert.JSONEq(t, `["/workspace/project"]`, string(got.AllowedRoots))

	listed, err := s.ListWorkspacePolicies()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, policy.ID, listed[0].ID)

	policy.Mode = agentruntime.WorkspaceModeFullAccess
	policy.NetworkMode = agentruntime.WorkspaceNetworkModeAllowed
	policy.ShellMode = agentruntime.WorkspaceShellModeAllowed
	policy.Metadata = []byte(`{"owner":"admin"}`)
	require.NoError(t, s.UpdateWorkspacePolicy(policy))

	updated, err := s.GetWorkspacePolicy(policy.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.WorkspaceModeFullAccess, updated.Mode)
	assert.Equal(t, agentruntime.WorkspaceNetworkModeAllowed, updated.NetworkMode)
	assert.Equal(t, agentruntime.WorkspaceShellModeAllowed, updated.ShellMode)
	assert.JSONEq(t, `{"owner":"admin"}`, string(updated.Metadata))

	_, err = s.GetWorkspacePolicy("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkspacePolicyNotFound))
}

func TestWorkspacePolicyDefaults(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	policy := &agentruntime.WorkspacePolicy{Name: "Default policy"}
	require.NoError(t, s.CreateWorkspacePolicy(policy))

	assert.Equal(t, agentruntime.WorkspaceModeAskWrite, policy.Mode)
	assert.Equal(t, agentruntime.WorkspaceNetworkModeAsk, policy.NetworkMode)
	assert.Equal(t, agentruntime.WorkspaceShellModeAsk, policy.ShellMode)
	assert.JSONEq(t, `[]`, string(policy.AllowedRoots))
	assert.JSONEq(t, `{}`, string(policy.Metadata))
}
