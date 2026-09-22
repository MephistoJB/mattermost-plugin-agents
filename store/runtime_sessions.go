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

var (
	ErrRuntimeSessionNotFound = errors.New("runtime session not found")
	ErrRuntimePolicyNotFound  = errors.New("runtime policy not found")
)

var runtimeSessionColumns = []string{
	"ID", "MattermostConversationID", "ServerID", "TeamID", "ChannelID", "RootPostID", "UserID", "AgentID",
	"RuntimeType", "ProviderID", "Model", "WorkspacePath", "ExternalSessionID",
	"Status", "LastError", "LastEventAt", "CreatedAt", "UpdatedAt", "Metadata",
}

var runtimePolicyColumns = []string{
	"ID", "ScopeType", "ScopeID", "RuntimeType", "ProviderID", "Model",
	"WorkspacePolicyID", "ApprovalPolicyID", "AllowCloud", "AllowLocal",
	"CloudBudgetCents", "CloudBudgetWindow", "CreatedBy", "UpdatedBy", "CreatedAt", "UpdatedAt",
}

// CreateRuntimeSession inserts a durable runtime session row.
// Missing ID/timestamps/status/metadata are initialized for callers.
func (s *Store) CreateRuntimeSession(session *agentruntime.RuntimeSession) error {
	if session.ID == "" {
		session.ID = model.NewId()
	}
	now := model.GetMillis()
	if session.CreatedAt == 0 {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	if session.LastEventAt == 0 {
		session.LastEventAt = now
	}
	if session.Status == "" {
		session.Status = agentruntime.SessionStatusIdle
	}
	metadata := normalizedJSON(session.Metadata)
	session.Metadata = metadata

	query, args, err := s.builder.Insert("Agents_RuntimeSessions").
		Columns(runtimeSessionColumns...).
		Values(
			session.ID, session.MattermostConversationID, session.ServerID, session.TeamID,
			session.ChannelID, session.RootPostID, session.UserID, session.AgentID,
			string(session.RuntimeType), session.ProviderID, session.Model, session.WorkspacePath,
			session.ExternalSessionID, string(session.Status),
			session.LastError, session.LastEventAt, session.CreatedAt, session.UpdatedAt, string(metadata),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create runtime session query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create runtime session: %w", err)
	}
	return nil
}

func (s *Store) GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error) {
	query, args, err := s.builder.
		Select(runtimeSessionColumns...).
		From("Agents_RuntimeSessions").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get runtime session query: %w", err)
	}
	var session agentruntime.RuntimeSession
	if err := s.db.Get(&session, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRuntimeSessionNotFound
		}
		return nil, fmt.Errorf("failed to get runtime session: %w", err)
	}
	return &session, nil
}

func (s *Store) GetRuntimeSessionByConversationAgent(conversationID, agentID string) (*agentruntime.RuntimeSession, error) {
	query, args, err := s.builder.
		Select(runtimeSessionColumns...).
		From("Agents_RuntimeSessions").
		Where(sq.Eq{"MattermostConversationID": conversationID}).
		Where(sq.Eq{"AgentID": agentID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get runtime session by conversation query: %w", err)
	}
	var session agentruntime.RuntimeSession
	if err := s.db.Get(&session, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRuntimeSessionNotFound
		}
		return nil, fmt.Errorf("failed to get runtime session by conversation: %w", err)
	}
	return &session, nil
}

func (s *Store) ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	builder := s.builder.
		Select(runtimeSessionColumns...).
		From("Agents_RuntimeSessions").
		OrderBy("UpdatedAt DESC")

	if filter.ServerID != "" {
		builder = builder.Where(sq.Eq{"ServerID": filter.ServerID})
	}
	if filter.TeamID != "" {
		builder = builder.Where(sq.Eq{"TeamID": filter.TeamID})
	}
	if filter.UserID != "" {
		builder = builder.Where(sq.Eq{"UserID": filter.UserID})
	}
	if filter.AgentID != "" {
		builder = builder.Where(sq.Eq{"AgentID": filter.AgentID})
	}
	if filter.ChannelID != "" {
		builder = builder.Where(sq.Eq{"ChannelID": filter.ChannelID})
	}
	if filter.RootPostID != "" {
		builder = builder.Where(sq.Eq{"RootPostID": filter.RootPostID})
	}
	if filter.ConversationID != "" {
		builder = builder.Where(sq.Eq{"MattermostConversationID": filter.ConversationID})
	}
	if filter.Status != "" {
		builder = builder.Where(sq.Eq{"Status": string(filter.Status)})
	}
	if filter.CreatedAtFrom > 0 {
		builder = builder.Where(sq.GtOrEq{"CreatedAt": filter.CreatedAtFrom})
	}
	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit)) // #nosec G115 -- guarded above
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list runtime sessions query: %w", err)
	}
	var sessions []agentruntime.RuntimeSession
	if err := s.db.Select(&sessions, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list runtime sessions: %w", err)
	}
	if sessions == nil {
		sessions = []agentruntime.RuntimeSession{}
	}
	return sessions, nil
}

