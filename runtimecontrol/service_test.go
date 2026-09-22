// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	enabled bool
	rates   []agentruntime.RuntimeCostRate
}

func (c testConfig) EnableAgentRuntimeControlPlane() bool {
	return c.enabled
}

func (c testConfig) RuntimeCostRates() []agentruntime.RuntimeCostRate {
	return c.rates
}

type testStore struct {
	mu                sync.Mutex
	sessions          []agentruntime.RuntimeSession
	policies          []agentruntime.RuntimePolicy
	workspacePolicies []agentruntime.WorkspacePolicy
	approvals         []agentruntime.RuntimeApproval
	supervisorRuns    []agentruntime.SupervisorRun
	subagentRuns      []agentruntime.SubagentRun
	statusUpdates     []agentruntime.RuntimeSessionStatus
	metadataUpdates   []json.RawMessage
	externalUpdates   []string
}

func (s *testStore) CreateRuntimeSession(session *agentruntime.RuntimeSession) error {
	if session.ID == "" {
		session.ID = "runtime-session-1"
	}
	s.sessions = append(s.sessions, *session)
	return nil
}

func (s *testStore) GetRuntimeSessionByConversationAgent(conversationID, agentID string) (*agentruntime.RuntimeSession, error) {
	for i := range s.sessions {
		if s.sessions[i].MattermostConversationID == conversationID && s.sessions[i].AgentID == agentID {
			return &s.sessions[i], nil
		}
	}
	return nil, store.ErrRuntimeSessionNotFound
}

func (s *testStore) GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error) {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i], nil
		}
	}
	return nil, store.ErrRuntimeSessionNotFound
}

func (s *testStore) ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	var sessions []agentruntime.RuntimeSession
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
		if filter.AgentID != "" && session.AgentID != filter.AgentID {
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
		if filter.CreatedAtFrom > 0 && session.CreatedAt < filter.CreatedAtFrom {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s *testStore) ListRuntimePolicies() ([]agentruntime.RuntimePolicy, error) {
	return s.policies, nil
}

func (s *testStore) GetWorkspacePolicy(id string) (*agentruntime.WorkspacePolicy, error) {
	for i := range s.workspacePolicies {
		if s.workspacePolicies[i].ID == id {
			return &s.workspacePolicies[i], nil
		}
	}
	return nil, store.ErrWorkspacePolicyNotFound
}

func (s *testStore) UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Status = status
			s.sessions[i].LastError = lastError
			s.statusUpdates = append(s.statusUpdates, status)
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *testStore) UpdateRuntimeSessionMetadata(id string, metadata json.RawMessage) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].Metadata = metadata
			s.metadataUpdates = append(s.metadataUpdates, metadata)
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *testStore) UpdateRuntimeSessionExternalSessionID(id string, externalSessionID string) error {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].ExternalSessionID = externalSessionID
			s.externalUpdates = append(s.externalUpdates, externalSessionID)
			return nil
		}
	}
	return store.ErrRuntimeSessionNotFound
}

func (s *testStore) CreateRuntimeApproval(approval *agentruntime.RuntimeApproval) error {
	if approval.ID == "" {
		approval.ID = "approval-1"
	}
	s.approvals = append(s.approvals, *approval)
	return nil
}

func (s *testStore) GetRuntimeApproval(id string) (*agentruntime.RuntimeApproval, error) {
	for i := range s.approvals {
		if s.approvals[i].ID == id {
			return &s.approvals[i], nil
		}
	}
	return nil, store.ErrRuntimeApprovalNotFound
}

func (s *testStore) UpdateRuntimeApprovalDecision(id string, decision agentruntime.RuntimeApprovalDecision, status agentruntime.RuntimeApprovalStatus) error {
	for i := range s.approvals {
		if s.approvals[i].ID == id {
			s.approvals[i].Status = status
			s.approvals[i].Decision = decision.Decision
			s.approvals[i].DecisionScope = decision.Scope
			s.approvals[i].DecisionReason = decision.Reason
			s.approvals[i].DecidedBy = decision.UserID
			return nil
		}
	}
	return store.ErrRuntimeApprovalNotFound
}

func (s *testStore) ExpireRuntimeApprovals(now int64) (int64, error) {
	var expired int64
	for i := range s.approvals {
		if s.approvals[i].Status == agentruntime.ApprovalStatusPending && s.approvals[i].ExpiresAt > 0 && s.approvals[i].ExpiresAt <= now {
			s.approvals[i].Status = agentruntime.ApprovalStatusExpired
			s.approvals[i].UpdatedAt = now
			expired++
		}
	}
	return expired, nil
}

func (s *testStore) CreateSupervisorRun(run *agentruntime.SupervisorRun) error {
	if run.ID == "" {
		run.ID = "supervisor-1"
	}
	s.supervisorRuns = append(s.supervisorRuns, *run)
	return nil
}

