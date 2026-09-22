// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/conversations"
	"github.com/mattermost/mattermost-plugin-agents/v2/enterprise"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	mmapimocks "github.com/mattermost/mattermost-plugin-agents/v2/mmapi/mocks"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost-plugin-agents/v2/ttsbroker"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type runtimeHealthTranscribeConfig struct {
	transcriptGenerator string
}

func (c runtimeHealthTranscribeConfig) GetBots() []llm.BotConfig {
	return nil
}

func (c runtimeHealthTranscribeConfig) GetServiceByID(string) (llm.ServiceConfig, bool) {
	return llm.ServiceConfig{}, false
}

func (c runtimeHealthTranscribeConfig) GetDefaultBotName() string {
	return ""
}

func (c runtimeHealthTranscribeConfig) EnableTokenUsageLogging() bool {
	return false
}

func (c runtimeHealthTranscribeConfig) EnableTokenUsageLogToPlugin() bool {
	return false
}

func (c runtimeHealthTranscribeConfig) EnableTokenUsageLogToFile() bool {
	return false
}

func (c runtimeHealthTranscribeConfig) GetTranscriptGenerator() string {
	return c.transcriptGenerator
}

type runtimeHealthSynthesizer struct {
	local bool
}

func (s runtimeHealthSynthesizer) Synthesize(context.Context, string) (*ttsbroker.Audio, error) {
	return nil, nil
}

func (s runtimeHealthSynthesizer) IsLocal() bool {
	return s.local
}

type runtimeHealthFileClient struct{}

func (c runtimeHealthFileClient) GetPost(string) (*model.Post, error) {
	return nil, nil
}

func (c runtimeHealthFileClient) UpdatePost(*model.Post) error {
	return nil
}

func (c runtimeHealthFileClient) UploadFile(io.Reader, string, string) (*model.FileInfo, error) {
	return nil, nil
}

func (c runtimeHealthFileClient) LogError(string, ...interface{}) {
}

type runtimeSessionActionControl struct {
	stopped      string
	resumed      string
	approval     agentruntime.RuntimeApprovalDecision
	expired      bool
	resumeEvents chan agentruntime.RuntimeEvent
}

func (c *runtimeSessionActionControl) SubmitApproval(_ context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	c.approval = decision
	return nil
}

func (c *runtimeSessionActionControl) ExpireApprovals(context.Context) error {
	c.expired = true
	return nil
}

func (c *runtimeSessionActionControl) StopSession(_ context.Context, sessionID string) error {
	c.stopped = sessionID
	return nil
}

func (c *runtimeSessionActionControl) ResumeSession(_ context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error) {
	c.resumed = sessionID
	events := c.resumeEvents
	if events == nil {
		events = make(chan agentruntime.RuntimeEvent)
		close(events)
	}
	return events, nil
}

func (c *runtimeSessionActionControl) GetStatus(context.Context, string) (agentruntime.RuntimeStatus, error) {
	return agentruntime.RuntimeStatus{}, nil
}

type runtimeHealthTestStore struct {
	sessions          []agentruntime.RuntimeSession
	tasks             []agentruntime.Task
	taskRuns          []agentruntime.TaskRun
	approvals         []agentruntime.RuntimeApproval
	supervisors       []agentruntime.SupervisorRun
	runtimePolicies   []agentruntime.RuntimePolicy
	workspacePolicies []agentruntime.WorkspacePolicy
	checklistStates   []agentruntime.HermesOffChecklistItemState
	listErr           error
	deletedTask       string
	upsertedPolicy    *agentruntime.RuntimePolicy
	createdWorkspace  *agentruntime.WorkspacePolicy
	updatedWorkspace  *agentruntime.WorkspacePolicy
}

func (s *runtimeHealthTestStore) CreateRuntimeSession(*agentruntime.RuntimeSession) error {
	return nil
}

func (s *runtimeHealthTestStore) GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error) {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i], nil
		}
	}
	return nil, store.ErrRuntimeSessionNotFound
}

func (s *runtimeHealthTestStore) GetRuntimeSessionByConversationAgent(string, string) (*agentruntime.RuntimeSession, error) {
	return nil, store.ErrRuntimeSessionNotFound
}

func (s *runtimeHealthTestStore) ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := []agentruntime.RuntimeSession{}
	for _, session := range s.sessions {
		if filter.UserID != "" && session.UserID != filter.UserID {
			continue
		}
		if filter.AgentID != "" && session.AgentID != filter.AgentID {
			continue
		}
		if filter.ServerID != "" && session.ServerID != filter.ServerID {
			continue
		}
		if filter.TeamID != "" && session.TeamID != filter.TeamID {
			continue
		}
		if filter.ChannelID != "" && session.ChannelID != filter.ChannelID {
			continue
		}
		if filter.RootPostID != "" && session.RootPostID != filter.RootPostID {
			continue
		}
		if filter.ConversationID != "" && session.MattermostConversationID != filter.ConversationID {
			continue
		}
		if filter.Status != "" && session.Status != filter.Status {
			continue
		}
		out = append(out, session)
	}
	return out, nil
}

func (s *runtimeHealthTestStore) UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Status = status
			s.sessions[i].LastError = lastError
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *runtimeHealthTestStore) GetRuntimePolicy(agentruntime.PolicyScopeType, string) (*agentruntime.RuntimePolicy, error) {
	return nil, store.ErrRuntimePolicyNotFound
}

func (s *runtimeHealthTestStore) UpsertRuntimePolicy(policy *agentruntime.RuntimePolicy) error {
	if policy != nil {
		clone := *policy
		s.upsertedPolicy = &clone
	}
	return nil
}

func (s *runtimeHealthTestStore) ListRuntimePolicies() ([]agentruntime.RuntimePolicy, error) {
	return s.runtimePolicies, nil
}

func (s *runtimeHealthTestStore) CreateWorkspacePolicy(policy *agentruntime.WorkspacePolicy) error {
	if policy != nil {
		clone := *policy
		s.createdWorkspace = &clone
	}
	return nil
}

func (s *runtimeHealthTestStore) GetWorkspacePolicy(string) (*agentruntime.WorkspacePolicy, error) {
	return nil, store.ErrWorkspacePolicyNotFound
}

