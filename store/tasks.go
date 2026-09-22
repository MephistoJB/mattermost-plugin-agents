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
	ErrTaskNotFound    = errors.New("task not found")
	ErrTaskRunNotFound = errors.New("task run not found")
)

var taskColumns = []string{
	"ID", "Title", "Prompt", "TaskType", "Status", "ChannelID", "RootPostID",
	"UserID", "AgentID", "RuntimePolicySnapshot", "WorkspacePath", "ScheduleSpec",
	"NextRunAt", "LastRunAt", "CreatedBy", "CreatedAt", "UpdatedAt", "Metadata",
}

var taskRunColumns = []string{
	"ID", "TaskID", "RuntimeSessionID", "Status", "StartedAt", "FinishedAt",
	"ResultPostID", "Error", "Usage",
}

type TaskFilter struct {
	UserID    string
	ChannelID string
	Status    agentruntime.TaskStatus
	DueBefore int64
	Limit     int
}

// ClaimDueTasks atomically moves queued due tasks to running and returns the
// claimed rows. SKIP LOCKED lets clustered plugin nodes share the queue.
func (s *Store) ClaimDueTasks(now int64, limit int) ([]agentruntime.Task, error) {
	if limit <= 0 {
		limit = 1
	}

	const query = `
WITH due AS (
	SELECT ID
	FROM Agents_Tasks
	WHERE Status = $1
	  AND NextRunAt > 0
	  AND NextRunAt <= $2
	ORDER BY NextRunAt ASC
	LIMIT $3
	FOR UPDATE SKIP LOCKED
)
UPDATE Agents_Tasks t
SET Status = $4,
    UpdatedAt = $2
FROM due
WHERE t.ID = due.ID
RETURNING t.ID, t.Title, t.Prompt, t.TaskType, t.Status, t.ChannelID, t.RootPostID,
	t.UserID, t.AgentID, t.RuntimePolicySnapshot, t.WorkspacePath, t.ScheduleSpec,
	t.NextRunAt, t.LastRunAt, t.CreatedBy, t.CreatedAt, t.UpdatedAt, t.Metadata`

	var tasks []agentruntime.Task
	if err := s.db.Select(&tasks, query, string(agentruntime.TaskStatusQueued), now, limit, string(agentruntime.TaskStatusRunning)); err != nil {
		return nil, fmt.Errorf("failed to claim due tasks: %w", err)
	}
	if tasks == nil {
		tasks = []agentruntime.Task{}
	}
	return tasks, nil
}

func (s *Store) CreateTask(task *agentruntime.Task) error {
	if task.ID == "" {
		task.ID = model.NewId()
	}
	now := model.GetMillis()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.TaskType == "" {
		task.TaskType = agentruntime.TaskTypeOneShot
	}
	if task.Status == "" {
		task.Status = agentruntime.TaskStatusQueued
	}
	task.RuntimePolicySnapshot = normalizedJSON(task.RuntimePolicySnapshot)
	task.Metadata = normalizedJSON(task.Metadata)

	query, args, err := s.builder.Insert("Agents_Tasks").
		Columns(taskColumns...).
		Values(
			task.ID, task.Title, task.Prompt, string(task.TaskType), string(task.Status),
			task.ChannelID, task.RootPostID, task.UserID, task.AgentID,
			string(task.RuntimePolicySnapshot), task.WorkspacePath, task.ScheduleSpec,
			task.NextRunAt, task.LastRunAt, task.CreatedBy, task.CreatedAt, task.UpdatedAt,
			string(task.Metadata),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create task query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	return nil
}

func (s *Store) GetTask(id string) (*agentruntime.Task, error) {
	query, args, err := s.builder.
		Select(taskColumns...).
		From("Agents_Tasks").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build get task query: %w", err)
	}
	var task agentruntime.Task
	if err := s.db.Get(&task, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return &task, nil
}

func (s *Store) ListTasks(filter TaskFilter) ([]agentruntime.Task, error) {
	builder := s.builder.
		Select(taskColumns...).
		From("Agents_Tasks").
		OrderBy("UpdatedAt DESC")
	if filter.UserID != "" {
		builder = builder.Where(sq.Eq{"UserID": filter.UserID})
	}
	if filter.ChannelID != "" {
		builder = builder.Where(sq.Eq{"ChannelID": filter.ChannelID})
	}
	if filter.Status != "" {
		builder = builder.Where(sq.Eq{"Status": string(filter.Status)})
	}
	if filter.DueBefore > 0 {
		builder = builder.Where(sq.LtOrEq{"NextRunAt": filter.DueBefore})
	}
	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit)) // #nosec G115 -- guarded above
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list tasks query: %w", err)
	}
	var tasks []agentruntime.Task
	if err := s.db.Select(&tasks, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	if tasks == nil {
		tasks = []agentruntime.Task{}
	}
	return tasks, nil
}