func (s *testStore) ListSupervisorRuns(filter store.SupervisorRunFilter) ([]agentruntime.SupervisorRun, error) {
	var runs []agentruntime.SupervisorRun
	for _, run := range s.supervisorRuns {
		if filter.RuntimeSessionID != "" && run.RuntimeSessionID != filter.RuntimeSessionID {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (s *testStore) CreateSubagentRun(run *agentruntime.SubagentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.ID == "" {
		run.ID = fmt.Sprintf("subagent-%d", len(s.subagentRuns)+1)
	}
	s.subagentRuns = append(s.subagentRuns, *run)
	return nil
}

func (s *testStore) ListSubagentRuns(supervisorRunID string) ([]agentruntime.SubagentRun, error) {
	var runs []agentruntime.SubagentRun
	for _, run := range s.subagentRuns {
		if run.SupervisorRunID == supervisorRunID {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (s *testStore) UpdateSupervisorRunStatus(id, status, finalResultPostID string) error {
	for i := range s.supervisorRuns {
		if s.supervisorRuns[i].ID == id {
			s.supervisorRuns[i].Status = status
			s.supervisorRuns[i].FinalResultPostID = finalResultPostID
			return nil
		}
	}
	return store.ErrSupervisorRunNotFound
}

func (s *testStore) UpdateSubagentRunResult(id, status string, result json.RawMessage, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subagentRuns {
		if s.subagentRuns[i].ID == id {
			s.subagentRuns[i].Status = status
			s.subagentRuns[i].Result = result
			s.subagentRuns[i].Summary = summary
			return nil
		}
	}
	return store.ErrSubagentRunNotFound
}

type testRuntime struct {
	mu              sync.Mutex
	events          chan agentruntime.RuntimeEvent
	resumeEvents    chan agentruntime.RuntimeEvent
	startErr        error
	req             agentruntime.RuntimeTurnRequest
	subagentReqs    []agentruntime.SubagentRequest
	approval        agentruntime.RuntimeApprovalDecision
	stopped         string
	stoppedIDs      []string
	resumed         string
	statusSession   string
	subagentStarted bool
}

func TestChannelSessionKeepsContextAndUsesCurrentAuthor(t *testing.T) {
	store := &testStore{}
	events := make(chan agentruntime.RuntimeEvent)
	close(events)
	runtime := &testRuntime{events: events}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true}, Store: store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{agentruntime.RuntimeTypeCodex: runtime},
	})
	req := StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "channel:one", ChannelID: "one", UserID: "alice", AgentID: "bot",
		},
		Prompt: "@alice: first", InitialContext: "older messages",
	}
	first, err := svc.StartTurn(context.Background(), req)
	require.NoError(t, err)
	for range first.Events {
	}
	require.True(t, first.Created)
	require.Equal(t, "older messages\n\n@alice: first", runtime.req.Prompt)
	require.Equal(t, "alice", runtime.req.Session.UserID)

	req.SessionRequest.UserID = "bob"
	req.Prompt = "@bob: second"
	second, err := svc.StartTurn(context.Background(), req)
	require.NoError(t, err)
	for range second.Events {
	}
	require.False(t, second.Created)
	require.Equal(t, first.Session.ID, second.Session.ID)
	require.Equal(t, "@bob: second", runtime.req.Prompt)
	require.Equal(t, "bob", runtime.req.Session.UserID)
}

func (r *testRuntime) StartTurn(_ context.Context, req agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	r.req = req
	if r.startErr != nil {
		return nil, r.startErr
	}
	return r.events, nil
}

func (r *testRuntime) StartSubagent(_ context.Context, req agentruntime.SubagentRequest) (<-chan agentruntime.RuntimeEvent, error) {
	r.mu.Lock()
	r.subagentStarted = true
	r.subagentReqs = append(r.subagentReqs, req)
	r.mu.Unlock()
	if r.events != nil {
		return r.events, nil
	}
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: req.Session.ID, Text: req.Role + " done"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: req.Session.ID}
	close(events)
	return events, nil
}

func (r *testRuntime) StopTurn(_ context.Context, sessionID string) error {
	r.stopped = sessionID
	r.stoppedIDs = append(r.stoppedIDs, sessionID)
	return nil
}

func (r *testRuntime) ResumeSession(_ context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error) {
	r.resumed = sessionID
	return r.resumeEvents, nil
}

func (r *testRuntime) GetStatus(_ context.Context, sessionID string) (agentruntime.RuntimeStatus, error) {
	r.statusSession = sessionID
	return agentruntime.RuntimeStatus{SessionID: sessionID, Status: agentruntime.SessionStatusRunning}, nil
}

func (r *testRuntime) ListSessions(context.Context, agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	return nil, nil
}

func (r *testRuntime) SubmitApproval(_ context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	r.approval = decision
	return nil
}

type approvalContinuationRuntime struct {
	mu       sync.Mutex
	approved chan struct{}
	once     sync.Once
	approval agentruntime.RuntimeApprovalDecision
}

func newApprovalContinuationRuntime() *approvalContinuationRuntime {
	return &approvalContinuationRuntime{
		approved: make(chan struct{}),
	}
}

func (r *approvalContinuationRuntime) StartTurn(ctx context.Context, req agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	events := make(chan agentruntime.RuntimeEvent)
	go func() {
		defer close(events)
		select {
		case events <- agentruntime.RuntimeEvent{
			Type:      agentruntime.EventTypeApprovalRequested,
			SessionID: req.Session.ID,
			Payload:   json.RawMessage(`{"approval_id":"provider-approval-1","command":"make test"}`),
		}:
		case <-ctx.Done():
			return
		}
		select {
		case <-r.approved:
		case <-ctx.Done():
			return
		}
		select {
		case events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: req.Session.ID, Text: "approved output"}:
		case <-ctx.Done():
			return
		}
		select {
		case events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: req.Session.ID}:
		case <-ctx.Done():
		}
	}()
	return events, nil
}

func (r *approvalContinuationRuntime) StartSubagent(context.Context, agentruntime.SubagentRequest) (<-chan agentruntime.RuntimeEvent, error) {
	return nil, errors.New("unexpected subagent start")
}

func (r *approvalContinuationRuntime) StopTurn(context.Context, string) error {
	return nil
}

func (r *approvalContinuationRuntime) ResumeSession(context.Context, string) (<-chan agentruntime.RuntimeEvent, error) {
	return nil, errors.New("unexpected resume")
}

func (r *approvalContinuationRuntime) GetStatus(context.Context, string) (agentruntime.RuntimeStatus, error) {
	return agentruntime.RuntimeStatus{}, nil
}

func (r *approvalContinuationRuntime) ListSessions(context.Context, agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	return nil, nil
}

func (r *approvalContinuationRuntime) SubmitApproval(_ context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	r.mu.Lock()
	r.approval = decision
	r.mu.Unlock()
	r.once.Do(func() {
		close(r.approved)
	})
	return nil
}

func (r *approvalContinuationRuntime) submittedApproval() agentruntime.RuntimeApprovalDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.approval
}

func receiveRuntimeControlEvent(t *testing.T, events <-chan agentruntime.RuntimeEvent) agentruntime.RuntimeEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		require.True(t, ok, "runtime event stream closed before expected event")
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime event")
	}
	return agentruntime.RuntimeEvent{}
}

func TestEnsureSessionCreatesRuntimeSessionFromEffectivePolicy(t *testing.T) {
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:          "channel-local",
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				ProviderID:  "ollama",
				Model:       "gpt-oss:20b",
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeCodex,
			ProviderID:  "codex",
			Model:       "gpt-5-codex",
			AllowCloud:  true,
			AllowLocal:  true,
		},
	})

	session, effective, created, err := svc.EnsureSession(EnsureSessionRequest{
		MattermostConversationID: "conversation-1",
		ChannelID:                "channel-1",
		RootPostID:               "root-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
		WorkspacePath:            "/workspace/project",
	})
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.True(t, created)
	assert.Equal(t, agentruntime.PolicyScopeChannel, effective.Source)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, session.RuntimeType)
	assert.Equal(t, "ollama", session.ProviderID)
	assert.Equal(t, "/workspace/project", session.WorkspacePath)
	require.Len(t, store.sessions, 1)
}