func (s *runtimeHealthTestStore) UpdateWorkspacePolicy(policy *agentruntime.WorkspacePolicy) error {
	if policy != nil {
		clone := *policy
		s.updatedWorkspace = &clone
	}
	return nil
}

func (s *runtimeHealthTestStore) ListWorkspacePolicies() ([]agentruntime.WorkspacePolicy, error) {
	return s.workspacePolicies, nil
}

func (s *runtimeHealthTestStore) ListRuntimeApprovals(filter store.RuntimeApprovalFilter) ([]agentruntime.RuntimeApproval, error) {
	out := []agentruntime.RuntimeApproval{}
	for _, approval := range s.approvals {
		if filter.RuntimeSessionID != "" && approval.RuntimeSessionID != filter.RuntimeSessionID {
			continue
		}
		if filter.RequestedBy != "" && approval.RequestedBy != filter.RequestedBy {
			continue
		}
		if filter.Status != "" && approval.Status != filter.Status {
			continue
		}
		out = append(out, approval)
	}
	return out, nil
}

func (s *runtimeHealthTestStore) GetRuntimeApproval(id string) (*agentruntime.RuntimeApproval, error) {
	for i := range s.approvals {
		if s.approvals[i].ID == id {
			return &s.approvals[i], nil
		}
	}
	return nil, store.ErrRuntimeApprovalNotFound
}

func (s *runtimeHealthTestStore) ListHermesOffChecklistStates() ([]agentruntime.HermesOffChecklistItemState, error) {
	return s.checklistStates, nil
}

func (s *runtimeHealthTestStore) UpsertHermesOffChecklistState(state *agentruntime.HermesOffChecklistItemState) error {
	for i := range s.checklistStates {
		if s.checklistStates[i].Key == state.Key {
			s.checklistStates[i] = *state
			return nil
		}
	}
	s.checklistStates = append(s.checklistStates, *state)
	return nil
}

func (s *runtimeHealthTestStore) ListTasks(filter store.TaskFilter) ([]agentruntime.Task, error) {
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
	}
	return out, nil
}

func (s *runtimeHealthTestStore) ListTaskRuns(taskID string) ([]agentruntime.TaskRun, error) {
	out := []agentruntime.TaskRun{}
	for _, run := range s.taskRuns {
		if run.TaskID == taskID {
			out = append(out, run)
		}
	}
	return out, nil
}

func (s *runtimeHealthTestStore) GetTask(id string) (*agentruntime.Task, error) {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			return &s.tasks[i], nil
		}
	}
	return nil, store.ErrTaskNotFound
}

func (s *runtimeHealthTestStore) UpdateTaskStatus(id string, status agentruntime.TaskStatus, _ int64, nextRunAt int64) error {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status
			if nextRunAt >= 0 {
				s.tasks[i].NextRunAt = nextRunAt
			}
			return nil
		}
	}
	return store.ErrTaskNotFound
}

func (s *runtimeHealthTestStore) DeleteTask(id string) error {
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.deletedTask = id
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return nil
		}
	}
	return store.ErrTaskNotFound
}

func (s *runtimeHealthTestStore) ListSupervisorRuns(filter store.SupervisorRunFilter) ([]agentruntime.SupervisorRun, error) {
	out := []agentruntime.SupervisorRun{}
	for _, supervisor := range s.supervisors {
		if filter.Status != "" && supervisor.Status != filter.Status {
			continue
		}
		out = append(out, supervisor)
	}
	return out, nil
}

func (s *runtimeHealthTestStore) ListSubagentRuns(string) ([]agentruntime.SubagentRun, error) {
	return nil, nil
}

func TestParseRuntimeLimit(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "default", raw: "", want: 60},
		{name: "custom", raw: "25", want: 25},
		{name: "invalid uses default", raw: "nope", want: 60},
		{name: "zero clamps to max", raw: "0", want: maxRuntimeSessionsPageSize},
		{name: "negative clamps to max", raw: "-1", want: maxRuntimeSessionsPageSize},
		{name: "too high clamps to max", raw: "999", want: maxRuntimeSessionsPageSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseRuntimeLimit(tt.raw))
		})
	}
}

func TestHandleRuntimeHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", Status: agentruntime.SessionStatusRunning, RuntimeType: agentruntime.RuntimeTypeCodex, Metadata: json.RawMessage(`{"usage":{"input_tokens":100,"output_tokens":25,"duration_ms":1200,"cost":0.01}}`)},
			{ID: "session-2", Status: agentruntime.SessionStatusWaitingApproval, RuntimeType: agentruntime.RuntimeTypeLocal, Metadata: json.RawMessage(`{"usage":{"input_tokens":50,"output_tokens":10,"duration_ms":2200}}`)},
			{ID: "session-3", Status: agentruntime.SessionStatusCompleted, RuntimeType: agentruntime.RuntimeTypeLocal},
		},
		tasks: []agentruntime.Task{
			{ID: "task-1", Status: agentruntime.TaskStatusQueued},
			{ID: "task-2", Status: agentruntime.TaskStatusRunning},
			{ID: "task-3", Status: agentruntime.TaskStatusCompleted},
		},
		approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", Status: agentruntime.ApprovalStatusPending},
			{ID: "approval-2", Status: agentruntime.ApprovalStatusAccepted},
		},
		supervisors: []agentruntime.SupervisorRun{
			{ID: "supervisor-1", Status: agentruntime.RunStatusRunning},
			{ID: "supervisor-2", Status: agentruntime.RunStatusCompleted},
		},
		runtimePolicies: []agentruntime.RuntimePolicy{
			{ID: "policy-1", ScopeType: agentruntime.PolicyScopeChannel, ScopeID: "channel-1", RuntimeType: agentruntime.RuntimeTypeCodex, AllowCloud: true},
			{ID: "policy-2", ScopeType: agentruntime.PolicyScopeThread, ScopeID: "root-1", RuntimeType: agentruntime.RuntimeTypeLocal, AllowLocal: true},
		},
		workspacePolicies: []agentruntime.WorkspacePolicy{
			{ID: "workspace-1", Name: "default"},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:               &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:             mmClient,
		runtimeStore:         runtimeStore,
		bots:                 localTranscriptionTestBots(),
		conversationsService: localTextToSpeechConversations(),
	}
	api.SetRuntimeRecoveryStatus(RuntimeRecoveryStatus{StartedAt: 10, CompletedAt: 20})
	router := gin.New()
	router.GET("/admin/runtime/health", api.handleRuntimeHealth)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/health", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response runtimeHealthResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Enabled)
	assert.True(t, response.Healthy)
	assert.True(t, response.HermesOffReady)
	assertRuntimeReadiness(t, response.Readiness, "cloud_runtime_policy", "ok")
	assertRuntimeReadiness(t, response.Readiness, "local_runtime_policy", "ok")
	assertRuntimeReadiness(t, response.Readiness, "channel_thread_routing", "ok")
	assertRuntimeReadiness(t, response.Readiness, "workspace_policy", "ok")
	assertRuntimeReadiness(t, response.Readiness, "local_voice", "ok")
	assertRuntimeReadiness(t, response.Readiness, "restart_recovery", "ok")
	assert.Equal(t, 2, response.ActiveSessions)
	assert.Equal(t, 1, response.SessionsByStatus[string(agentruntime.SessionStatusRunning)])
	assert.Equal(t, 2, response.SessionsByRuntimeType[agentruntime.RuntimeTypeLocal])
	assert.Equal(t, int64(150), response.Usage.InputTokens)
	assert.Equal(t, int64(35), response.Usage.OutputTokens)
	assert.Equal(t, int64(3400), response.Usage.DurationMS)
	assert.Equal(t, 0.01, response.Usage.Cost)
	assert.Equal(t, 2, response.ActiveTasks)
	assert.Equal(t, 1, response.TasksByStatus[agentruntime.TaskStatusQueued])
	assert.Equal(t, 1, response.PendingApprovals)
	assert.Equal(t, 1, response.ActiveSupervisorRuns)
	assert.Equal(t, 1, response.SupervisorsByStatus[agentruntime.RunStatusRunning])
	assert.True(t, response.Voice.TranscriptionConfigured)
	assert.True(t, response.Voice.LocalTranscriptionConfigured)
	assert.True(t, response.Voice.TextToSpeechConfigured)
	assert.True(t, response.Voice.LocalTextToSpeechConfigured)
	assert.True(t, response.Voice.LocalVoiceReady)
	assert.True(t, response.Recovery.Ready)
	assert.Equal(t, int64(10), response.Recovery.StartedAt)
	assert.Equal(t, int64(20), response.Recovery.CompletedAt)
}

func TestRuntimeHealthBlocksHermesOffWhenRestartRecoveryFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		runtimePolicies: []agentruntime.RuntimePolicy{
			{ID: "policy-1", ScopeType: agentruntime.PolicyScopeChannel, ScopeID: "channel-1", RuntimeType: agentruntime.RuntimeTypeCodex, AllowCloud: true},
			{ID: "policy-2", ScopeType: agentruntime.PolicyScopeThread, ScopeID: "root-1", RuntimeType: agentruntime.RuntimeTypeLocal, AllowLocal: true},
		},
		workspacePolicies: []agentruntime.WorkspacePolicy{{ID: "workspace-1", Name: "default"}},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:               &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:             mmClient,
		runtimeStore:         runtimeStore,
		bots:                 localTranscriptionTestBots(),
		conversationsService: localTextToSpeechConversations(),
	}
	api.SetRuntimeRecoveryStatus(RuntimeRecoveryStatus{StartedAt: 10, CompletedAt: 20, TaskRecoveryError: "db unavailable"})
	router := gin.New()
	router.GET("/admin/runtime/health", api.handleRuntimeHealth)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/health", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response runtimeHealthResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.HermesOffReady)
	assert.False(t, response.Recovery.Ready)
	assert.Equal(t, "db unavailable", response.Recovery.TaskRecoveryError)
	assertRuntimeReadiness(t, response.Readiness, "restart_recovery", "missing")
	assertRuntimeReadinessDetail(t, response.Readiness, "restart_recovery", "task recovery failed")
}

func TestHandleAdminListRuntimeSessionsFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", UserID: "user-1", AgentID: "agent-1", ChannelID: "channel-1", RootPostID: "root-1", Status: agentruntime.SessionStatusFailed},
			{ID: "session-2", UserID: "user-2", AgentID: "agent-1", ChannelID: "channel-1", RootPostID: "root-1", Status: agentruntime.SessionStatusFailed},
			{ID: "session-3", UserID: "user-1", AgentID: "agent-2", ChannelID: "channel-2", RootPostID: "root-2", Status: agentruntime.SessionStatusRunning},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/sessions", api.handleAdminListRuntimeSessions)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/sessions?user_id=user-1&agent_id=agent-1&channel_id=channel-1&root_post_id=root-1&status=failed", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var sessions []agentruntime.RuntimeSession
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &sessions))
	require.Len(t, sessions, 1)
	assert.Equal(t, "session-1", sessions[0].ID)
}

func TestHandleAdminListRuntimeSessionsRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", UserID: "user-1", Status: agentruntime.SessionStatusRunning},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/sessions", api.handleAdminListRuntimeSessions)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/sessions", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleAdminListRuntimeApprovalsFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeApprovalControl: &runtimeSessionActionControl{},
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
			{ID: "approval-2", RuntimeSessionID: "session-2", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
			{ID: "approval-3", RuntimeSessionID: "session-1", RequestedBy: "user-2", Status: agentruntime.ApprovalStatusAccepted},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/approvals", api.handleAdminListRuntimeApprovals)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/approvals?runtime_session_id=session-1&requested_by=user-1&status=pending", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var approvals []agentruntime.RuntimeApproval
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &approvals))
	require.Len(t, approvals, 1)
	assert.Equal(t, "approval-1", approvals[0].ID)
}

func TestHandleAdminListRuntimeApprovalsRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeApprovalControl: &runtimeSessionActionControl{},
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/approvals", api.handleAdminListRuntimeApprovals)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/approvals", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestRuntimeReadinessBlocksHermesOffWhenRequiredConfigIsMissing(t *testing.T) {
	voice := runtimeVoiceHealthResponse{
		TranscriptionConfigured:      true,
		LocalTranscriptionConfigured: false,
		TextToSpeechConfigured:       true,
		LocalTextToSpeechConfigured:  false,
		LocalVoiceReady:              false,
	}

	readiness := runtimeReadinessChecks(nil, nil, voice, runtimeRecoveryHealthResponse{})

	assert.False(t, runtimeHermesOffReady(readiness))
	assertRuntimeReadiness(t, readiness, "cloud_runtime_policy", "missing")
	assertRuntimeReadiness(t, readiness, "local_runtime_policy", "missing")
	assertRuntimeReadiness(t, readiness, "channel_thread_routing", "missing")
	assertRuntimeReadiness(t, readiness, "workspace_policy", "missing")
	assertRuntimeReadiness(t, readiness, "voice_discussion", "ok")
	assertRuntimeReadiness(t, readiness, "local_voice", "missing")
	assertRuntimeReadiness(t, readiness, "restart_recovery", "missing")
}

func TestHandleHermesOffChecklist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		runtimePolicies: []agentruntime.RuntimePolicy{
			{ID: "policy-1", ScopeType: agentruntime.PolicyScopeChannel, ScopeID: "channel-1", RuntimeType: agentruntime.RuntimeTypeCodex, AllowCloud: true},
			{ID: "policy-2", ScopeType: agentruntime.PolicyScopeThread, ScopeID: "root-1", RuntimeType: agentruntime.RuntimeTypeLocal, AllowLocal: true},
		},
		workspacePolicies: []agentruntime.WorkspacePolicy{
			{ID: "workspace-1", Name: "default"},
		},
		checklistStates: []agentruntime.HermesOffChecklistItemState{
			{Key: "test_channel_soak", Status: agentruntime.ChecklistStatusOK, Detail: "7 days passed", UpdatedBy: "admin-1", UpdatedAt: 1},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:               &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:             mmClient,
		runtimeStore:         runtimeStore,
		bots:                 localTranscriptionTestBots(),
		conversationsService: localTextToSpeechConversations(),
	}
	api.SetRuntimeRecoveryStatus(RuntimeRecoveryStatus{StartedAt: 10, CompletedAt: 20})
	router := gin.New()
	router.GET("/admin/runtime/hermes-off-checklist", api.handleHermesOffChecklist)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/hermes-off-checklist", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response hermesOffChecklistResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Ready)
	assert.True(t, response.Health.HermesOffReady)
	assertChecklistItem(t, response.Groups, "cloud_runtime_policy", "ok")
	assertChecklistItem(t, response.Groups, "test_channel_soak", "ok")
	assertChecklistEvidence(t, response.Groups, "test_channel_soak", "7 days passed")
	assertChecklistGroup(t, response.Groups, "hermes_capability_migration")
	assertChecklistItem(t, response.Groups, "migration_workspace_files", "manual")
	assertChecklistItem(t, response.Groups, "migration_approval_resume", "manual")
	assertChecklistItem(t, response.Groups, "hermes_adapter_disabled", "manual")
}

func TestHandleHermesOffChecklistRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{},
	}
	router := gin.New()
	router.GET("/admin/runtime/hermes-off-checklist", api.handleHermesOffChecklist)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/hermes-off-checklist", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleUpdateHermesOffChecklistItem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/hermes-off-checklist/:itemKey", api.handleUpdateHermesOffChecklistItem)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/hermes-off-checklist/test_channel_soak", bytes.NewBufferString(`{"status":"ok","detail":"7 days passed"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response agentruntime.HermesOffChecklistItemState
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "test_channel_soak", response.Key)
	assert.Equal(t, agentruntime.ChecklistStatusOK, response.Status)
	assert.Equal(t, "7 days passed", response.Detail)
	assert.Equal(t, "admin-1", response.UpdatedBy)
	require.Len(t, runtimeStore.checklistStates, 1)
}

func TestHandleUpdateHermesOffChecklistItemAcceptsCapabilityMigrationGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/hermes-off-checklist/:itemKey", api.handleUpdateHermesOffChecklistItem)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/hermes-off-checklist/migration_approval_resume", bytes.NewBufferString(`{"status":"ok","detail":"accepted command approval and verified resumed output in thread"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response agentruntime.HermesOffChecklistItemState
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "migration_approval_resume", response.Key)
	assert.Equal(t, agentruntime.ChecklistStatusOK, response.Status)
	assert.Equal(t, "accepted command approval and verified resumed output in thread", response.Detail)
	assert.Equal(t, "admin-1", response.UpdatedBy)
	require.Len(t, runtimeStore.checklistStates, 1)
}

func TestHandleUpdateHermesOffChecklistItemRejectsAutomaticChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{},
	}
	router := gin.New()
	router.PUT("/admin/runtime/hermes-off-checklist/:itemKey", api.handleUpdateHermesOffChecklistItem)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/hermes-off-checklist/cloud_runtime_policy", bytes.NewBufferString(`{"status":"ok"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestHandleUpdateHermesOffChecklistItemRequiresEvidenceForDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{},
	}
	router := gin.New()
	router.PUT("/admin/runtime/hermes-off-checklist/:itemKey", api.handleUpdateHermesOffChecklistItem)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/hermes-off-checklist/test_channel_soak", bytes.NewBufferString(`{"status":"ok","detail":"   "}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestHandleUpdateHermesOffChecklistItemRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/hermes-off-checklist/:itemKey", api.handleUpdateHermesOffChecklistItem)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/hermes-off-checklist/test_channel_soak", bytes.NewBufferString(`{"status":"ok","detail":"7 days passed"}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, runtimeStore.checklistStates)
}

func TestHandleRuntimeSessionActionStopsOwnedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/sessions/:sessionID/action", api.handleRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"stop"}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "session-1", control.stopped)
}

func TestHandleSubmitRuntimeApprovalRejectsForeignApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	api := &API{
		config: &testConfigImpl{enableAgentRuntimeControlPlane: true},
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/approvals/:approvalID/decision", api.handleSubmitRuntimeApproval)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/approval-1/decision", bytes.NewBufferString(`{"decision":"accept"}`))
	req.Header.Set("Mattermost-User-Id", "user-2")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, control.approval.Decision)
}

