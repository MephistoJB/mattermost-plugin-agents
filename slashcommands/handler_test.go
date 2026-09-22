// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package slashcommands

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	enabled bool
}

func (c testConfig) EnableAgentRuntimeControlPlane() bool {
	return c.enabled
}

type testStore struct {
	policies      []agentruntime.RuntimePolicy
	tasks         []agentruntime.Task
	sessions      []agentruntime.RuntimeSession
	approvals     []agentruntime.RuntimeApproval
	supervisors   []agentruntime.SupervisorRun
	subagents     []agentruntime.SubagentRun
	taskStatusSet []agentruntime.TaskStatus
}

func (s *testStore) CreateRuntimeSession(session *agentruntime.RuntimeSession) error {
	if session.ID == "" {
		session.ID = "session-1"
	}
	s.sessions = append(s.sessions, *session)
	return nil
}

func (s *testStore) GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error) {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i], nil
		}
	}
	return nil, store.ErrRuntimeSessionNotFound
}

func (s *testStore) UpsertRuntimePolicy(policy *agentruntime.RuntimePolicy) error {
	s.policies = append(s.policies, *policy)
	return nil
}

func (s *testStore) GetRuntimePolicy(scopeType agentruntime.PolicyScopeType, scopeID string) (*agentruntime.RuntimePolicy, error) {
	for i := range s.policies {
		if s.policies[i].ScopeType == scopeType && s.policies[i].ScopeID == scopeID {
			return &s.policies[i], nil
		}
	}
	return nil, store.ErrRuntimePolicyNotFound
}

func (s *testStore) ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	out := []agentruntime.RuntimeSession{}
	for _, session := range s.sessions {
		if filter.ServerID != "" && session.ServerID != filter.ServerID {
			continue
		}
		if filter.TeamID != "" && session.TeamID != filter.TeamID {
			continue
		}
		if filter.UserID != "" && session.UserID != filter.UserID {
			continue
		}
		if filter.ChannelID != "" && session.ChannelID != filter.ChannelID {
			continue
		}
		if filter.RootPostID != "" && session.RootPostID != filter.RootPostID {
			continue
		}
		if filter.Status != "" && session.Status != filter.Status {
			continue
		}
		out = append(out, session)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *testStore) UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Status = status
			s.sessions[i].LastError = lastError
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *testStore) UpdateRuntimeSessionMetadata(id string, metadata json.RawMessage) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Metadata = metadata
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *testStore) CreateTask(task *agentruntime.Task) error {
	task.ID = "task-1"
	s.tasks = append(s.tasks, *task)
	return nil
}

func (s *testStore) GetTask(id string) (*agentruntime.Task, error) {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i], nil
		}
	}
	return nil, store.ErrTaskNotFound
}

func (s *testStore) ListTasks(filter store.TaskFilter) ([]agentruntime.Task, error) {
	out := []agentruntime.Task{}
	for _, task := range s.tasks {
		if filter.UserID != "" && task.UserID != filter.UserID {
			continue
		}
		if filter.ChannelID != "" && task.ChannelID != filter.ChannelID {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		out = append(out, task)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *testStore) UpdateTaskStatus(id string, status agentruntime.TaskStatus, lastRunAt, nextRunAt int64) error {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status
			if nextRunAt >= 0 {
				s.tasks[i].NextRunAt = nextRunAt
			}
			s.taskStatusSet = append(s.taskStatusSet, status)
			return nil
		}
	}
	return store.ErrTaskNotFound
}

func (s *testStore) DeleteTask(id string) error {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return nil
		}
	}
	return store.ErrTaskNotFound
}

