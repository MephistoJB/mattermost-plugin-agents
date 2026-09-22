// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package taskscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	tasks         []agentruntime.Task
	runs          []agentruntime.TaskRun
	taskStatuses  []agentruntime.TaskStatus
	runStatuses   []agentruntime.TaskStatus
	resultPostIDs []string
	usages        []json.RawMessage
}

func (s *fakeStore) ClaimDueTasks(_ int64, limit int) ([]agentruntime.Task, error) {
	if len(s.tasks) < limit {
		limit = len(s.tasks)
	}
	claimed := append([]agentruntime.Task(nil), s.tasks[:limit]...)
	s.tasks = s.tasks[limit:]
	return claimed, nil
}

func (s *fakeStore) ListTasks(filter store.TaskFilter) ([]agentruntime.Task, error) {
	var tasks []agentruntime.Task
	for _, task := range s.tasks {
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *fakeStore) ListTaskRuns(taskID string) ([]agentruntime.TaskRun, error) {
	var runs []agentruntime.TaskRun
	for _, run := range s.runs {
		if run.TaskID == taskID {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (s *fakeStore) CreateTaskRun(run *agentruntime.TaskRun) error {
	if run.ID == "" {
		run.ID = "run-1"
	}
	s.runs = append(s.runs, *run)
	return nil
}

func (s *fakeStore) UpdateTaskRunRuntimeSession(id, runtimeSessionID string) error {
	for i := range s.runs {
		if s.runs[i].ID == id {
			s.runs[i].RuntimeSessionID = runtimeSessionID
		}
	}
	return nil
}

func (s *fakeStore) UpdateTaskRunResult(id string, status agentruntime.TaskStatus, resultPostID, errorText string, usage json.RawMessage) error {
	s.runStatuses = append(s.runStatuses, status)
	s.resultPostIDs = append(s.resultPostIDs, resultPostID)
	s.usages = append(s.usages, usage)
	for i := range s.runs {
		if s.runs[i].ID == id {
			s.runs[i].Status = status
			s.runs[i].ResultPostID = resultPostID
			s.runs[i].Error = errorText
			s.runs[i].Usage = usage
		}
	}
	return nil
}

func (s *fakeStore) UpdateTaskStatus(id string, status agentruntime.TaskStatus, lastRunAt, nextRunAt int64) error {
	s.taskStatuses = append(s.taskStatuses, status)
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status
			s.tasks[i].LastRunAt = lastRunAt
			if nextRunAt >= 0 {
				s.tasks[i].NextRunAt = nextRunAt
			}
		}
	}
	return nil
}

type fakeRuntimeControl struct {
	events chan agentruntime.RuntimeEvent
	req    runtimecontrol.StartTurnRequest
	err    error
	calls  int
}

type fakeScheduleParser struct {
	next time.Time
	ok   bool
}

type fakePostClient struct {
	posts []model.Post
}

type fakeThreadReader struct {
	thread *model.PostList
	err    error
}

func (c *fakePostClient) CreatePost(post *model.Post) error {
	if post.Id == "" {
		post.Id = "post-1"
	}
	c.posts = append(c.posts, *post)
	return nil
}

func (p fakeScheduleParser) NextRunAfter(string, time.Time) (time.Time, bool) {
	return p.next, p.ok
}

func (r *fakeRuntimeControl) StartTurn(_ context.Context, req runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error) {
	r.calls++
	r.req = req
	if r.err != nil {
		return nil, r.err
	}
	return &runtimecontrol.StartTurnResult{
		Session: agentruntime.RuntimeSession{ID: "runtime-session-1"},
		Events:  r.events,
	}, nil
}

func (r fakeThreadReader) GetPostThread(string) (*model.PostList, error) {
	return r.thread, r.err
}

func TestRunDueOnceStartsRuntimeAndCompletesTask(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "done"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:            "task-1",
			Prompt:        "remind me",
			TaskType:      agentruntime.TaskTypeReminder,
			ChannelID:     "channel-1",
			RootPostID:    "root-1",
			UserID:        "user-1",
			AgentID:       "agent-1",
			WorkspacePath: "/workspace/project",
			RuntimePolicySnapshot: []byte(`{
				"runtimeType":"local",
				"providerID":"ollama",
				"model":"gpt-oss:20b",
				"workspacePolicyID":"workspace-policy-1",
				"allowLocal":true
			}`),
			Metadata: []byte(`{"kind":"reminder"}`),
		},
	}}
	runtimeControl := &fakeRuntimeControl{events: events}
	postClient := &fakePostClient{}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: runtimeControl,
		PostClient:     postClient,
		Now:            func() time.Time { return time.UnixMilli(5000) },
		ServerID:       "server-1",
		TeamID:         "team-1",
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	require.Len(t, store.runs, 1)
	assert.Equal(t, "task-1", store.runs[0].TaskID)
	assert.Equal(t, "task-1", runtimeControl.req.SessionRequest.MattermostConversationID)
	assert.Equal(t, "channel-1", runtimeControl.req.SessionRequest.ChannelID)
	require.NotNil(t, runtimeControl.req.SessionRequest.PolicyOverride)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, runtimeControl.req.SessionRequest.PolicyOverride.RuntimeType)
	assert.Equal(t, "workspace-policy-1", runtimeControl.req.SessionRequest.PolicyOverride.WorkspacePolicyID)
	assert.Equal(t, "remind me", runtimeControl.req.Prompt)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusCompleted}, store.runStatuses)
	assert.Equal(t, []string{"post-1"}, store.resultPostIDs)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusCompleted}, store.taskStatuses)
	require.Len(t, postClient.posts, 1)
	assert.Equal(t, "channel-1", postClient.posts[0].ChannelId)
	assert.Equal(t, "root-1", postClient.posts[0].RootId)
	assert.Equal(t, "agent-1", postClient.posts[0].UserId)
	assert.Equal(t, "done", postClient.posts[0].Message)
}