func TestEnsureSessionReusesExistingSession(t *testing.T) {
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:                       "existing-session",
				MattermostConversationID: "conversation-1",
				AgentID:                  "agent-1",
				RuntimeType:              agentruntime.RuntimeTypeCodex,
				ProviderID:               "codex",
				Model:                    "gpt-5-codex",
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
	})

	session, _, created, err := svc.EnsureSession(EnsureSessionRequest{
		MattermostConversationID: "conversation-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, "existing-session", session.ID)
	require.Len(t, store.sessions, 1)
}

func TestEnsureSessionAppliesWorkspacePolicyDefault(t *testing.T) {
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:                "channel-local",
				ScopeType:         agentruntime.PolicyScopeChannel,
				ScopeID:           "channel-1",
				RuntimeType:       agentruntime.RuntimeTypeLocal,
				ProviderID:        "ollama",
				WorkspacePolicyID: "workspace-policy-1",
				AllowLocal:        true,
			},
		},
		workspacePolicies: []agentruntime.WorkspacePolicy{
			{
				ID:                   "workspace-policy-1",
				DefaultWorkspacePath: "/workspace/project",
				AllowedRoots:         []byte(`["/workspace"]`),
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
	})

	session, _, created, err := svc.EnsureSession(EnsureSessionRequest{
		MattermostConversationID: "conversation-1",
		ChannelID:                "channel-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "/workspace/project", session.WorkspacePath)
}

func TestEnsureSessionBlocksWorkspaceOutsidePolicyRoots(t *testing.T) {
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:                "channel-local",
				ScopeType:         agentruntime.PolicyScopeChannel,
				ScopeID:           "channel-1",
				RuntimeType:       agentruntime.RuntimeTypeLocal,
				WorkspacePolicyID: "workspace-policy-1",
				AllowLocal:        true,
			},
		},
		workspacePolicies: []agentruntime.WorkspacePolicy{
			{
				ID:           "workspace-policy-1",
				AllowedRoots: []byte(`["/workspace/allowed"]`),
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
	})

	_, _, _, err := svc.EnsureSession(EnsureSessionRequest{
		MattermostConversationID: "conversation-1",
		ChannelID:                "channel-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
		WorkspacePath:            "/workspace/blocked",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, agentruntime.ErrWorkspaceNotAllowed))
	assert.Empty(t, store.sessions)
}

func TestEnsureSessionRequiresFeatureFlagAndIDs(t *testing.T) {
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: false},
		Store:  &testStore{},
	})

	_, _, _, err := svc.EnsureSession(EnsureSessionRequest{
		MattermostConversationID: "conversation-1",
		UserID:                   "user-1",
		AgentID:                  "agent-1",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDisabled))

	svc = NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  &testStore{},
	})
	_, _, _, err = svc.EnsureSession(EnsureSessionRequest{
		UserID:  "user-1",
		AgentID: "agent-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mattermost conversation id")
}

func TestResolvePolicyBlocksDisallowedCloud(t *testing.T) {
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store: &testStore{
			policies: []agentruntime.RuntimePolicy{
				{
					ID:          "thread-cloud",
					ScopeType:   agentruntime.PolicyScopeThread,
					ScopeID:     "root-1",
					RuntimeType: agentruntime.RuntimeTypeCodex,
					AllowCloud:  false,
				},
			},
		},
	})

	_, err := svc.ResolvePolicy(EnsureSessionRequest{
		RootPostID: "root-1",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, agentruntime.ErrCloudNotAllowed))
}

func TestStartTurnStartsSelectedRuntimeAndPersistsTerminalStatus(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: "runtime-session-1", Text: "hello"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "runtime-session-1"}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:          "channel-local",
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				ProviderID:  "ollama",
				Model:       "gpt-oss:20b",
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})
	tools := llm.NewNoTools()
	tools.AddTools([]llm.Tool{{Name: "nexus__search_memory"}})
	llmContext := &llm.Context{Tools: tools}

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt:            "hi",
		Context:           llmContext,
		ShouldExecuteTool: func(llm.ToolCall) bool { return true },
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Created)
	assert.Equal(t, "hi", runtime.req.Prompt)
	assert.Same(t, llmContext, runtime.req.Context)
	require.NotNil(t, runtime.req.ShouldExecuteTool)
	assert.True(t, runtime.req.ShouldExecuteTool(llm.ToolCall{Name: "nexus__search_memory"}))
	assert.Equal(t, agentruntime.RuntimeTypeLocal, runtime.req.Session.RuntimeType)

	got := []agentruntime.RuntimeEvent{}
	for event := range result.Events {
		got = append(got, event)
	}
	require.Len(t, got, 2)
	assert.Equal(t, []agentruntime.RuntimeSessionStatus{
		agentruntime.SessionStatusRunning,
		agentruntime.SessionStatusCompleted,
	}, store.statusUpdates)
}

func TestStartTurnSanitizesRuntimeStartError(t *testing.T) {
	const leakedKey = "sk-proj-1234567890abcdefghijklmnop"
	runtime := &testRuntime{startErr: fmt.Errorf("provider failed Authorization: Bearer %s", leakedKey)}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	_, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.Error(t, err)
	require.Len(t, store.sessions, 1)
	assert.NotContains(t, err.Error(), leakedKey)
	assert.Contains(t, err.Error(), "[REDACTED]")
	assert.NotContains(t, store.sessions[0].LastError, leakedKey)
	assert.Contains(t, store.sessions[0].LastError, "[REDACTED]")
}

func TestStartTurnSanitizesRuntimeEventError(t *testing.T) {
	const leakedKey = "sk-proj-1234567890abcdefghijklmnop"
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{
		Type:      agentruntime.EventTypeError,
		SessionID: "runtime-session-1",
		Err:       fmt.Errorf("runtime failed with api key %s", leakedKey),
	}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)
	event := receiveRuntimeControlEvent(t, result.Events)

	require.Len(t, store.sessions, 1)
	require.Error(t, event.Err)
	assert.NotContains(t, event.Err.Error(), leakedKey)
	assert.Contains(t, event.Err.Error(), "[REDACTED]")
	assert.Equal(t, agentruntime.SessionStatusFailed, store.sessions[0].Status)
	assert.NotContains(t, store.sessions[0].LastError, leakedKey)
	assert.Contains(t, store.sessions[0].LastError, "[REDACTED]")
}

func TestStartTurnPersistsRuntimeUsageInSessionMetadata(t *testing.T) {
	usagePayload := json.RawMessage(`{"input_tokens":12,"output_tokens":7,"cached_read_tokens":3,"reasoning_tokens":2,"duration_ms":1250,"cost":0.0123}`)
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeUsage, SessionID: "runtime-session-1", Payload: usagePayload}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "runtime-session-1"}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:          "channel-local",
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				ProviderID:  "ollama",
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.NotEmpty(t, store.metadataUpdates)
	finalMetadata := store.metadataUpdates[len(store.metadataUpdates)-1]
	assert.JSONEq(t, `{"usage":{"input_tokens":12,"output_tokens":7,"cached_read_tokens":3,"reasoning_tokens":2,"duration_ms":1250,"cost":0.0123}}`, string(finalMetadata))
}

