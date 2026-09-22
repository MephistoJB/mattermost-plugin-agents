// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost/server/public/model"
)

var ErrWorkspacePolicyNotFound = errors.New("workspace policy not found")

var workspacePolicyColumns = []string{
	"ID", "Name", "AllowedRoots", "DefaultWorkspacePath", "Mode",
	"NetworkMode", "ShellMode", "CreatedAt", "UpdatedAt", "Metadata",
}

func (s *Store) CreateWorkspacePolicy(policy *agentruntime.WorkspacePolicy) error {
	if policy.ID == "" {
		policy.ID = model.NewId()
	}
	now := model.GetMillis()
	if policy.CreatedAt == 0 {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now
	normalizeWorkspacePolicy(policy)

	query, args, err := s.builder.Insert("Agents_WorkspacePolicies").
		Columns(workspacePolicyColumns...).
		Values(
			policy.ID, policy.Name, string(policy.AllowedRoots), policy.DefaultWorkspacePath,
			string(policy.Mode), string(policy.NetworkMode), string(policy.ShellMode),
			policy.CreatedAt, policy.UpdatedAt, string(policy.Metadata),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create workspace policy query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create workspace policy: %w", err)
	}
	return nil
}

func (s *Store) GetWorkspacePolicy(id string) (*agentruntime.WorkspacePolicy, error) {
	query, args, err := s.builder.
		Select(workspacePolicyColumns...).
		From("Agents_WorkspacePolicies").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get workspace policy query: %w", err)
	}
	var policy agentruntime.WorkspacePolicy
	if err := s.db.Get(&policy, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkspacePolicyNotFound
		}
		return nil, fmt.Errorf("failed to get workspace policy: %w", err)
	}
	return &policy, nil
}

func (s *Store) ListWorkspacePolicies() ([]agentruntime.WorkspacePolicy, error) {
	query, args, err := s.builder.
		Select(workspacePolicyColumns...).
		From("Agents_WorkspacePolicies").
		OrderBy("Name ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list workspace policies query: %w", err)
	}
	var policies []agentruntime.WorkspacePolicy
	if err := s.db.Select(&policies, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list workspace policies: %w", err)
	}
	if policies == nil {
		policies = []agentruntime.WorkspacePolicy{}
	}
	return policies, nil
}

func (s *Store) UpdateWorkspacePolicy(policy *agentruntime.WorkspacePolicy) error {
	if policy.ID == "" {
		return errors.New("workspace policy id is required")
	}
	normalizeWorkspacePolicy(policy)
	policy.UpdatedAt = model.GetMillis()

	query, args, err := s.builder.
		Update("Agents_WorkspacePolicies").
		Set("Name", policy.Name).
		Set("AllowedRoots", string(policy.AllowedRoots)).
		Set("DefaultWorkspacePath", policy.DefaultWorkspacePath).
		Set("Mode", string(policy.Mode)).
		Set("NetworkMode", string(policy.NetworkMode)).
		Set("ShellMode", string(policy.ShellMode)).
		Set("UpdatedAt", policy.UpdatedAt).
		Set("Metadata", string(policy.Metadata)).
		Where(sq.Eq{"ID": policy.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update workspace policy query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update workspace policy: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect workspace policy update: %w", err)
	}
	if rows == 0 {
		return ErrWorkspacePolicyNotFound
	}
	return nil
}

func normalizeWorkspacePolicy(policy *agentruntime.WorkspacePolicy) {
	if len(policy.AllowedRoots) == 0 {
		policy.AllowedRoots = json.RawMessage(`[]`)
	}
	if len(policy.Metadata) == 0 {
		policy.Metadata = json.RawMessage(`{}`)
	}
	if policy.Mode == "" {
		policy.Mode = agentruntime.WorkspaceModeAskWrite
	}
	if policy.NetworkMode == "" {
		policy.NetworkMode = agentruntime.WorkspaceNetworkModeAsk
	}
	if policy.ShellMode == "" {
		policy.ShellMode = agentruntime.WorkspaceShellModeAsk
	}
}
