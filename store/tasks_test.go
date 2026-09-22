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

func TestTaskStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	task := &agentruntime.Task{
		Title:         "Daily check",
		Prompt:        "Check the build.",
		TaskType:      agentruntime.TaskTypeRecurring,
		ChannelID:     "channel-1",
		RootPostID:    "root-1",
		UserID:        "user-1",
		AgentID:       "agent-1",
		WorkspacePath: "/workspace/project",
		ScheduleSpec:  "every day 09:00",
		NextRunAt:     1000,
		CreatedBy:     "user-1",
	}
	require.NoError(t, s.CreateTask(task))
	require.NotEmpty(t, task.ID)
	assert.Equal(t, agentruntime.TaskStatusQueued, task.Status)
	assert.JSONEq(t, `{}`, string(task.RuntimePolicySnapshot))

	got, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.Title, got.Title)
	assert.Equal(t, agentruntime.TaskTypeRecurring, got.TaskType)

	listed, err := s.ListTasks(TaskFilter{
		UserID:    "user-1",
		ChannelID: "channel-1",
		Status:    agentruntime.TaskStatusQueued,
		DueBefore: 1000,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, task.ID, listed[0].ID)

	require.NoError(t, s.UpdateTaskStatus(task.ID, agentruntime.TaskStatusPaused, 2000, 3000))
	updated, err := s.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.TaskStatusPaused, updated.Status)
	assert.Equal(t, int64(2000), updated.LastRunAt)
	assert.Equal(t, int64(3000), updated.NextRunAt)

	err = s.UpdateTaskStatus("missing", agentruntime.TaskStatusCancelled, 0, -1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestDeleteTaskDeletesRuns(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	task := &agentruntime.Task{
		Prompt:    "Remove me later.",
		TaskType:  agentruntime.TaskTypeOneShot,
		UserID:    "user-1",
		AgentID:   "agent-1",
		CreatedBy: "user-1",
	}
	require.NoError(t, s.CreateTask(task))

	run := &agentruntime.TaskRun{TaskID: task.ID}
	require.NoError(t, s.CreateTaskRun(run))

	require.NoError(t, s.DeleteTask(task.ID))

	_, err := s.GetTask(task.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))

	runs, err := s.ListTaskRuns(task.ID)
	require.NoError(t, err)
	assert.Empty(t, runs)

	err = s.DeleteTask("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskNotFound))
}

func TestTaskRunStore(t *testing.T) {
	s := setupTestStore(t)
	require.NoError(t, s.RunMigrations())

	run := &agentruntime.TaskRun{
		TaskID:           "task-1",
		RuntimeSessionID: "runtime-session-1",
	}
	require.NoError(t, s.CreateTaskRun(run))
	require.NotEmpty(t, run.ID)
	assert.Equal(t, agentruntime.TaskStatusRunning, run.Status)
	assert.NotZero(t, run.StartedAt)
	assert.JSONEq(t, `{}`, string(run.Usage))

	require.NoError(t, s.UpdateTaskRunRuntimeSession(run.ID, "runtime-session-2"))

	require.NoError(t, s.UpdateTaskRunResult(
		run.ID,
		agentruntime.TaskStatusCompleted,
		"post-1",
		"",
		[]byte(`{"input_tokens":10}`),
	))

	runs, err := s.ListTaskRuns("task-1")
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "runtime-session-2", runs[0].RuntimeSessionID)
	assert.Equal(t, agentruntime.TaskStatusCompleted, runs[0].Status)
	assert.Equal(t, "post-1", runs[0].ResultPostID)
	assert.JSONEq(t, `{"input_tokens":10}`, string(runs[0].Usage))
	assert.NotZero(t, runs[0].FinishedAt)

	err = s.UpdateTaskRunResult("missing", agentruntime.TaskStatusFailed, "", "boom", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskRunNotFound))
}