func TestHandleSubmitRuntimeApprovalAcceptsOwnedApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.ChannelId == "channel-1" &&
			post.RootId == "root-1" &&
			strings.Contains(post.Message, "Runtime approval accepted") &&
			strings.Contains(post.Message, "user-1") &&
			strings.Contains(post.Message, "Command: make test")
	})).Return(nil)
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending, RequestPayload: json.RawMessage(`{"method":"item/commandExecution/requestApproval","params":{"command":"make test"}}`)},
		}, sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", ChannelID: "channel-1", RootPostID: "root-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/approvals/:approvalID/decision", api.handleSubmitRuntimeApproval)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/approvals/approval-1/decision", bytes.NewBufferString(`{"decision":"accept"}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, agentruntime.ApprovalDecisionAccept, control.approval.Decision)
}

func TestHandleAdminSubmitRuntimeApprovalAcceptsAnyApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	mmClient.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.ChannelId == "channel-1" &&
			post.RootId == "root-1" &&
			strings.Contains(post.Message, "Runtime approval denied") &&
			strings.Contains(post.Message, "admin-1") &&
			strings.Contains(post.Message, "write: /workspace/result.md")
	})).Return(nil)
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending, RequestPayload: json.RawMessage(`{"method":"item/fileChange/requestApproval","params":{"action":"write","path":"/workspace/result.md"}}`)},
		}, sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", ChannelID: "channel-1", RootPostID: "root-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/admin/runtime/approvals/:approvalID/decision", api.handleAdminSubmitRuntimeApproval)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/approvals/approval-1/decision", bytes.NewBufferString(`{"decision":"deny","reason":"unsafe"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, agentruntime.ApprovalDecisionDeny, control.approval.Decision)
	assert.Equal(t, "unsafe", control.approval.Reason)
	assert.Equal(t, "admin-1", control.approval.UserID)
}

func TestHandleAdminSubmitRuntimeApprovalRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-2", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{approvals: []agentruntime.RuntimeApproval{
			{ID: "approval-1", RuntimeSessionID: "session-1", RequestedBy: "user-1", Status: agentruntime.ApprovalStatusPending},
		}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/admin/runtime/approvals/:approvalID/decision", api.handleAdminSubmitRuntimeApproval)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/approvals/approval-1/decision", bytes.NewBufferString(`{"decision":"accept"}`))
	req.Header.Set("Mattermost-User-Id", "user-2")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, control.approval.Decision)
}

func TestHandleRuntimeSessionActionResumesOwnedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.ChannelId == "channel-1" &&
			post.RootId == "root-1" &&
			post.Message == "Runtime session resumed."
	})).Return(nil)
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex, ChannelID: "channel-1", RootPostID: "root-1"}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/sessions/:sessionID/action", api.handleRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"resume"}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "session-1", control.resumed)
}

func TestHandleRuntimeSessionActionPostsResumeCompletionEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "tests passed"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)
	control := &runtimeSessionActionControl{resumeEvents: events}
	posted := make(chan string, 2)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		return post.ChannelId == "channel-1" && post.RootId == "root-1"
	})).Run(func(args mock.Arguments) {
		posted <- args.Get(0).(*model.Post).Message
	}).Return(nil).Twice()
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex, ChannelID: "channel-1", RootPostID: "root-1"}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/sessions/:sessionID/action", api.handleRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"resume"}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, "Runtime session resumed.", <-posted)
	require.Eventually(t, func() bool {
		select {
		case message := <-posted:
			return strings.Contains(message, "Runtime session completed.") && strings.Contains(message, "tests passed")
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestHandleRuntimeSessionActionRejectsForeignSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/runtime/sessions/:sessionID/action", api.handleRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"stop"}`))
	req.Header.Set("Mattermost-User-Id", "user-2")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, control.stopped)
}

func TestHandleAdminRuntimeSessionActionStopsAnySession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/admin/runtime/sessions/:sessionID/action", api.handleAdminRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"stop"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "session-1", control.stopped)
}

func TestHandleAdminRuntimeSessionActionRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control := &runtimeSessionActionControl{}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-2", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:                 &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:               mmClient,
		runtimeStore:           &runtimeHealthTestStore{sessions: []agentruntime.RuntimeSession{{ID: "session-1", UserID: "user-1", RuntimeType: agentruntime.RuntimeTypeCodex}}},
		runtimeApprovalControl: control,
	}
	router := gin.New()
	router.POST("/admin/runtime/sessions/:sessionID/action", api.handleAdminRuntimeSessionAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/sessions/session-1/action", bytes.NewBufferString(`{"action":"stop"}`))
	req.Header.Set("Mattermost-User-Id", "user-2")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, control.stopped)
}

func assertChecklistItem(t *testing.T, groups []hermesOffChecklistGroup, key string, status string) {
	t.Helper()
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Key == key {
				assert.Equal(t, status, item.Status)
				return
			}
		}
	}
	require.Failf(t, "checklist item not found", "missing checklist item %q", key)
}

func assertChecklistEvidence(t *testing.T, groups []hermesOffChecklistGroup, key string, evidence string) {
	t.Helper()
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Key == key {
				assert.Equal(t, evidence, item.Evidence)
				return
			}
		}
	}
	require.Failf(t, "checklist item not found", "missing checklist item %q", key)
}

func assertChecklistGroup(t *testing.T, groups []hermesOffChecklistGroup, key string) {
	t.Helper()
	for _, group := range groups {
		if group.Key == key {
			return
		}
	}
	require.Failf(t, "checklist group not found", "missing checklist group %q", key)
}

func assertRuntimeReadiness(t *testing.T, readiness []runtimeReadinessCheckResponse, key string, status string) {
	t.Helper()
	for _, check := range readiness {
		if check.Key == key {
			assert.Equal(t, status, check.Status)
			return
		}
	}
	require.Failf(t, "readiness check not found", "missing readiness check %q", key)
}