func TestRunDueOnceFailsTaskOnInvalidRuntimePolicySnapshot(t *testing.T) {
	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:                    "task-1",
			Prompt:                "go",
			UserID:                "user-1",
			AgentID:               "agent-1",
			RuntimePolicySnapshot: json.RawMessage(`{`),
		},
	}}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: &fakeRuntimeControl{},
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusFailed}, store.runStatuses)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusFailed}, store.taskStatuses)
}

func TestRunDueOnceMarksTaskFailedOnRuntimeError(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{{ID: "task-1", Prompt: "go", UserID: "user-1", AgentID: "agent-1"}}}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: &fakeRuntimeControl{events: events},
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusFailed}, store.runStatuses)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusFailed}, store.taskStatuses)
}

func TestRunDueOnceSanitizesTaskRunError(t *testing.T) {
	const leakedKey = "sk-proj-1234567890abcdefghijklmnop"
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{
		Type: agentruntime.EventTypeError,
		Err:  fmt.Errorf("provider failed Authorization: Bearer %s", leakedKey),
	}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{{ID: "task-1", Prompt: "go", UserID: "user-1", AgentID: "agent-1"}}}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: &fakeRuntimeControl{events: events},
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	require.Len(t, store.runs, 1)
	assert.NotContains(t, store.runs[0].Error, leakedKey)
	assert.Contains(t, store.runs[0].Error, "[REDACTED]")
}

func TestRunDueOnceQueuesRecurringTaskWhenNextRunIsKnown(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:           "task-1",
			Prompt:       "watch",
			TaskType:     agentruntime.TaskTypeRecurring,
			UserID:       "user-1",
			AgentID:      "agent-1",
			ScheduleSpec: "every day 09:00",
		},
	}}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: &fakeRuntimeControl{events: events},
		ScheduleParser: fakeScheduleParser{next: time.UnixMilli(9000), ok: true},
		Now:            func() time.Time { return time.UnixMilli(5000) },
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusQueued}, store.taskStatuses)
}

func TestRunDueOncePausesRecurringTaskWithoutNextRun(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:       "task-1",
			Prompt:   "watch",
			TaskType: agentruntime.TaskTypeRecurring,
			UserID:   "user-1",
			AgentID:  "agent-1",
		},
	}}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: &fakeRuntimeControl{events: events},
		Now:            func() time.Time { return time.UnixMilli(5000) },
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusPaused}, store.taskStatuses)
}