func (s *Store) UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error {
	now := model.GetMillis()
	query, args, err := s.builder.
		Update("Agents_RuntimeSessions").
		Set("Status", string(status)).
		Set("LastError", lastError).
		Set("LastEventAt", now).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update runtime session status query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update runtime session status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect runtime session status update: %w", err)
	}
	if rows == 0 {
		return ErrRuntimeSessionNotFound
	}
	return nil
}

func (s *Store) UpdateRuntimeSessionMetadata(id string, metadata json.RawMessage) error {
	now := model.GetMillis()
	query, args, err := s.builder.
		Update("Agents_RuntimeSessions").
		Set("Metadata", string(normalizedJSON(metadata))).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update runtime session metadata query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update runtime session metadata: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect runtime session metadata update: %w", err)
	}
	if rows == 0 {
		return ErrRuntimeSessionNotFound
	}
	return nil
}

func (s *Store) UpdateRuntimeSessionExternalSessionID(id string, externalSessionID string) error {
	now := model.GetMillis()
	query, args, err := s.builder.
		Update("Agents_RuntimeSessions").
		Set("ExternalSessionID", externalSessionID).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update runtime session external id query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update runtime session external id: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect runtime session external id update: %w", err)
	}
	if rows == 0 {
		return ErrRuntimeSessionNotFound
	}
	return nil
}

func (s *Store) GetRuntimePolicy(scopeType agentruntime.PolicyScopeType, scopeID string) (*agentruntime.RuntimePolicy, error) {
	query, args, err := s.builder.
		Select(runtimePolicyColumns...).
		From("Agents_RuntimePolicies").
		Where(sq.Eq{"ScopeType": string(scopeType)}).
		Where(sq.Eq{"ScopeID": scopeID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get runtime policy query: %w", err)
	}
	var policy agentruntime.RuntimePolicy
	if err := s.db.Get(&policy, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRuntimePolicyNotFound
		}
		return nil, fmt.Errorf("failed to get runtime policy: %w", err)
	}
	return &policy, nil
}

func (s *Store) UpsertRuntimePolicy(policy *agentruntime.RuntimePolicy) error {
	if policy.ID == "" {
		policy.ID = model.NewId()
	}
	now := model.GetMillis()
	if policy.CreatedAt == 0 {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now

	const query = `
INSERT INTO Agents_RuntimePolicies (
	ID, ScopeType, ScopeID, RuntimeType, ProviderID, Model,
	WorkspacePolicyID, ApprovalPolicyID, AllowCloud, AllowLocal,
	CloudBudgetCents, CloudBudgetWindow, CreatedBy, UpdatedBy, CreatedAt, UpdatedAt
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (ScopeType, ScopeID) DO UPDATE SET
	RuntimeType = EXCLUDED.RuntimeType,
	ProviderID = EXCLUDED.ProviderID,
	Model = EXCLUDED.Model,
	WorkspacePolicyID = EXCLUDED.WorkspacePolicyID,
	ApprovalPolicyID = EXCLUDED.ApprovalPolicyID,
	AllowCloud = EXCLUDED.AllowCloud,
	AllowLocal = EXCLUDED.AllowLocal,
	CloudBudgetCents = EXCLUDED.CloudBudgetCents,
	CloudBudgetWindow = EXCLUDED.CloudBudgetWindow,
	UpdatedBy = EXCLUDED.UpdatedBy,
	UpdatedAt = EXCLUDED.UpdatedAt
RETURNING ID, CreatedAt`

	err := s.db.QueryRow(query,
		policy.ID, string(policy.ScopeType), policy.ScopeID, string(policy.RuntimeType),
		policy.ProviderID, policy.Model, policy.WorkspacePolicyID, policy.ApprovalPolicyID,
		policy.AllowCloud, policy.AllowLocal, policy.CloudBudgetCents, policy.CloudBudgetWindow,
		policy.CreatedBy, policy.UpdatedBy, policy.CreatedAt, policy.UpdatedAt,
	).Scan(&policy.ID, &policy.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert runtime policy: %w", err)
	}
	return nil
}

func (s *Store) ListRuntimePolicies() ([]agentruntime.RuntimePolicy, error) {
	query, args, err := s.builder.
		Select(runtimePolicyColumns...).
		From("Agents_RuntimePolicies").
		OrderBy("ScopeType ASC", "ScopeID ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list runtime policies query: %w", err)
	}
	var policies []agentruntime.RuntimePolicy
	if err := s.db.Select(&policies, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list runtime policies: %w", err)
	}
	if policies == nil {
		policies = []agentruntime.RuntimePolicy{}
	}
	return policies, nil
}

func normalizedJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
