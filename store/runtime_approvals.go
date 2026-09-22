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

var ErrRuntimeApprovalNotFound = errors.New("runtime approval not found")

var runtimeApprovalColumns = []string{
	"ID", "RuntimeSessionID", "ExternalApprovalID", "SubagentRunID", "Status",
	"RequestPayload", "Decision", "DecisionScope", "DecisionReason", "RequestedBy",
	"DecidedBy", "ExpiresAt", "CreatedAt", "UpdatedAt", "Metadata",
}

type RuntimeApprovalFilter struct {
	RuntimeSessionID string
	Status           agentruntime.RuntimeApprovalStatus
	RequestedBy      string
	Limit            int
}

func (s *Store) CreateRuntimeApproval(approval *agentruntime.RuntimeApproval) error {
	if approval.ID == "" {
		approval.ID = model.NewId()
	}
	now := model.GetMillis()
	if approval.CreatedAt == 0 {
		approval.CreatedAt = now
	}
	approval.UpdatedAt = now
	normalizeRuntimeApproval(approval)

	query, args, err := s.builder.Insert("Agents_RuntimeApprovals").
		Columns(runtimeApprovalColumns...).
		Values(
			approval.ID, approval.RuntimeSessionID, approval.ExternalApprovalID, approval.SubagentRunID,
			string(approval.Status), string(approval.RequestPayload), approval.Decision,
			approval.DecisionScope, approval.DecisionReason, approval.RequestedBy, approval.DecidedBy,
			approval.ExpiresAt, approval.CreatedAt, approval.UpdatedAt, string(approval.Metadata),
		).
		Suffix("ON CONFLICT DO NOTHING").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create runtime approval query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create runtime approval: %w", err)
	}
	return nil
}

func (s *Store) GetRuntimeApproval(id string) (*agentruntime.RuntimeApproval, error) {
	query, args, err := s.builder.
		Select(runtimeApprovalColumns...).
		From("Agents_RuntimeApprovals").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get runtime approval query: %w", err)
	}
	var approval agentruntime.RuntimeApproval
	if err := s.db.Get(&approval, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRuntimeApprovalNotFound
		}
		return nil, fmt.Errorf("failed to get runtime approval: %w", err)
	}
	return &approval, nil
}

func (s *Store) ListRuntimeApprovals(filter RuntimeApprovalFilter) ([]agentruntime.RuntimeApproval, error) {
	builder := s.builder.
		Select(runtimeApprovalColumns...).
		From("Agents_RuntimeApprovals").
		OrderBy("CreatedAt DESC")

	if filter.RuntimeSessionID != "" {
		builder = builder.Where(sq.Eq{"RuntimeSessionID": filter.RuntimeSessionID})
	}
	if filter.Status != "" {
		builder = builder.Where(sq.Eq{"Status": string(filter.Status)})
	}
	if filter.RequestedBy != "" {
		builder = builder.Where(sq.Eq{"RequestedBy": filter.RequestedBy})
	}
	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit)) // #nosec G115 -- guarded above
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list runtime approvals query: %w", err)
	}
	var approvals []agentruntime.RuntimeApproval
	if err := s.db.Select(&approvals, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list runtime approvals: %w", err)
	}
	if approvals == nil {
		approvals = []agentruntime.RuntimeApproval{}
	}
	return approvals, nil
}

func (s *Store) UpdateRuntimeApprovalDecision(id string, decision agentruntime.RuntimeApprovalDecision, status agentruntime.RuntimeApprovalStatus) error {
	now := model.GetMillis()
	query, args, err := s.builder.
		Update("Agents_RuntimeApprovals").
		Set("Status", string(status)).
		Set("Decision", decision.Decision).
		Set("DecisionScope", decision.Scope).
		Set("DecisionReason", decision.Reason).
		Set("DecidedBy", decision.UserID).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update runtime approval decision query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update runtime approval decision: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect runtime approval decision update: %w", err)
	}
	if rows == 0 {
		return ErrRuntimeApprovalNotFound
	}
	return nil
}

func (s *Store) ExpireRuntimeApprovals(now int64) (int64, error) {
	query, args, err := s.builder.
		Update("Agents_RuntimeApprovals").
		Set("Status", string(agentruntime.ApprovalStatusExpired)).
		Set("UpdatedAt", now).
		Where(sq.Eq{"Status": string(agentruntime.ApprovalStatusPending)}).
		Where(sq.Gt{"ExpiresAt": int64(0)}).
		Where(sq.LtOrEq{"ExpiresAt": now}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build expire runtime approvals query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to expire runtime approvals: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to inspect runtime approval expiry update: %w", err)
	}
	return rows, nil
}

func normalizeRuntimeApproval(approval *agentruntime.RuntimeApproval) {
	if approval.Status == "" {
		approval.Status = agentruntime.ApprovalStatusPending
	}
	if len(approval.RequestPayload) == 0 {
		approval.RequestPayload = json.RawMessage(`{}`)
	}
	if len(approval.Metadata) == 0 {
		approval.Metadata = json.RawMessage(`{}`)
	}
}