func TestStartTurnEstimatesCloudRuntimeUsageCostWhenProviderOmitsCost(t *testing.T) {
	usagePayload := json.RawMessage(`{"input_tokens":1000000,"output_tokens":100000}`)
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeUsage, SessionID: "runtime-session-1", Payload: usagePayload}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "runtime-session-1"}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ID:          "channel-codex",
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				ProviderID:  "codex",
				Model:       "gpt-5.3-codex",
				AllowCloud:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: runtime,
		},
	})

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.NotEmpty(t, store.metadataUpdates)
	finalMetadata := store.metadataUpdates[len(store.metadataUpdates)-1]
	var stored struct {
		Usage agentruntime.RuntimeUsage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(finalMetadata, &stored))
	assert.InDelta(t, 6.30, stored.Usage.Cost, 0.000001)
}

func TestEstimateRuntimeUsageCostUsesCacheRatesAndSkipsLocal(t *testing.T) {
	usage := agentruntime.RuntimeUsage{
		InputTokens:       1_000_000,
		OutputTokens:      100_000,
		CachedReadTokens:  100_000,
		CachedWriteTokens: 100_000,
	}

	service := NewService(NewServiceOptions{Config: testConfig{enabled: true}})
	got := service.estimateRuntimeUsageCost(agentruntime.RuntimeSession{
		RuntimeType: agentruntime.RuntimeTypeOpenAI,
		Model:       "gpt-5.6-luna",
	}, usage)

	assert.InDelta(t, 0.1535, got.Cost, 0.000001)

	local := service.estimateRuntimeUsageCost(agentruntime.RuntimeSession{
		RuntimeType: agentruntime.RuntimeTypeLocal,
		Model:       "gpt-5.6-luna",
	}, usage)
	assert.Zero(t, local.Cost)
}

func TestEstimateRuntimeUsageCostUsesCustomRatesBeforeBuiltInRates(t *testing.T) {
	service := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		CostRates: []agentruntime.RuntimeCostRate{
			{
				RuntimeType:      agentruntime.RuntimeTypeOpenAI,
				ProviderID:       "custom-provider",
				Model:            "gpt-5.6-luna",
				InputPerMillion:  10,
				OutputPerMillion: 20,
			},
		},
	})

	got := service.estimateRuntimeUsageCost(agentruntime.RuntimeSession{
		RuntimeType: agentruntime.RuntimeTypeOpenAI,
		ProviderID:  "custom-provider",
		Model:       "gpt-5.6-luna",
	}, agentruntime.RuntimeUsage{InputTokens: 100_000, OutputTokens: 100_000})

	assert.InDelta(t, 3.0, got.Cost, 0.000001)
}

func TestSetCostRatesReplacesCustomRates(t *testing.T) {
	service := NewService(NewServiceOptions{Config: testConfig{enabled: true}})
	service.SetCostRates([]agentruntime.RuntimeCostRate{{
		RuntimeType:      agentruntime.RuntimeTypeOpenAI,
		ProviderID:       "custom-provider",
		Model:            "model-1",
		InputPerMillion:  1,
		OutputPerMillion: 1,
	}})
	service.SetCostRates([]agentruntime.RuntimeCostRate{{
		RuntimeType:      agentruntime.RuntimeTypeOpenAI,
		ProviderID:       "custom-provider",
		Model:            "model-2",
		InputPerMillion:  2,
		OutputPerMillion: 2,
	}})

	oldModel := service.estimateRuntimeUsageCost(agentruntime.RuntimeSession{
		RuntimeType: agentruntime.RuntimeTypeOpenAI,
		ProviderID:  "custom-provider",
		Model:       "model-1",
	}, agentruntime.RuntimeUsage{InputTokens: 1_000_000})
	newModel := service.estimateRuntimeUsageCost(agentruntime.RuntimeSession{
		RuntimeType: agentruntime.RuntimeTypeOpenAI,
		ProviderID:  "custom-provider",
		Model:       "model-2",
	}, agentruntime.RuntimeUsage{InputTokens: 1_000_000})

	assert.Zero(t, oldModel.Cost)
	assert.Equal(t, 2.0, newModel.Cost)
}

func TestStartTurnBlocksCloudWhenPolicyBudgetExceeded(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "previous-session",
				ChannelID:   "channel-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Metadata:    json.RawMessage(`{"usage":{"cost":0.02}}`),
				CreatedAt:   time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC).UnixMilli(),
			},
		},
		policies: []agentruntime.RuntimePolicy{
			{
				ID:               "channel-codex",
				ScopeType:        agentruntime.PolicyScopeChannel,
				ScopeID:          "channel-1",
				RuntimeType:      agentruntime.RuntimeTypeCodex,
				ProviderID:       "codex",
				AllowCloud:       true,
				CloudBudgetCents: 2,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: runtime,
		},
	})

	_, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, agentruntime.ErrBudgetExceeded))
	require.Len(t, store.sessions, 2)
	assert.Equal(t, agentruntime.SessionStatusFailed, store.sessions[1].Status)
	assert.Empty(t, runtime.req.Prompt)
}

func TestRuntimeBudgetFilterScopesTeamAndServer(t *testing.T) {
	session := agentruntime.RuntimeSession{
		ServerID:   "server-1",
		TeamID:     "team-1",
		ChannelID:  "channel-1",
		RootPostID: "root-1",
		UserID:     "user-1",
		AgentID:    "agent-1",
	}

	assert.Equal(t,
		agentruntime.RuntimeSessionFilter{TeamID: "team-1"},
		runtimeBudgetFilter(agentruntime.PolicyScopeTeam, "", session, time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)),
	)
	assert.Equal(t,
		agentruntime.RuntimeSessionFilter{ServerID: "server-1"},
		runtimeBudgetFilter(agentruntime.PolicyScopeServer, "", session, time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)),
	)
}

func TestRuntimeBudgetFilterAppliesWindowStart(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, time.UTC)

	daily := runtimeBudgetFilter(agentruntime.PolicyScopeChannel, "daily", agentruntime.RuntimeSession{ChannelID: "channel-1"}, now)
	assert.Equal(t, "channel-1", daily.ChannelID)
	assert.Equal(t, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC).UnixMilli(), daily.CreatedAtFrom)

	weekly := runtimeBudgetFilter(agentruntime.PolicyScopeChannel, "weekly", agentruntime.RuntimeSession{ChannelID: "channel-1"}, now)
	assert.Equal(t, time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC).UnixMilli(), weekly.CreatedAtFrom)

	monthly := runtimeBudgetFilter(agentruntime.PolicyScopeChannel, "monthly", agentruntime.RuntimeSession{ChannelID: "channel-1"}, now)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), monthly.CreatedAtFrom)

	none := runtimeBudgetFilter(agentruntime.PolicyScopeChannel, "", agentruntime.RuntimeSession{ChannelID: "channel-1"}, now)
	assert.Zero(t, none.CreatedAtFrom)
}