func assertRuntimeReadinessDetail(t *testing.T, readiness []runtimeReadinessCheckResponse, key string, detail string) {
	t.Helper()
	for _, check := range readiness {
		if check.Key == key {
			assert.Equal(t, detail, check.Detail)
			return
		}
	}
	require.Failf(t, "readiness check not found", "missing readiness check %q", key)
}

func TestRuntimeVoiceHealthRejectsCloudTTSForLocalReady(t *testing.T) {
	api := &API{
		bots:                 localTranscriptionTestBots(),
		conversationsService: cloudTextToSpeechConversations(),
	}

	health := api.runtimeVoiceHealth()

	assert.True(t, health.TranscriptionConfigured)
	assert.True(t, health.LocalTranscriptionConfigured)
	assert.True(t, health.TextToSpeechConfigured)
	assert.False(t, health.LocalTextToSpeechConfigured)
	assert.False(t, health.LocalVoiceReady)
}

func localTranscriptionTestBots() *bots.MMBots {
	mmBots := bots.New(nil, nil, enterprise.NewLicenseChecker(nil), runtimeHealthTranscribeConfig{transcriptGenerator: "transcriber"}, nil, &http.Client{}, nil)
	mmBots.SetBotsForTesting([]*bots.Bot{
		bots.NewBot(
			llm.BotConfig{Name: "transcriber"},
			llm.ServiceConfig{Type: llm.ServiceTypeOpenAICompatible, APIURL: "http://whisper.local:8080/v1"},
			&model.Bot{UserId: "bot-user-id", Username: "transcriber"},
			nil,
		),
	})
	return mmBots
}

func localTextToSpeechConversations() *conversations.Conversations {
	conversationService := &conversations.Conversations{}
	conversationService.SetTextToSpeech(ttsbroker.New(ttsbroker.Options{
		Files:       runtimeHealthFileClient{},
		Synthesizer: runtimeHealthSynthesizer{local: true},
	}))
	return conversationService
}

func cloudTextToSpeechConversations() *conversations.Conversations {
	conversationService := &conversations.Conversations{}
	conversationService.SetTextToSpeech(ttsbroker.New(ttsbroker.Options{
		Files:       runtimeHealthFileClient{},
		Synthesizer: runtimeHealthSynthesizer{local: false},
	}))
	return conversationService
}

func TestHandleRuntimeHealthReportsStoreErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{listErr: errors.New("database unavailable")},
	}
	router := gin.New()
	router.GET("/admin/runtime/health", api.handleRuntimeHealth)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/health", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestHandleRuntimeHealthRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{},
	}
	router := gin.New()
	router.GET("/admin/runtime/health", api.handleRuntimeHealth)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/health", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleAdminListRuntimeTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", RootPostID: "root-1", Status: agentruntime.TaskStatusQueued},
			{ID: "task-2", UserID: "user-2", ChannelID: "channel-1", RootPostID: "root-2", Status: agentruntime.TaskStatusQueued},
			{ID: "task-3", UserID: "user-3", ChannelID: "channel-2", RootPostID: "root-1", Status: agentruntime.TaskStatusCompleted},
		},
		taskRuns: []agentruntime.TaskRun{
			{ID: "run-1", TaskID: "task-2", Status: agentruntime.TaskStatusFailed, Error: "boom"},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.GET("/admin/runtime/tasks", api.handleAdminListRuntimeTasks)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/tasks?channel_id=channel-1&status=queued&root_post_id=root-2", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response []runtimeTaskResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response, 1)
	assert.Equal(t, "task-2", response[0].ID)
	require.NotNil(t, response[0].LastRun)
	assert.Equal(t, "run-1", response[0].LastRun.ID)
	assert.Equal(t, "boom", response[0].LastRun.Error)
}

func TestHandleAdminListRuntimeTasksRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", Status: agentruntime.TaskStatusQueued},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/tasks", api.handleAdminListRuntimeTasks)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/tasks", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleAdminListRuntimeTaskRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", Status: agentruntime.TaskStatusQueued},
		},
		taskRuns: []agentruntime.TaskRun{
			{ID: "run-2", TaskID: "task-1", Status: agentruntime.TaskStatusCompleted, StartedAt: 2},
			{ID: "run-1", TaskID: "task-1", Status: agentruntime.TaskStatusFailed, StartedAt: 1, Error: "boom"},
			{ID: "other-run", TaskID: "task-2", Status: agentruntime.TaskStatusCompleted},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.GET("/admin/runtime/tasks/:taskID/runs", api.handleAdminListRuntimeTaskRuns)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/tasks/task-1/runs", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response []agentruntime.TaskRun
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response, 2)
	assert.Equal(t, "run-2", response[0].ID)
	assert.Equal(t, "task-1", response[0].TaskID)
	assert.Equal(t, agentruntime.TaskStatusCompleted, response[0].Status)
}

func TestHandleAdminListRuntimeTaskRunsMissingTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: &runtimeHealthTestStore{},
	}
	router := gin.New()
	router.GET("/admin/runtime/tasks/:taskID/runs", api.handleAdminListRuntimeTaskRuns)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/tasks/task-1/runs", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestHandleAdminListRuntimeTaskRunsRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{
			tasks:    []agentruntime.Task{{ID: "task-1", UserID: "user-1", Status: agentruntime.TaskStatusQueued}},
			taskRuns: []agentruntime.TaskRun{{ID: "run-1", TaskID: "task-1"}},
		},
	}
	router := gin.New()
	router.GET("/admin/runtime/tasks/:taskID/runs", api.handleAdminListRuntimeTaskRuns)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/tasks/task-1/runs", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleAdminRuntimeTaskActionUpdatesTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", RootPostID: "root-1", AgentID: "agent-1", TaskType: agentruntime.TaskTypeRecurring, Status: agentruntime.TaskStatusRunning, NextRunAt: 1000},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/tasks/:taskID/action", api.handleAdminRuntimeTaskAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/tasks/task-1/action", bytes.NewBufferString(`{"action":"pause"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response agentruntime.Task
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, agentruntime.TaskStatusPaused, response.Status)
	assert.Equal(t, agentruntime.TaskStatusPaused, runtimeStore.tasks[0].Status)
	assert.Equal(t, int64(1000), runtimeStore.tasks[0].NextRunAt)
}

func TestHandleAdminRuntimeTaskActionSnoozesTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", Status: agentruntime.TaskStatusPaused},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/tasks/:taskID/action", api.handleAdminRuntimeTaskAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/tasks/task-1/action", bytes.NewBufferString(`{"action":"snooze","nextRunAt":9999999999999}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, agentruntime.TaskStatusQueued, runtimeStore.tasks[0].Status)
	assert.Equal(t, int64(9999999999999), runtimeStore.tasks[0].NextRunAt)
}

