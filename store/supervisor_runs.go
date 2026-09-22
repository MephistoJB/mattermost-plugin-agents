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
	ErrSupervisorRunNotFound = errors.New("supervisor run not found")
	ErrSubagentRunNotFound   = errors.New("subagent run not found")
)

var supervisorRunColumns = []string{
	"ID", "RuntimeSessionID", "MattermostConversationID", "RootTaskID",
	"SupervisorAgentID", "Status", "Objective", "Plan", "FinalResultPostID",
	"CreatedBy", "CreatedAt", "UpdatedAt", "Metadata",
}

var subagentRunColumns = []string{
	"ID", "SupervisorRunID", "ParentSubagentRunID", "Role", "Title", "Prompt",
	"RuntimeType", "ProviderID", "Model", "WorkspacePath", "ExternalSessionID",
	"Status", "Result", "Summary", "StartedAt", "FinishedAt", "CreatedAt", "UpdatedAt", "Metadata",
}

type SupervisorRunFilter struct {
	RuntimeSessionID         string
	MattermostConversationID string
	RootTaskID               string
	SupervisorAgentID        string
	CreatedBy                string
	Status                   string
	Limit                    int
}

func (s *Store) CreateSupervisorRun(run *agentruntime.SupervisorRun) error {
	if run.ID == "" {
		run.ID = model.NewId()
	}
	now := model.GetMillis()
	if run.CreatedAt == 0 {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	if run.Status == "" {
		run.Status = agentruntime.RunStatusPending
	}
	run.Plan = normalizedJSON(run.Plan)
	run.Metadata = normalizedJSON(run.Metadata)

	query, args, err := s.builder.Insert("Agents_SupervisorRuns").
		Columns(supervisorRunColumns...).
		Values(
			run.ID, run.RuntimeSessionID, run.MattermostConversationID, run.RootTaskID,
			run.SupervisorAgentID, run.Status, run.Objective, string(run.Plan),
			run.FinalResultPostID, run.CreatedBy, run.CreatedAt, run.UpdatedAt, string(run.Metadata),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create supervisor run query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create supervisor run: %w", err)
	}
	return nil
}

func (s *Store) GetSupervisorRun(id string) (*agentruntime.SupervisorRun, error) {
	query, args, err := s.builder.
		Select(supervisorRunColumns...).
		From("Agents_SupervisorRuns").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get supervisor run query: %w", err)
	}
	var run agentruntime.SupervisorRun
	if err := s.db.Get(&run, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSupervisorRunNotFound
		}
		return nil, fmt.Errorf("failed to get supervisor run: %w", err)
	}
	return &run, nil
}

func (s *Store) ListSupervisorRuns(filter SupervisorRunFilter) ([]agentruntime.SupervisorRun, error) {
	builder := s.builder.
		Select(supervisorRunColumns...).
		From("Agents_SupervisorRuns").
		OrderBy("UpdatedAt DESC")

	if filter.RuntimeSessionID != "" {
		builder = builder.Where(sq.Eq{"RuntimeSessionID": filter.RuntimeSessionID})
	}
	if filter.MattermostConversationID != "" {
		builder = builder.Where(sq.Eq{"MattermostConversationID": filter.MattermostConversationID})
	}
	if filter.RootTaskID != "" {
		builder = builder.Where(sq.Eq{"RootTaskID": filter.RootTaskID})
	}
	if filter.SupervisorAgentID != "" {
		builder = builder.Where(sq.Eq{"SupervisorAgentID": filter.SupervisorAgentID})
	}
	if filter.CreatedBy != "" {
		builder = builder.Where(sq.Eq{"CreatedBy": filter.CreatedBy})
	}
	if filter.Status != "" {
		builder = builder.Where(sq.Eq{"Status": filter.Status})
	}
	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit)) // #nosec G115 -- guarded above
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list supervisor runs query: %w", err)
	}
	var runs []agentruntime.SupervisorRun
	if err := s.db.Select(&runs, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list supervisor runs: %w", err)
	}
	if runs == nil {
		runs = []agentruntime.SupervisorRun{}
	}
	return runs, nil
}

func (s *Store) UpdateSupervisorRunStatus(id, status, finalResultPostID string) error {
	now := model.GetMillis()
	builder := s.builder.
		Update("Agents_SupervisorRuns").
		Set("Status", status).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id})
	if finalResultPostID != "" {
		builder = builder.Set("FinalResultPostID", finalResultPostID)
	}
	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update supervisor run status query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update supervisor run status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect supervisor run status update: %w", err)
	}
	if rows == 0 {
		return ErrSupervisorRunNotFound
	}
	return nil
}

func (s *Store) CreateSubagentRun(run *agentruntime.SubagentRun) error {
	if run.ID == "" {
		run.ID = model.NewId()
	}
	now := model.GetMillis()
	if run.CreatedAt == 0 {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	if run.StartedAt == 0 {
		run.StartedAt = now
	}
	if run.Status == "" {
		run.Status = agentruntime.RunStatusPending
	}
	run.Result = normalizedJSON(run.Result)
	run.Metadata = normalizedJSON(run.Metadata)

	query, args, err := s.builder.Insert("Agents_SubagentRuns").
		Columns(subagentRunColumns...).
		Values(
			run.ID, run.SupervisorRunID, run.ParentSubagentRunID, run.Role, run.Title, run.Prompt,
			string(run.RuntimeType), run.ProviderID, run.Model, run.WorkspacePath, run.ExternalSessionID,
			run.Status, string(run.Result), run.Summary, run.StartedAt, run.FinishedAt,
			run.CreatedAt, run.UpdatedAt, string(run.Metadata),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create subagent run query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create subagent run: %w", err)
	}
	return nil
}

func (s *Store) ListSubagentRuns(supervisorRunID string) ([]agentruntime.SubagentRun, error) {
	query, args, err := s.builder.
		Select(subagentRunColumns...).
		From("Agents_SubagentRuns").
		Where(sq.Eq{"SupervisorRunID": supervisorRunID}).
		OrderBy("CreatedAt ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list subagent runs query: %w", err)
	}
	var runs []agentruntime.SubagentRun
	if err := s.db.Select(&runs, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list subagent runs: %w", err)
	}
	if runs == nil {
		runs = []agentruntime.SubagentRun{}
	}
	return runs, nil
}

func (s *Store) UpdateSubagentRunResult(id, status string, result json.RawMessage, summary string) error {
	now := model.GetMillis()
	query, args, err := s.builder.
		Update("Agents_SubagentRuns").
		Set("Status", status).
		Set("Result", string(normalizedJSON(result))).
		Set("Summary", summary).
		Set("FinishedAt", now).
		Set("UpdatedAt", now).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update subagent run result query: %w", err)
	}
	resultExec, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update subagent run result: %w", err)
	}
	rows, err := resultExec.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect subagent run result update: %w", err)
	}
	if rows == 0 {
		return ErrSubagentRunNotFound
	}
	return nil
}