func TestStartTurnBudgetWindowIgnoresOlderUsage(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "old-session",
				ChannelID:   "channel-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Metadata:    json.RawMessage(`{"usage":{"cost":9.99}}`),
				CreatedAt:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
			},
		},
		policies: []agentruntime.RuntimePolicy{
			{
				ID:                "channel-codex",
				ScopeType:         agentruntime.PolicyScopeChannel,
				ScopeID:           "channel-1",
				RuntimeType:       agentruntime.RuntimeTypeCodex,
				ProviderID:        "codex",
				AllowCloud:        true,
				CloudBudgetCents:  2,
				CloudBudgetWindow: "daily",
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: runtime,
		},
	})

	_, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})

	require.NoError(t, err)
	assert.Equal(t, "hi", runtime.req.Prompt)
}

func TestSetRuntimesReplacesRegisteredRuntimes(t *testing.T) {
	service := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  &testStore{},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: &testRuntime{},
		},
	})

	_, ok := service.runtimeFor(agentruntime.RuntimeTypeLocal)
	require.True(t, ok)

	service.SetRuntimes(map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
		agentruntime.RuntimeTypeCodex: &testRuntime{},
	})

	_, ok = service.runtimeFor(agentruntime.RuntimeTypeLocal)
	assert.False(t, ok)
	_, ok = service.runtimeFor(agentruntime.RuntimeTypeCodex)
	assert.True(t, ok)
}

func TestRegisterRuntimeIfAbsentDoesNotReplaceExistingRuntime(t *testing.T) {
	existing := &testRuntime{}
	replacement := &testRuntime{}
	service := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  &testStore{},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeOpenAI: existing,
		},
	})

	service.RegisterRuntimeIfAbsent(agentruntime.RuntimeTypeOpenAI, replacement)
	got, ok := service.runtimeFor(agentruntime.RuntimeTypeOpenAI)

	require.True(t, ok)
	assert.Same(t, existing, got)

	service.RegisterRuntimeIfAbsent(agentruntime.RuntimeTypeLocal, replacement)
	got, ok = service.runtimeFor(agentruntime.RuntimeTypeLocal)

	require.True(t, ok)
	assert.Same(t, replacement, got)
}

func TestStartTurnImportsAttachmentsBeforeRuntimeStarts(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "runtime-session-1"}
	close(events)

	runtime := &testRuntime{events: events}
	importer := &testAttachmentImporter{
		attachments: []agentruntime.RuntimeAttachment{{
			FileID:    "file-1",
			LocalPath: "/workspace/.mattermost-agent-attachments/runtime-session-1/file-1-input.txt",
		}},
	}
	svc := NewService(NewServiceOptions{
		Config:      testConfig{enabled: true},
		Store:       &testStore{},
		Attachments: importer,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	_, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
			WorkspacePath:            "/workspace",
		},
		Attachments: []agentruntime.RuntimeAttachment{{FileID: "file-1"}},
	})

	require.NoError(t, err)
	require.True(t, importer.called)
	require.Len(t, runtime.req.Attachments, 1)
	assert.Equal(t, importer.attachments[0].LocalPath, runtime.req.Attachments[0].LocalPath)
}

func TestStartSupervisorTurnCreatesRunAndAggregatesSubagents(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartSupervisorTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "replace Hermes",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	got := []agentruntime.RuntimeEvent{}
	for event := range result.Events {
		got = append(got, event)
	}

	require.Len(t, store.supervisorRuns, 1)
	require.Len(t, store.subagentRuns, 2)
	require.Len(t, runtime.subagentReqs, 2)
	assert.Equal(t, agentruntime.RunStatusCompleted, store.supervisorRuns[0].Status)
	assert.ElementsMatch(t, []string{"planner", "reviewer"}, []string{store.subagentRuns[0].Role, store.subagentRuns[1].Role})
	assert.NotEqual(t, runtime.subagentReqs[0].Session.ID, runtime.subagentReqs[1].Session.ID)
	require.NotEmpty(t, got)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[len(got)-1].Type)
	assert.Contains(t, collectRuntimeText(got), "Subagent results")
}

func TestStartSupervisorTurnBuildsHeuristicSubagentPlan(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartSupervisorTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "implement code, run tests, and research latest docs",
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.Len(t, store.supervisorRuns, 1)
	require.Len(t, store.subagentRuns, 5)
	assert.ElementsMatch(t, []string{"planner", "coder", "tester", "researcher", "reviewer"}, []string{
		store.subagentRuns[0].Role,
		store.subagentRuns[1].Role,
		store.subagentRuns[2].Role,
		store.subagentRuns[3].Role,
		store.subagentRuns[4].Role,
	})
	assert.JSONEq(t, `{
		"subagents": [
			{"role":"planner","title":"Plan task","prompt":"x","runtimeType":"local","providerID":"local"},
			{"role":"coder","title":"Implement task","prompt":"x","runtimeType":"local","providerID":"local"},
			{"role":"tester","title":"Verify task","prompt":"x","runtimeType":"local","providerID":"local"},
			{"role":"researcher","title":"Research task","prompt":"x","runtimeType":"local","providerID":"local"},
			{"role":"reviewer","title":"Review task","prompt":"x","runtimeType":"local","providerID":"local"}
		]
	}`, normalizeSupervisorPlanForTest(t, store.supervisorRuns[0].Plan))
}

func TestStartSupervisorTurnUsesMetadataSubagentPlan(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			Model:       "local-model",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartSupervisorTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
			WorkspacePath:            "/workspace/project",
		},
		Prompt: "replace Hermes",
		Metadata: []byte(`{
			"supervisorPlan": {
				"subagents": [
					{"role":"coder","title":"Custom coder","prompt":"write the implementation"},
					{"role":"tester","prompt":"verify the implementation"}
				]
			}
		}`),
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.Len(t, store.subagentRuns, 2)
	assert.ElementsMatch(t, []string{"coder", "tester"}, []string{store.subagentRuns[0].Role, store.subagentRuns[1].Role})
	require.Len(t, runtime.subagentReqs, 2)
	reqsByRole := map[string]agentruntime.SubagentRequest{}
	for _, req := range runtime.subagentReqs {
		reqsByRole[req.Role] = req
	}
	require.Contains(t, reqsByRole, "coder")
	assert.Equal(t, "write the implementation", reqsByRole["coder"].Prompt)
	assert.Equal(t, agentruntime.RuntimeTypeLocal, reqsByRole["coder"].Session.RuntimeType)
	assert.Equal(t, "local-model", reqsByRole["coder"].Session.Model)
	assert.Equal(t, "/workspace/project", reqsByRole["coder"].Session.WorkspacePath)
	assert.Contains(t, string(store.supervisorRuns[0].Plan), "Custom coder")
}