func TestHandleAdminRuntimeTaskActionDeletesTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", Status: agentruntime.TaskStatusPaused},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/tasks/:taskID/action", api.handleAdminRuntimeTaskAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/tasks/task-1/action", bytes.NewBufferString(`{"action":"delete"}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "task-1", runtimeStore.deletedTask)
	assert.Empty(t, runtimeStore.tasks)
}

func TestHandleAdminRuntimeTaskActionRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtimeStore := &runtimeHealthTestStore{
		tasks: []agentruntime.Task{
			{ID: "task-1", UserID: "user-1", ChannelID: "channel-1", Status: agentruntime.TaskStatusRunning},
		},
	}
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-2", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/tasks/:taskID/action", api.handleAdminRuntimeTaskAction)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/tasks/task-1/action", bytes.NewBufferString(`{"action":"pause"}`))
	req.Header.Set("Mattermost-User-Id", "user-2")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Equal(t, agentruntime.TaskStatusRunning, runtimeStore.tasks[0].Status)
}

func TestRuntimeTaskActionTargetValidatesActions(t *testing.T) {
	task := &agentruntime.Task{Status: agentruntime.TaskStatusPaused, NextRunAt: 1000}

	status, nextRunAt, deleteTask, err := runtimeTaskActionTarget(runtimeTaskActionRequest{Action: "resume"}, task, 2000)
	require.NoError(t, err)
	assert.Equal(t, agentruntime.TaskStatusQueued, status)
	assert.Equal(t, int64(62000), nextRunAt)
	assert.False(t, deleteTask)

	_, _, _, err = runtimeTaskActionTarget(runtimeTaskActionRequest{Action: "snooze"}, task, 2000)
	assert.Error(t, err)

	_, _, deleteTask, err = runtimeTaskActionTarget(runtimeTaskActionRequest{Action: "delete"}, task, 2000)
	require.NoError(t, err)
	assert.True(t, deleteTask)

	_, _, _, err = runtimeTaskActionTarget(runtimeTaskActionRequest{Action: "bogus"}, task, 2000)
	assert.Error(t, err)
}

func TestRuntimeValidationHelpers(t *testing.T) {
	for _, scopeType := range []agentruntime.PolicyScopeType{
		agentruntime.PolicyScopeServer,
		agentruntime.PolicyScopeTeam,
		agentruntime.PolicyScopeChannel,
		agentruntime.PolicyScopeThread,
		agentruntime.PolicyScopeUser,
		agentruntime.PolicyScopeAgent,
	} {
		assert.True(t, validRuntimePolicyScope(scopeType))
	}
	assert.False(t, validRuntimePolicyScope("bogus"))

	for _, runtimeType := range []agentruntime.RuntimeType{
		agentruntime.RuntimeTypeInherit,
		agentruntime.RuntimeTypeCodex,
		agentruntime.RuntimeTypeOpenAI,
		agentruntime.RuntimeTypeLocal,
	} {
		assert.True(t, validRuntimeType(runtimeType))
	}
	assert.False(t, validRuntimeType("bogus"))

	assert.True(t, validRuntimeApprovalDecision(agentruntime.ApprovalDecisionAccept))
	assert.True(t, validRuntimeApprovalDecision(agentruntime.ApprovalDecisionDeny))
	assert.False(t, validRuntimeApprovalDecision("maybe"))
}

func TestHandleListRuntimePoliciesRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{runtimePolicies: []agentruntime.RuntimePolicy{
			{ID: "policy-1", RuntimeType: agentruntime.RuntimeTypeLocal},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/policies", api.handleListRuntimePolicies)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/policies", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleListRuntimePoliciesAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{runtimePolicies: []agentruntime.RuntimePolicy{
			{ID: "policy-1", RuntimeType: agentruntime.RuntimeTypeLocal},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/policies", api.handleListRuntimePolicies)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/policies", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var policies []agentruntime.RuntimePolicy
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &policies))
	require.Len(t, policies, 1)
	assert.Equal(t, "policy-1", policies[0].ID)
}

func TestHandleListWorkspacePoliciesRequiresAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{workspacePolicies: []agentruntime.WorkspacePolicy{
			{ID: "workspace-1", Name: "Project"},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/workspace-policies", api.handleListWorkspacePolicies)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/workspace-policies", nil)
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHandleListWorkspacePoliciesAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	api := &API{
		config:   &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient: mmClient,
		runtimeStore: &runtimeHealthTestStore{workspacePolicies: []agentruntime.WorkspacePolicy{
			{ID: "workspace-1", Name: "Project"},
		}},
	}
	router := gin.New()
	router.GET("/admin/runtime/workspace-policies", api.handleListWorkspacePolicies)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/runtime/workspace-policies", nil)
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var policies []agentruntime.WorkspacePolicy
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &policies))
	require.Len(t, policies, 1)
	assert.Equal(t, "workspace-1", policies[0].ID)
}

func TestHandleUpsertRuntimePolicyRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/policies/:scopeType/:scopeID", api.handleUpsertRuntimePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/policies/channel/channel-1", bytes.NewBufferString(`{"runtimeType":"local","allowLocal":true}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Nil(t, runtimeStore.upsertedPolicy)
}

func TestHandleUpsertRuntimePolicyAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/policies/:scopeType/:scopeID", api.handleUpsertRuntimePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/policies/channel/channel-1", bytes.NewBufferString(`{"runtimeType":"local","allowLocal":true}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, runtimeStore.upsertedPolicy)
	assert.Equal(t, agentruntime.PolicyScopeChannel, runtimeStore.upsertedPolicy.ScopeType)
	assert.Equal(t, "channel-1", runtimeStore.upsertedPolicy.ScopeID)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, runtimeStore.upsertedPolicy.RuntimeType)
	assert.Equal(t, "admin-1", runtimeStore.upsertedPolicy.UpdatedBy)
}

func TestHandleCreateWorkspacePolicyRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/workspace-policies", api.handleCreateWorkspacePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/workspace-policies", bytes.NewBufferString(`{"name":"Project","allowedRoots":["/workspace"]}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Nil(t, runtimeStore.createdWorkspace)
}

func TestHandleCreateWorkspacePolicyAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.POST("/admin/runtime/workspace-policies", api.handleCreateWorkspacePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/runtime/workspace-policies", bytes.NewBufferString(`{"name":"Project","allowedRoots":["/workspace"]}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.NotNil(t, runtimeStore.createdWorkspace)
	assert.Equal(t, "Project", runtimeStore.createdWorkspace.Name)
	assert.JSONEq(t, `["/workspace"]`, string(runtimeStore.createdWorkspace.AllowedRoots))
}

func TestHandleUpdateWorkspacePolicyRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "user-1", model.PermissionManageSystem).Return(false).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/workspace-policies/:policyID", api.handleUpdateWorkspacePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/workspace-policies/workspace-1", bytes.NewBufferString(`{"name":"Project","allowedRoots":["/workspace"]}`))
	req.Header.Set("Mattermost-User-Id", "user-1")
	router.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Nil(t, runtimeStore.updatedWorkspace)
}

func TestHandleUpdateWorkspacePolicyAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mmClient := mmapimocks.NewMockClient(t)
	mmClient.On("HasPermissionTo", "admin-1", model.PermissionManageSystem).Return(true).Once()
	runtimeStore := &runtimeHealthTestStore{}
	api := &API{
		config:       &testConfigImpl{enableAgentRuntimeControlPlane: true},
		mmClient:     mmClient,
		runtimeStore: runtimeStore,
	}
	router := gin.New()
	router.PUT("/admin/runtime/workspace-policies/:policyID", api.handleUpdateWorkspacePolicy)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/runtime/workspace-policies/workspace-1", bytes.NewBufferString(`{"name":"Project","allowedRoots":["/workspace"]}`))
	req.Header.Set("Mattermost-User-Id", "admin-1")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, runtimeStore.updatedWorkspace)
	assert.Equal(t, "workspace-1", runtimeStore.updatedWorkspace.ID)
	assert.Equal(t, "Project", runtimeStore.updatedWorkspace.Name)
	assert.JSONEq(t, `["/workspace"]`, string(runtimeStore.updatedWorkspace.AllowedRoots))
}

func TestRuntimePolicyFromRequestIncludesCloudBudget(t *testing.T) {
	policy := runtimePolicyFromRequest(runtimePolicyRequest{
		RuntimeType:       agentruntime.RuntimeTypeCodex,
		ProviderID:        "codex",
		AllowCloud:        true,
		CloudBudgetCents:  1234,
		CloudBudgetWindow: "weekly",
	}, agentruntime.PolicyScopeChannel, "channel-1", "admin-1")

	assert.Equal(t, int64(1234), policy.CloudBudgetCents)
	assert.Equal(t, "weekly", policy.CloudBudgetWindow)
	assert.Equal(t, "admin-1", policy.CreatedBy)
	assert.Equal(t, "admin-1", policy.UpdatedBy)
}

func TestValidateCloudEscalationConfirmation(t *testing.T) {
	for _, req := range []runtimePolicyRequest{
		{RuntimeType: agentruntime.RuntimeTypeCodex, AllowCloud: true},
		{RuntimeType: agentruntime.RuntimeTypeOpenAI, AllowCloud: true},
		{RuntimeType: agentruntime.RuntimeTypeInherit, AllowCloud: true},
	} {
		assert.Error(t, validateCloudEscalationConfirmation(req))
		req.CloudEscalationConfirmed = true
		assert.NoError(t, validateCloudEscalationConfirmation(req))
	}

	assert.NoError(t, validateCloudEscalationConfirmation(runtimePolicyRequest{
		RuntimeType: agentruntime.RuntimeTypeLocal,
		AllowLocal:  true,
	}))
}

func TestWorkspacePolicyFromRequestDefaultsAndValidates(t *testing.T) {
	policy, err := workspacePolicyFromRequest(workspacePolicyRequest{
		Name:                 "Project",
		DefaultWorkspacePath: "/workspace/project",
		AllowedRoots:         []string{"/workspace"},
	})
	assert.NoError(t, err)
	assert.Equal(t, agentruntime.WorkspaceModeAskWrite, policy.Mode)
	assert.Equal(t, agentruntime.WorkspaceNetworkModeAsk, policy.NetworkMode)
	assert.Equal(t, agentruntime.WorkspaceShellModeAsk, policy.ShellMode)
	assert.JSONEq(t, `["/workspace"]`, string(policy.AllowedRoots))
	assert.JSONEq(t, `{}`, string(policy.Metadata))

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name: "Project",
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name:         "Project",
		AllowedRoots: []string{"workspace"},
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name:                 "Project",
		AllowedRoots:         []string{"/workspace"},
		DefaultWorkspacePath: "workspace/project",
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name:                 "Project",
		AllowedRoots:         []string{"/workspace/allowed"},
		DefaultWorkspacePath: "/workspace/blocked",
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name: "Project",
		Mode: "bogus",
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name:        "Project",
		NetworkMode: "bogus",
	})
	assert.Error(t, err)

	_, err = workspacePolicyFromRequest(workspacePolicyRequest{
		Name:      "Project",
		ShellMode: "bogus",
	})
	assert.Error(t, err)
}

func TestFilterTasksByRootPostID(t *testing.T) {
	tasks := []agentruntime.Task{
		{ID: "task-1", RootPostID: "root-1"},
		{ID: "task-2", RootPostID: "root-2"},
	}

	filtered := filterTasksByRootPostID(tasks, "root-1")
	assert.Equal(t, []agentruntime.Task{{ID: "task-1", RootPostID: "root-1"}}, filtered)
	assert.Equal(t, tasks, filterTasksByRootPostID(tasks, ""))
}