func (s *testStore) ListRuntimeApprovals(filter store.RuntimeApprovalFilter) ([]agentruntime.RuntimeApproval, error) {
	out := []agentruntime.RuntimeApproval{}
	for _, approval := range s.approvals {
		if filter.RequestedBy != "" && approval.RequestedBy != filter.RequestedBy {
			continue
		}
		if filter.Status != "" && approval.Status != filter.Status {
			continue
		}
		out = append(out, approval)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *testStore) ListSupervisorRuns(filter store.SupervisorRunFilter) ([]agentruntime.SupervisorRun, error) {
	out := []agentruntime.SupervisorRun{}
	for _, supervisor := range s.supervisors {
		if filter.RuntimeSessionID != "" && supervisor.RuntimeSessionID != filter.RuntimeSessionID {
			continue
		}
		if filter.CreatedBy != "" && supervisor.CreatedBy != filter.CreatedBy {
			continue
		}
		out = append(out, supervisor)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *testStore) ListSubagentRuns(supervisorRunID string) ([]agentruntime.SubagentRun, error) {
	out := []agentruntime.SubagentRun{}
	for _, subagent := range s.subagents {
		if subagent.SupervisorRunID == supervisorRunID {
			out = append(out, subagent)
		}
	}
	return out, nil
}

type testRuntimeControl struct {
	decision agentruntime.RuntimeApprovalDecision
}

type testPermissions struct {
	canManageRuntimePolicy bool
}

func (p testPermissions) CanManageRuntimePolicy(string, string) bool {
	return p.canManageRuntimePolicy
}

type testAuditLogger struct {
	runtimePolicyEvents []RuntimePolicyAuditEvent
	taskEvents          []TaskAuditEvent
}

func (l *testAuditLogger) RecordRuntimePolicyChange(event RuntimePolicyAuditEvent) {
	l.runtimePolicyEvents = append(l.runtimePolicyEvents, event)
}

func (l *testAuditLogger) RecordTaskChange(event TaskAuditEvent) {
	l.taskEvents = append(l.taskEvents, event)
}

func (c *testRuntimeControl) StopSession(context.Context, string) error {
	return nil
}

func (c *testRuntimeControl) ResumeSession(context.Context, string) (<-chan agentruntime.RuntimeEvent, error) {
	events := make(chan agentruntime.RuntimeEvent)
	close(events)
	return events, nil
}

func (c *testRuntimeControl) GetStatus(context.Context, string) (agentruntime.RuntimeStatus, error) {
	return agentruntime.RuntimeStatus{}, nil
}

func (c *testRuntimeControl) SubmitApproval(_ context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	c.decision = decision
	return nil
}

func (c *testRuntimeControl) ExpireApprovals(context.Context) error {
	return nil
}

func TestRuntimeCommandSetsThreadPolicy(t *testing.T) {
	st := &testStore{}
	auditLog := &testAuditLogger{}
	handler := New(Options{
		Config:      testConfig{enabled: true},
		Store:       st,
		Permissions: testPermissions{canManageRuntimePolicy: true},
		Audit:       auditLog,
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/runtime local",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "thread")
	require.Len(t, st.policies, 1)
	assert.Equal(t, agentruntime.PolicyScopeThread, st.policies[0].ScopeType)
	assert.Equal(t, "root-1", st.policies[0].ScopeID)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, st.policies[0].RuntimeType)
	assert.True(t, st.policies[0].AllowLocal)
	assert.False(t, st.policies[0].AllowCloud)
	require.Len(t, auditLog.runtimePolicyEvents, 1)
	assert.Equal(t, "user-1", auditLog.runtimePolicyEvents[0].UserID)
	assert.Equal(t, "channel-1", auditLog.runtimePolicyEvents[0].ChannelID)
	assert.Equal(t, "root-1", auditLog.runtimePolicyEvents[0].RootPostID)
	assert.Equal(t, agentruntime.PolicyScopeThread, auditLog.runtimePolicyEvents[0].ScopeType)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, auditLog.runtimePolicyEvents[0].RuntimeType)
	assert.ElementsMatch(t, []string{"runtimeType", "providerID", "allowCloud", "allowLocal"}, auditLog.runtimePolicyEvents[0].ChangedFields)
}

func TestRuntimeCommandRejectsPolicyUpdateWithoutPermission(t *testing.T) {
	st := &testStore{}
	auditLog := &testAuditLogger{}
	handler := New(Options{
		Config:      testConfig{enabled: true},
		Store:       st,
		Permissions: testPermissions{canManageRuntimePolicy: false},
		Audit:       auditLog,
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/runtime local",
		UserId:    "user-1",
		ChannelId: "channel-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "do not have permission")
	assert.Empty(t, st.policies)
	assert.Empty(t, auditLog.runtimePolicyEvents)
}

func TestTaskCommandCreatesReminder(t *testing.T) {
	st := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:         agentruntime.PolicyScopeThread,
				ScopeID:           "root-1",
				RuntimeType:       agentruntime.RuntimeTypeLocal,
				ProviderID:        "ollama",
				Model:             "gpt-oss:20b",
				WorkspacePolicyID: "workspace-policy-1",
				AllowLocal:        true,
			},
		},
	}
	auditLog := &testAuditLogger{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Audit:  auditLog,
		Now:    func() time.Time { return time.UnixMilli(1000) },
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/task reminder 2000 --agent agent-1 follow up here",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, agentruntime.TaskTypeReminder, st.tasks[0].TaskType)
	assert.Equal(t, "agent-1", st.tasks[0].AgentID)
	assert.Equal(t, int64(2000), st.tasks[0].NextRunAt)
	assert.Equal(t, "follow up here", st.tasks[0].Prompt)
	assert.JSONEq(t, `{
		"runtimeType":"local",
		"providerID":"ollama",
		"model":"gpt-oss:20b",
		"workspacePolicyID":"workspace-policy-1",
		"approvalPolicyID":"",
		"allowCloud":false,
		"allowLocal":true
	}`, string(st.tasks[0].RuntimePolicySnapshot))
	require.Len(t, auditLog.taskEvents, 1)
	assert.Equal(t, "create", auditLog.taskEvents[0].Action)
	assert.Equal(t, st.tasks[0].ID, auditLog.taskEvents[0].TaskID)
	assert.Equal(t, agentruntime.TaskTypeReminder, auditLog.taskEvents[0].TaskType)
	assert.Equal(t, agentruntime.TaskStatusQueued, auditLog.taskEvents[0].TaskStatus)
	assert.Equal(t, "agent-1", auditLog.taskEvents[0].AgentID)
	assert.ElementsMatch(t, []string{"taskType", "status", "agentID", "nextRunAt", "scheduleSpec", "runtimePolicySnapshot"}, auditLog.taskEvents[0].ChangedFields)
}

func TestRemindCommandCreatesDurationReminder(t *testing.T) {
	st := &testStore{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Now:    func() time.Time { return time.UnixMilli(1000).UTC() },
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/remind me in 2 days --agent agent-1 backup pruefen",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, agentruntime.TaskTypeReminder, st.tasks[0].TaskType)
	assert.Equal(t, "agent-1", st.tasks[0].AgentID)
	assert.Equal(t, int64(172801000), st.tasks[0].NextRunAt)
	assert.Equal(t, "root-1", st.tasks[0].RootPostID)
	assert.Equal(t, "backup pruefen", st.tasks[0].Prompt)
	assert.JSONEq(t, `{"kind":"reminder","scope":"me"}`, string(st.tasks[0].Metadata))
}

func TestRemindCommandCreatesTomorrowReminder(t *testing.T) {
	st := &testStore{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Now:    func() time.Time { return time.Date(2026, 8, 18, 12, 30, 0, 0, time.UTC) },
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/remind thread tomorrow 09:00 status pruefen",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC).UnixMilli(), st.tasks[0].NextRunAt)
	assert.Equal(t, "root-1", st.tasks[0].RootPostID)
	assert.Equal(t, "status pruefen", st.tasks[0].Prompt)
	assert.JSONEq(t, `{"kind":"reminder","scope":"thread"}`, string(st.tasks[0].Metadata))
}

func TestRemindCommandCreatesWeeklyChannelReminder(t *testing.T) {
	st := &testStore{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Now:    func() time.Time { return time.Date(2026, 8, 18, 12, 30, 0, 0, time.UTC) },
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/remind channel every friday 15:00 Wochenstatus",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, agentruntime.TaskTypeRecurring, st.tasks[0].TaskType)
	assert.Equal(t, time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC).UnixMilli(), st.tasks[0].NextRunAt)
	assert.Equal(t, "every friday 15:00", st.tasks[0].ScheduleSpec)
	assert.Empty(t, st.tasks[0].RootPostID)
	assert.Equal(t, "Wochenstatus", st.tasks[0].Prompt)
	assert.JSONEq(t, `{"kind":"reminder","scope":"channel"}`, string(st.tasks[0].Metadata))
}

func TestFollowupCommandCreatesNoReplyWatcher(t *testing.T) {
	st := &testStore{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Now:    func() time.Time { return time.UnixMilli(1000).UTC() },
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/followup in 1 week if no reply bitte nachfassen",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, agentruntime.TaskTypeWatcher, st.tasks[0].TaskType)
	assert.Equal(t, int64(604801000), st.tasks[0].NextRunAt)
	assert.Equal(t, "root-1", st.tasks[0].RootPostID)
	assert.Equal(t, "bitte nachfassen", st.tasks[0].Prompt)
	assert.JSONEq(t, `{"kind":"followup","condition":"no_reply","rootPostID":"root-1"}`, string(st.tasks[0].Metadata))
}

func TestTaskCommandUsesDefaultAgentWhenUnset(t *testing.T) {
	st := &testStore{}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{
		Command: "/task create do the thing",
		UserId:  "user-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.tasks, 1)
	assert.Equal(t, DefaultTaskAgentID, st.tasks[0].AgentID)
}

func TestTaskCommandPausesCancelsAndSnoozesOwnedTask(t *testing.T) {
	st := &testStore{
		tasks: []agentruntime.Task{
			{
				ID:        "task-1",
				UserID:    "user-1",
				Status:    agentruntime.TaskStatusQueued,
				NextRunAt: 2000,
			},
		},
	}
	auditLog := &testAuditLogger{}
	handler := New(Options{
		Config: testConfig{enabled: true},
		Store:  st,
		Audit:  auditLog,
		Now:    func() time.Time { return time.UnixMilli(1000) },
	})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/task pause task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusPaused, st.tasks[0].Status)
	assert.Equal(t, int64(2000), st.tasks[0].NextRunAt)

	resp, err = handler.Execute(&model.CommandArgs{Command: "/task resume task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusQueued, st.tasks[0].Status)
	assert.Equal(t, int64(61000), st.tasks[0].NextRunAt)

	resp, err = handler.Execute(&model.CommandArgs{Command: "/task run task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusQueued, st.tasks[0].Status)
	assert.Equal(t, int64(1000), st.tasks[0].NextRunAt)

	resp, err = handler.Execute(&model.CommandArgs{Command: "/task snooze task-1 15m", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusQueued, st.tasks[0].Status)
	assert.Equal(t, int64(901000), st.tasks[0].NextRunAt)

	resp, err = handler.Execute(&model.CommandArgs{Command: "/task cancel task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusCancelled, st.tasks[0].Status)
	assert.Equal(t, int64(0), st.tasks[0].NextRunAt)

	st.tasks[0].Status = agentruntime.TaskStatusQueued
	resp, err = handler.Execute(&model.CommandArgs{Command: "/task stop task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.TaskStatusCancelled, st.tasks[0].Status)
	require.Len(t, auditLog.taskEvents, 6)
	assert.Equal(t, "paused", auditLog.taskEvents[0].Action)
	assert.Equal(t, "queued", auditLog.taskEvents[1].Action)
	assert.Equal(t, "run", auditLog.taskEvents[2].Action)
	assert.Equal(t, "snooze", auditLog.taskEvents[3].Action)
	assert.Equal(t, "cancelled", auditLog.taskEvents[4].Action)
	assert.Equal(t, "cancelled", auditLog.taskEvents[5].Action)
}

func TestTaskCommandDeletesOwnedTask(t *testing.T) {
	st := &testStore{
		tasks: []agentruntime.Task{
			{
				ID:     "task-1",
				UserID: "user-1",
				Status: agentruntime.TaskStatusQueued,
			},
		},
	}
	auditLog := &testAuditLogger{}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st, Audit: auditLog})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/task delete task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "deleted")
	assert.Empty(t, st.tasks)
	require.Len(t, auditLog.taskEvents, 1)
	assert.Equal(t, "delete", auditLog.taskEvents[0].Action)
	assert.Equal(t, "task-1", auditLog.taskEvents[0].TaskID)
	assert.ElementsMatch(t, []string{"deleted"}, auditLog.taskEvents[0].ChangedFields)
}

func TestTaskCommandRejectsTaskOwnedByAnotherUser(t *testing.T) {
	st := &testStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "other-user", Status: agentruntime.TaskStatusQueued},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/task cancel task-1", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "was not found")
	assert.Equal(t, agentruntime.TaskStatusQueued, st.tasks[0].Status)
}

func TestModelCommandUpdatesScopedPolicy(t *testing.T) {
	st := &testStore{}
	auditLog := &testAuditLogger{}
	handler := New(Options{
		Config:      testConfig{enabled: true},
		Store:       st,
		Permissions: testPermissions{canManageRuntimePolicy: true},
		Audit:       auditLog,
	})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/model gpt-5-codex",
		UserId:    "user-1",
		ChannelId: "channel-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.policies, 1)
	assert.Equal(t, agentruntime.PolicyScopeChannel, st.policies[0].ScopeType)
	assert.Equal(t, "gpt-5-codex", st.policies[0].Model)
	assert.Equal(t, agentruntime.RuntimeTypeCodex, st.policies[0].RuntimeType)
	require.Len(t, auditLog.runtimePolicyEvents, 1)
	assert.Equal(t, agentruntime.PolicyScopeChannel, auditLog.runtimePolicyEvents[0].ScopeType)
	assert.Equal(t, "channel-1", auditLog.runtimePolicyEvents[0].ScopeID)
	assert.ElementsMatch(t, []string{"model"}, auditLog.runtimePolicyEvents[0].ChangedFields)
}

func TestNewCommandCreatesRuntimeSession(t *testing.T) {
	st := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:   agentruntime.PolicyScopeThread,
				ScopeID:     "root-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				ProviderID:  "ollama",
				Model:       "gpt-oss:20b",
				AllowLocal:  true,
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/new agent-1",
		UserId:    "user-1",
		ChannelId: "channel-1",
		RootId:    "root-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, st.sessions, 1)
	assert.Equal(t, "root-1", st.sessions[0].MattermostConversationID)
	assert.Equal(t, "agent-1", st.sessions[0].AgentID)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, st.sessions[0].RuntimeType)
}

func TestStopCommandMarksSessionCancelled(t *testing.T) {
	st := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				UserID:      "user-1",
				ChannelID:   "channel-1",
				Status:      agentruntime.SessionStatusRunning,
				RuntimeType: agentruntime.RuntimeTypeLocal,
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/stop session-1",
		UserId:    "user-1",
		ChannelId: "channel-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, agentruntime.SessionStatusCancelled, st.sessions[0].Status)
}

func TestStopCommandRejectsForeignSessionID(t *testing.T) {
	st := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				UserID:      "user-1",
				ChannelID:   "channel-1",
				Status:      agentruntime.SessionStatusRunning,
				RuntimeType: agentruntime.RuntimeTypeLocal,
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/stop session-1",
		UserId:    "user-2",
		ChannelId: "channel-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "runtime session `session-1` was not found")
	assert.Equal(t, agentruntime.SessionStatusRunning, st.sessions[0].Status)
}

func TestStatusCommandIncludesRuntimeContext(t *testing.T) {
	st := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:                "session-1",
				UserID:            "user-1",
				ChannelID:         "channel-1",
				RootPostID:        "root-1",
				Status:            agentruntime.SessionStatusRunning,
				RuntimeType:       agentruntime.RuntimeTypeLocal,
				ProviderID:        "local",
				Model:             "gpt-oss:20b",
				WorkspacePath:     "/workspace/project",
				ExternalSessionID: "local-session-1",
			},
		},
		approvals: []agentruntime.RuntimeApproval{
			{
				ID:               "approval-1",
				RuntimeSessionID: "session-1",
				RequestedBy:      "user-1",
				Status:           agentruntime.ApprovalStatusPending,
			},
		},
		tasks: []agentruntime.Task{
			{
				ID:         "task-1",
				UserID:     "user-1",
				ChannelID:  "channel-1",
				RootPostID: "root-1",
				Status:     agentruntime.TaskStatusRunning,
			},
		},
		supervisors: []agentruntime.SupervisorRun{
			{
				ID:               "supervisor-1",
				RuntimeSessionID: "session-1",
				CreatedBy:        "user-1",
				Status:           agentruntime.RunStatusRunning,
			},
		},
		subagents: []agentruntime.SubagentRun{
			{
				ID:              "subagent-1",
				SupervisorRunID: "supervisor-1",
				Role:            "reviewer",
				Status:          agentruntime.RunStatusRunning,
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{
		Command:   "/status session-1",
		UserId:    "user-1",
		ChannelId: "channel-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "external `local-session-1`")
	assert.Contains(t, resp.Text, "workspace `/workspace/project`")
	assert.Contains(t, resp.Text, "Pending approvals: 1")
	assert.Contains(t, resp.Text, "Tasks: task-1:running")
	assert.Contains(t, resp.Text, "Supervisor `supervisor-1`: running, 1 subagents")
	assert.Contains(t, resp.Text, "reviewer:running")
}

func TestUsageCommandAggregatesRuntimeUsage(t *testing.T) {
	st := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				UserID:      "user-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Metadata:    json.RawMessage(`{"usage":{"input_tokens":100,"output_tokens":25,"duration_ms":1200,"cost":0.01}}`),
			},
			{
				ID:          "session-2",
				UserID:      "user-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				Metadata:    json.RawMessage(`{"usage":{"input_tokens":50,"output_tokens":10,"duration_ms":2200}}`),
			},
			{
				ID:          "session-3",
				UserID:      "other-user",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Metadata:    json.RawMessage(`{"usage":{"input_tokens":999,"output_tokens":999,"duration_ms":999}}`),
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/usage", UserId: "user-1"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "Runtime usage summary: `2` sessions, `150` input tokens, `35` output tokens")
	assert.Contains(t, resp.Text, "- `codex`: 1 sessions, 100 in, 25 out, 1s, $0.0100")
	assert.Contains(t, resp.Text, "- `local`: 1 sessions, 50 in, 10 out, 2s, $0.0000")
	assert.NotContains(t, resp.Text, "999")
}

func TestApprovalsCommandListsWaitingSessions(t *testing.T) {
	st := &testStore{
		approvals: []agentruntime.RuntimeApproval{
			{
				ID:                 "approval-1",
				RuntimeSessionID:   "session-1",
				ExternalApprovalID: "provider-approval-1",
				RequestedBy:        "user-1",
				Status:             agentruntime.ApprovalStatusPending,
			},
			{
				ID:                 "approval-2",
				RuntimeSessionID:   "session-2",
				ExternalApprovalID: "provider-approval-2",
				RequestedBy:        "user-1",
				Status:             agentruntime.ApprovalStatusAccepted,
			},
		},
	}
	handler := New(Options{Config: testConfig{enabled: true}, Store: st})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/approvals", UserId: "user-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "approval-1")
	assert.NotContains(t, resp.Text, "approval-2")
}

func TestApproveCommandSubmitsApprovalDecision(t *testing.T) {
	rt := &testRuntimeControl{}
	handler := New(Options{Config: testConfig{enabled: true}, Store: &testStore{
		approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		},
	}, RuntimeControl: rt})

	resp, err := handler.Execute(&model.CommandArgs{
		Command: "/approve approval-1 looks safe",
		UserId:  "user-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "approved")
	assert.Equal(t, "approval-1", rt.decision.ApprovalID)
	assert.Equal(t, "user-1", rt.decision.UserID)
	assert.Equal(t, agentruntime.ApprovalDecisionAccept, rt.decision.Decision)
	assert.Equal(t, "looks safe", rt.decision.Reason)
}

func TestApproveCommandRejectsForeignApproval(t *testing.T) {
	rt := &testRuntimeControl{}
	handler := New(Options{Config: testConfig{enabled: true}, Store: &testStore{
		approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		},
	}, RuntimeControl: rt})

	resp, err := handler.Execute(&model.CommandArgs{
		Command: "/approve approval-1",
		UserId:  "user-2",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "Runtime approval `approval-1` was not found.")
	assert.Empty(t, rt.decision.Decision)
}

func TestDenyCommandSubmitsApprovalDecision(t *testing.T) {
	rt := &testRuntimeControl{}
	handler := New(Options{Config: testConfig{enabled: true}, Store: &testStore{
		approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		},
	}, RuntimeControl: rt})

	resp, err := handler.Execute(&model.CommandArgs{
		Command: "/deny approval-1 too risky",
		UserId:  "user-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "denied")
	assert.Equal(t, "approval-1", rt.decision.ApprovalID)
	assert.Equal(t, agentruntime.ApprovalDecisionDeny, rt.decision.Decision)
	assert.Equal(t, "too risky", rt.decision.Reason)
}

func TestCommandDisabled(t *testing.T) {
	handler := New(Options{Config: testConfig{enabled: false}, Store: &testStore{}})

	resp, err := handler.Execute(&model.CommandArgs{Command: "/runtime status"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Text, "disabled")
}