func TestRunDueOnceRunsNoReplyFollowupWhenThreadHasNoNewReply(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)

	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:         "task-1",
			Prompt:     "follow up",
			TaskType:   agentruntime.TaskTypeWatcher,
			UserID:     "user-1",
			AgentID:    "agent-1",
			ChannelID:  "channel-1",
			RootPostID: "root-1",
			CreatedAt:  1000,
			Metadata:   json.RawMessage(`{"kind":"followup","condition":"no_reply","rootPostID":"root-1"}`),
		},
	}}
	runtimeControl := &fakeRuntimeControl{events: events}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: runtimeControl,
		ThreadReader: fakeThreadReader{thread: &model.PostList{
			Order: []string{"root-1", "agent-post"},
			Posts: map[string]*model.Post{
				"root-1":     {Id: "root-1", UserId: "user-1", CreateAt: 500},
				"agent-post": {Id: "agent-post", UserId: "agent-1", CreateAt: 1500},
			},
		}},
		Now: func() time.Time { return time.UnixMilli(5000) },
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, 1, runtimeControl.calls)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusCompleted}, store.taskStatuses)
}

func TestRunDueOnceSkipsNoReplyFollowupWhenThreadHasNewReply(t *testing.T) {
	store := &fakeStore{tasks: []agentruntime.Task{
		{
			ID:         "task-1",
			Prompt:     "follow up",
			TaskType:   agentruntime.TaskTypeWatcher,
			UserID:     "user-1",
			AgentID:    "agent-1",
			ChannelID:  "channel-1",
			RootPostID: "root-1",
			CreatedAt:  1000,
			Metadata:   json.RawMessage(`{"kind":"followup","condition":"no_reply","rootPostID":"root-1"}`),
		},
	}}
	runtimeControl := &fakeRuntimeControl{}
	scheduler := New(Options{
		Store:          store,
		RuntimeControl: runtimeControl,
		ThreadReader: fakeThreadReader{thread: &model.PostList{
			Order: []string{"root-1", "human-reply"},
			Posts: map[string]*model.Post{
				"root-1":      {Id: "root-1", UserId: "user-1", CreateAt: 500},
				"human-reply": {Id: "human-reply", UserId: "user-2", CreateAt: 1500},
			},
		}},
		Now: func() time.Time { return time.UnixMilli(5000) },
	})

	claimed, err := scheduler.RunDueOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, claimed)
	assert.Equal(t, 0, runtimeControl.calls)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusCompleted}, store.runStatuses)
	assert.Equal(t, []agentruntime.TaskStatus{agentruntime.TaskStatusCompleted}, store.taskStatuses)
	require.Len(t, store.usages, 1)
	assert.JSONEq(t, `{"skipped":true,"reason":"reply_detected"}`, string(store.usages[0]))
}

func TestRecoverInterruptedTasksMarksRunningTasksFailed(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		tasks: []agentruntime.Task{
			{ID: "task-running", Status: agentruntime.TaskStatusRunning},
			{ID: "task-waiting", Status: agentruntime.TaskStatusWaitingApproval},
			{ID: "task-fresh", Status: agentruntime.TaskStatusRunning, UpdatedAt: now.UnixMilli()},
			{ID: "task-queued", Status: agentruntime.TaskStatusQueued},
		},
		runs: []agentruntime.TaskRun{
			{ID: "run-running", TaskID: "task-running", Status: agentruntime.TaskStatusRunning, ResultPostID: "post-1"},
			{ID: "run-waiting", TaskID: "task-waiting", Status: agentruntime.TaskStatusWaitingApproval},
		},
	}
	scheduler := New(Options{
		Store: store,
		Now:   func() time.Time { return now },
	})

	require.NoError(t, scheduler.RecoverInterruptedTasks(context.Background()))

	assert.Equal(t, agentruntime.TaskStatusFailed, store.tasks[0].Status)
	assert.Equal(t, agentruntime.TaskStatusFailed, store.tasks[1].Status)
	assert.Equal(t, agentruntime.TaskStatusRunning, store.tasks[2].Status)
	assert.Equal(t, agentruntime.TaskStatusQueued, store.tasks[3].Status)
	assert.Equal(t, now.UnixMilli(), store.tasks[0].LastRunAt)
	assert.Contains(t, store.taskStatuses, agentruntime.TaskStatusFailed)
	assert.Equal(t, agentruntime.TaskStatusFailed, store.runs[0].Status)
	assert.Equal(t, "interrupted by plugin restart", store.runs[0].Error)
	assert.JSONEq(t, `{"interrupted":true,"reason":"plugin_restart"}`, string(store.runs[0].Usage))
	assert.Equal(t, agentruntime.TaskStatusFailed, store.runs[1].Status)
	assert.Equal(t, "interrupted by plugin restart", store.runs[1].Error)
}

func TestRecoverInterruptedTasksRequiresStore(t *testing.T) {
	scheduler := New(Options{})

	require.Error(t, scheduler.RecoverInterruptedTasks(context.Background()))
}