func TestStartSupervisorTurnPersistsSubagentApprovalRequest(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{
		Type:    agentruntime.EventTypeTextDelta,
		Text:    "need shell",
		Payload: json.RawMessage(`{"note":"partial"}`),
	}
	events <- agentruntime.RuntimeEvent{
		Type:    agentruntime.EventTypeApprovalRequested,
		Payload: json.RawMessage(`{"approval_id":"provider-approval-1","tool":"shell"}`),
	}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartSupervisorTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "replace Hermes",
		Metadata: []byte(`{
			"supervisorPlan": {
				"subagents": [
					{"role":"coder","prompt":"write the implementation"}
				]
			}
		}`),
	})
	require.NoError(t, err)

	got := []agentruntime.RuntimeEvent{}
	for event := range result.Events {
		got = append(got, event)
	}

	require.Len(t, store.approvals, 1)
	require.Len(t, store.subagentRuns, 1)
	assert.Equal(t, agentruntime.EventTypeApprovalRequested, findRuntimeEventType(t, got, agentruntime.EventTypeApprovalRequested).Type)
	assert.Equal(t, result.Session.ID, store.approvals[0].RuntimeSessionID)
	assert.Equal(t, store.subagentRuns[0].ID, store.approvals[0].SubagentRunID)
	assert.Equal(t, "provider-approval-1", store.approvals[0].ExternalApprovalID)
	assert.Equal(t, agentruntime.ApprovalStatusPending, store.approvals[0].Status)
	assert.Equal(t, "user-1", store.approvals[0].RequestedBy)
	assert.Equal(t, agentruntime.RunStatusWaitingApproval, store.subagentRuns[0].Status)
	assert.Equal(t, agentruntime.RunStatusWaitingApproval, store.supervisorRuns[0].Status)
	assert.Contains(t, store.statusUpdates, agentruntime.SessionStatusWaitingApproval)
}

func TestStartSupervisorTurnForcesSubagentsToSupervisorRuntimePolicy(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		FallbackPolicy: agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			Model:       "local-model",
			AllowLocal:  true,
		},
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartSupervisorTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
			WorkspacePath:            "/workspace/project",
		},
		Prompt: "replace Hermes",
		Metadata: []byte(`{
			"supervisorPlan": {
				"subagents": [
					{
						"role": "coder",
						"prompt": "write the implementation",
						"runtimeType": "codex",
						"providerID": "codex",
						"model": "gpt-5-codex",
						"workspacePath": "/tmp/outside"
					}
				]
			}
		}`),
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.Len(t, runtime.subagentReqs, 1)
	req := runtime.subagentReqs[0]
	assert.Equal(t, agentruntime.RuntimeTypeLocal, req.Session.RuntimeType)
	assert.Equal(t, "local", req.Session.ProviderID)
	assert.Equal(t, "local-model", req.Session.Model)
	assert.Equal(t, "/workspace/project", req.Session.WorkspacePath)
	assert.Contains(t, string(store.supervisorRuns[0].Plan), `"runtimeType":"local"`)
	assert.NotContains(t, string(store.supervisorRuns[0].Plan), "gpt-5-codex")
	assert.NotContains(t, string(store.supervisorRuns[0].Plan), "/tmp/outside")
}

func TestStartTurnPersistsApprovalRequest(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{
		Type:      agentruntime.EventTypeApprovalRequested,
		SessionID: "runtime-session-1",
		Payload:   []byte(`{"approval_id":"provider-approval-1","tool":"shell"}`),
	}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "runtime-session-1"}
	close(events)

	runtime := &testRuntime{events: events}
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)
	for range result.Events {
	}

	require.Len(t, store.approvals, 1)
	assert.Equal(t, "runtime-session-1", store.approvals[0].RuntimeSessionID)
	assert.Equal(t, "provider-approval-1", store.approvals[0].ExternalApprovalID)
	assert.Equal(t, agentruntime.ApprovalStatusPending, store.approvals[0].Status)
	assert.Equal(t, "user-1", store.approvals[0].RequestedBy)
	assert.Contains(t, store.statusUpdates, agentruntime.SessionStatusWaitingApproval)
}