func (s *Store) UpdateTaskStatus(id string, status agentruntime.TaskStatus, lastRunAt, nextRunAt int64) error {
	builder := s.builder.
		Update("Agents_Tasks").
		Set("Status", string(status)).
		Set("UpdatedAt", model.GetMillis()).
		Where(sq.Eq{"ID": id})
	if lastRunAt > 0 {
		builder = builder.Set("LastRunAt", lastRunAt)
	}
	if nextRunAt >= 0 {
		builder = builder.Set("NextRunAt", nextRunAt)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update task status query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect task status update: %w", err)
	}
	if rows == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *Store) DeleteTask(id string) error {
	tx, err := s.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin delete task transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	deleteRunsQuery, deleteRunsArgs, err := s.builder.
		Delete("Agents_TaskRuns").
		Where(sq.Eq{"TaskID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete task runs query: %w", err)
	}
	if _, err := tx.Exec(deleteRunsQuery, deleteRunsArgs...); err != nil {
		return fmt.Errorf("failed to delete task runs: %w", err)
	}

	deleteTaskQuery, deleteTaskArgs, err := s.builder.
		Delete("Agents_Tasks").
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete task query: %w", err)
	}
	result, err := tx.Exec(deleteTaskQuery, deleteTaskArgs...)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect task delete: %w", err)
	}
	if rows == 0 {
		return ErrTaskNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit delete task transaction: %w", err)
	}
	return nil
}

func (s *Store) CreateTaskRun(run *agentruntime.TaskRun) error {
	if run.ID == "" {
		run.ID = model.NewId()
	}
	if run.Status == "" {
		run.Status = agentruntime.TaskStatusRunning
	}
	if run.StartedAt == 0 {
		run.StartedAt = model.GetMillis()
	}
	run.Usage = normalizedJSON(run.Usage)

	query, args, err := s.builder.Insert("Agents_TaskRuns").
		Columns(taskRunColumns...).
		Values(
			run.ID, run.TaskID, run.RuntimeSessionID, string(run.Status),
			run.StartedAt, run.FinishedAt, run.ResultPostID, run.Error, string(run.Usage),
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build create task run query: %w", err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to create task run: %w", err)
	}
	return nil
}

func (s *Store) UpdateTaskRunResult(id string, status agentruntime.TaskStatus, resultPostID, errorText string, usage json.RawMessage) error {
	query, args, err := s.builder.
		Update("Agents_TaskRuns").
		Set("Status", string(status)).
		Set("FinishedAt", model.GetMillis()).
		Set("ResultPostID", resultPostID).
		Set("Error", errorText).
		Set("Usage", string(normalizedJSON(usage))).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update task run result query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update task run result: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect task run result update: %w", err)
	}
	if rows == 0 {
		return ErrTaskRunNotFound
	}
	return nil
}

func (s *Store) UpdateTaskRunRuntimeSession(id, runtimeSessionID string) error {
	query, args, err := s.builder.
		Update("Agents_TaskRuns").
		Set("RuntimeSessionID", runtimeSessionID).
		Where(sq.Eq{"ID": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build update task run runtime session query: %w", err)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update task run runtime session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect task run runtime session update: %w", err)
	}
	if rows == 0 {
		return ErrTaskRunNotFound
	}
	return nil
}

func (s *Store) ListTaskRuns(taskID string) ([]agentruntime.TaskRun, error) {
	query, args, err := s.builder.
		Select(taskRunColumns...).
		From("Agents_TaskRuns").
		Where(sq.Eq{"TaskID": taskID}).
		OrderBy("StartedAt DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build list task runs query: %w", err)
	}
	var runs []agentruntime.TaskRun
	if err := s.db.Select(&runs, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list task runs: %w", err)
	}
	if runs == nil {
		runs = []agentruntime.TaskRun{}
	}
	return runs, nil
}