func TestStartTurnContinuesAfterApprovalDecision(t *testing.T) {
	runtime := newApprovalContinuationRuntime()
	store := &testStore{
		policies: []agentruntime.RuntimePolicy{
			{
				ScopeType:   agentruntime.PolicyScopeChannel,
				ScopeID:     "channel-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				AllowLocal:  true,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	result, err := svc.StartTurn(context.Background(), StartTurnRequest{
		SessionRequest: EnsureSessionRequest{
			MattermostConversationID: "conversation-1",
			ChannelID:                "channel-1",
			UserID:                   "user-1",
			AgentID:                  "agent-1",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)

	approvalEvent := receiveRuntimeControlEvent(t, result.Events)
	require.Equal(t, agentruntime.EventTypeApprovalRequested, approvalEvent.Type)
	require.Len(t, store.approvals, 1)
	require.Equal(t, agentruntime.ApprovalStatusPending, store.approvals[0].Status)
	assert.Equal(t, "provider-approval-1", store.approvals[0].ExternalApprovalID)
	assert.Contains(t, store.statusUpdates, agentruntime.SessionStatusWaitingApproval)

	err = svc.SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: store.approvals[0].ID,
		UserID:     "user-1",
		Decision:   agentruntime.ApprovalDecisionAccept,
		Scope:      "session",
		Reason:     "ok",
	})
	require.NoError(t, err)

	textEvent := receiveRuntimeControlEvent(t, result.Events)
	require.Equal(t, agentruntime.EventTypeTextDelta, textEvent.Type)
	assert.Equal(t, "approved output", textEvent.Text)

	completedEvent := receiveRuntimeControlEvent(t, result.Events)
	require.Equal(t, agentruntime.EventTypeCompleted, completedEvent.Type)
	assert.Equal(t, agentruntime.ApprovalStatusAccepted, store.approvals[0].Status)
	assert.Equal(t, agentruntime.ApprovalDecisionAccept, store.approvals[0].Decision)
	assert.Contains(t, store.statusUpdates, agentruntime.SessionStatusCompleted)

	submitted := runtime.submittedApproval()
	assert.Equal(t, "runtime-session-1", submitted.SessionID)
	assert.Equal(t, "provider-approval-1", submitted.ApprovalID)
	assert.Equal(t, "user-1", submitted.UserID)
	assert.Equal(t, agentruntime.ApprovalDecisionAccept, submitted.Decision)
}

func TestExternalApprovalIDPrefersApprovalIDWhenJSONRPCIDIsNumeric(t *testing.T) {
	event := agentruntime.RuntimeEvent{
		Type:    agentruntime.EventTypeApprovalRequested,
		Payload: json.RawMessage(`{"id":0,"method":"item/commandExecution/requestApproval","approval_id":"provider-approval-1","approvalId":"provider-approval-1"}`),
	}

	assert.Equal(t, "provider-approval-1", externalApprovalID(event))
}

type testAttachmentImporter struct {
	called      bool
	attachments []agentruntime.RuntimeAttachment
}

func (i *testAttachmentImporter) ImportRuntimeAttachments(_ context.Context, _ agentruntime.RuntimeSession, _ []agentruntime.RuntimeAttachment) ([]agentruntime.RuntimeAttachment, error) {
	i.called = true
	return i.attachments, nil
}

func TestSubmitApprovalForwardsExternalApprovalIDAndPersistsDecision(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
			},
		},
		approvals: []agentruntime.RuntimeApproval{
			{
				ID:                 "approval-1",
				RuntimeSessionID:   "session-1",
				ExternalApprovalID: "provider-approval-1",
				SubagentRunID:      "subagent-1",
				Status:             agentruntime.ApprovalStatusPending,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	err := svc.SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: "approval-1",
		UserID:     "user-1",
		Decision:   agentruntime.ApprovalDecisionAccept,
		Scope:      "session",
		Reason:     "ok",
	})
	require.NoError(t, err)

	assert.Equal(t, "session-1", runtime.approval.SessionID)
	assert.Equal(t, "provider-approval-1", runtime.approval.ApprovalID)
	assert.Equal(t, "subagent-1", runtime.approval.SubagentRunID)
	assert.Equal(t, agentruntime.ApprovalStatusAccepted, store.approvals[0].Status)
	assert.Equal(t, agentruntime.ApprovalDecisionAccept, store.approvals[0].Decision)
	assert.Equal(t, "user-1", store.approvals[0].DecidedBy)
}

func TestSubmitApprovalRejectsExpiredApproval(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
			},
		},
		approvals: []agentruntime.RuntimeApproval{
			{
				ID:               "approval-1",
				RuntimeSessionID: "session-1",
				Status:           agentruntime.ApprovalStatusPending,
				ExpiresAt:        time.Now().Add(-time.Minute).UnixMilli(),
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	err := svc.SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: "approval-1",
		UserID:     "user-1",
		Decision:   agentruntime.ApprovalDecisionAccept,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime approval is expired")
	assert.Equal(t, agentruntime.ApprovalStatusExpired, store.approvals[0].Status)
	assert.Empty(t, runtime.approval.ApprovalID)
}

func TestStopSessionStopsRuntimeAndMarksCancelled(t *testing.T) {
	runtime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				Status:      agentruntime.SessionStatusRunning,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	err := svc.StopSession(context.Background(), "session-1")
	require.NoError(t, err)

	assert.Equal(t, "session-1", runtime.stopped)
	assert.Equal(t, agentruntime.SessionStatusCancelled, store.sessions[0].Status)
	assert.Contains(t, store.statusUpdates, agentruntime.SessionStatusCancelled)
}

func TestStopSessionCancelsSupervisorAndActiveSubagents(t *testing.T) {
	supervisorRuntime := &testRuntime{}
	subagentRuntime := &testRuntime{}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Status:      agentruntime.SessionStatusRunning,
			},
		},
		supervisorRuns: []agentruntime.SupervisorRun{
			{
				ID:               "supervisor-1",
				RuntimeSessionID: "session-1",
				Status:           agentruntime.RunStatusRunning,
			},
		},
		subagentRuns: []agentruntime.SubagentRun{
			{
				ID:                "subagent-running",
				SupervisorRunID:   "supervisor-1",
				RuntimeType:       agentruntime.RuntimeTypeLocal,
				ExternalSessionID: "external-subagent-session",
				Status:            agentruntime.RunStatusRunning,
			},
			{
				ID:              "subagent-completed",
				SupervisorRunID: "supervisor-1",
				RuntimeType:     agentruntime.RuntimeTypeLocal,
				Status:          agentruntime.RunStatusCompleted,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: supervisorRuntime,
			agentruntime.RuntimeTypeLocal: subagentRuntime,
		},
	})

	err := svc.StopSession(context.Background(), "session-1")
	require.NoError(t, err)

	assert.Equal(t, []string{"session-1"}, supervisorRuntime.stoppedIDs)
	assert.Equal(t, []string{"external-subagent-session"}, subagentRuntime.stoppedIDs)
	assert.Equal(t, agentruntime.RunStatusCancelled, store.supervisorRuns[0].Status)
	assert.Equal(t, agentruntime.RunStatusCancelled, store.subagentRuns[0].Status)
	assert.Equal(t, agentruntime.RunStatusCompleted, store.subagentRuns[1].Status)
	assert.JSONEq(t, `{"cancelled":true}`, string(store.subagentRuns[0].Result))
}

func TestRecoverInterruptedRunsMarksActiveStateFailed(t *testing.T) {
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{ID: "runtime-running", Status: agentruntime.SessionStatusRunning},
			{ID: "runtime-waiting", Status: agentruntime.SessionStatusWaitingApproval},
			{ID: "runtime-fresh", Status: agentruntime.SessionStatusRunning, LastEventAt: 9999999999999},
			{ID: "runtime-completed", Status: agentruntime.SessionStatusCompleted},
		},
		supervisorRuns: []agentruntime.SupervisorRun{
			{ID: "supervisor-running", Status: agentruntime.RunStatusRunning},
			{ID: "supervisor-fresh", Status: agentruntime.RunStatusRunning, UpdatedAt: 9999999999999},
			{ID: "supervisor-completed", Status: agentruntime.RunStatusCompleted},
		},
		subagentRuns: []agentruntime.SubagentRun{
			{ID: "subagent-running", SupervisorRunID: "supervisor-running", Status: agentruntime.RunStatusRunning},
			{ID: "subagent-fresh", SupervisorRunID: "supervisor-fresh", Status: agentruntime.RunStatusRunning, UpdatedAt: 9999999999999},
			{ID: "subagent-completed", SupervisorRunID: "supervisor-running", Status: agentruntime.RunStatusCompleted},
		},
	}
	service := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
	})

	require.NoError(t, service.RecoverInterruptedRuns(context.Background()))

	assert.Equal(t, agentruntime.SessionStatusFailed, store.sessions[0].Status)
	assert.Equal(t, "interrupted by plugin restart", store.sessions[0].LastError)
	assert.Equal(t, agentruntime.SessionStatusFailed, store.sessions[1].Status)
	assert.Equal(t, agentruntime.SessionStatusRunning, store.sessions[2].Status)
	assert.Equal(t, agentruntime.SessionStatusCompleted, store.sessions[3].Status)
	assert.Equal(t, agentruntime.RunStatusFailed, store.supervisorRuns[0].Status)
	assert.Equal(t, agentruntime.RunStatusRunning, store.supervisorRuns[1].Status)
	assert.Equal(t, agentruntime.RunStatusCompleted, store.supervisorRuns[2].Status)
	assert.Equal(t, agentruntime.RunStatusFailed, store.subagentRuns[0].Status)
	assert.JSONEq(t, `{"interrupted":true,"reason":"plugin_restart"}`, string(store.subagentRuns[0].Result))
	assert.Equal(t, agentruntime.RunStatusRunning, store.subagentRuns[1].Status)
	assert.Equal(t, agentruntime.RunStatusCompleted, store.subagentRuns[2].Status)
}

func TestResumeSessionStartsRuntimeAndPersistsTerminalStatus(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "session-1"}
	close(events)

	runtime := &testRuntime{resumeEvents: events}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeLocal,
				Status:      agentruntime.SessionStatusCompleted,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeLocal: runtime,
		},
	})

	resumed, err := svc.ResumeSession(context.Background(), "session-1")
	require.NoError(t, err)
	for range resumed {
	}

	assert.Equal(t, "session-1", runtime.resumed)
	assert.Equal(t, []agentruntime.RuntimeSessionStatus{
		agentruntime.SessionStatusRunning,
		agentruntime.SessionStatusCompleted,
	}, store.statusUpdates)
}

func TestResumeSessionUsesExternalSessionIDWhenAvailable(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "codex-external-1"}
	close(events)

	runtime := &testRuntime{resumeEvents: events}
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:                "session-1",
				ExternalSessionID: "codex-external-1",
				RuntimeType:       agentruntime.RuntimeTypeCodex,
				Status:            agentruntime.SessionStatusCompleted,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
		Runtimes: map[agentruntime.RuntimeType]agentruntime.AgentRuntime{
			agentruntime.RuntimeTypeCodex: runtime,
		},
	})

	resumed, err := svc.ResumeSession(context.Background(), "session-1")
	require.NoError(t, err)
	for range resumed {
	}

	assert.Equal(t, "codex-external-1", runtime.resumed)
	assert.Equal(t, agentruntime.SessionStatusCompleted, store.sessions[0].Status)
}

func TestPersistTerminalEventsStoresExternalSessionID(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: "session-1", ExternalSessionID: "codex-external-1"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "session-1"}
	close(events)

	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		},
	}
	svc := NewService(NewServiceOptions{Config: testConfig{enabled: true}, Store: store})

	for range svc.persistTerminalEvents("session-1", events) {
	}

	assert.Equal(t, "codex-external-1", store.sessions[0].ExternalSessionID)
	assert.Equal(t, []string{"codex-external-1"}, store.externalUpdates)
}

func TestPersistTerminalEventsStoresKnownStatusEvents(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: "session-1", Text: string(agentruntime.SessionStatusWaitingApproval)}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: "session-1", Text: "arbitrary progress"}
	close(events)

	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		},
	}
	svc := NewService(NewServiceOptions{Config: testConfig{enabled: true}, Store: store})

	for range svc.persistTerminalEvents("session-1", events) {
	}

	assert.Equal(t, agentruntime.SessionStatusWaitingApproval, store.sessions[0].Status)
	assert.Equal(t, []agentruntime.RuntimeSessionStatus{agentruntime.SessionStatusWaitingApproval}, store.statusUpdates)
}

func TestPersistTerminalEventsNormalizesProviderSessionID(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: "codex-external-1"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: "codex-external-1"}
	close(events)

	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{ID: "session-1", RuntimeType: agentruntime.RuntimeTypeCodex},
		},
	}
	svc := NewService(NewServiceOptions{Config: testConfig{enabled: true}, Store: store})

	got := []agentruntime.RuntimeEvent{}
	for event := range svc.persistTerminalEvents("session-1", events) {
		got = append(got, event)
	}

	require.Len(t, got, 2)
	assert.Equal(t, "session-1", got[0].SessionID)
	assert.Equal(t, "codex-external-1", got[0].ExternalSessionID)
	assert.Equal(t, "session-1", got[1].SessionID)
	assert.Equal(t, "codex-external-1", got[1].ExternalSessionID)
	assert.Equal(t, "codex-external-1", store.sessions[0].ExternalSessionID)
	assert.Equal(t, []string{"codex-external-1"}, store.externalUpdates)
}

func TestGetStatusFallsBackToStoredSessionWhenRuntimeUnavailable(t *testing.T) {
	store := &testStore{
		sessions: []agentruntime.RuntimeSession{
			{
				ID:          "session-1",
				RuntimeType: agentruntime.RuntimeTypeCodex,
				Status:      agentruntime.SessionStatusCompleted,
				UpdatedAt:   123,
			},
		},
	}
	svc := NewService(NewServiceOptions{
		Config: testConfig{enabled: true},
		Store:  store,
	})

	status, err := svc.GetStatus(context.Background(), "session-1")
	require.NoError(t, err)

	assert.Equal(t, "session-1", status.SessionID)
	assert.Equal(t, agentruntime.SessionStatusCompleted, status.Status)
	assert.Equal(t, agentruntime.RuntimeTypeCodex, status.RuntimeType)
}

func collectRuntimeText(events []agentruntime.RuntimeEvent) string {
	var text strings.Builder
	for _, event := range events {
		if event.Type == agentruntime.EventTypeTextDelta {
			text.WriteString(event.Text)
		}
	}
	return text.String()
}

func findRuntimeEventType(t *testing.T, events []agentruntime.RuntimeEvent, eventType agentruntime.RuntimeEventType) agentruntime.RuntimeEvent {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	require.Failf(t, "runtime event not found", "missing runtime event %q", eventType)
	return agentruntime.RuntimeEvent{}
}

func normalizeSupervisorPlanForTest(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var plan supervisorPlan
	require.NoError(t, json.Unmarshal(raw, &plan))
	for i := range plan.Subagents {
		plan.Subagents[i].Prompt = "x"
	}
	out, err := json.Marshal(plan)
	require.NoError(t, err)
	return string(out)
}
