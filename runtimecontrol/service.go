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
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost-plugin-agents/v2/supervisororchestrator"
	"github.com/mattermost/mattermost/server/public/model"
)

var ErrDisabled = errors.New("agent runtime control plane is disabled")

const (
	interruptedRecoveryStaleAfter = 2 * time.Minute
	defaultRuntimeApprovalTTL     = 24 * time.Hour
)

type Config interface {
	EnableAgentRuntimeControlPlane() bool
}

type Store interface {
	CreateRuntimeSession(session *agentruntime.RuntimeSession) error
	GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error)
	GetRuntimeSessionByConversationAgent(conversationID, agentID string) (*agentruntime.RuntimeSession, error)
	ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error)
	ListRuntimePolicies() ([]agentruntime.RuntimePolicy, error)
	GetWorkspacePolicy(id string) (*agentruntime.WorkspacePolicy, error)
	UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error
	UpdateRuntimeSessionMetadata(id string, metadata json.RawMessage) error
	UpdateRuntimeSessionExternalSessionID(id string, externalSessionID string) error
	CreateRuntimeApproval(approval *agentruntime.RuntimeApproval) error
	GetRuntimeApproval(id string) (*agentruntime.RuntimeApproval, error)
	UpdateRuntimeApprovalDecision(id string, decision agentruntime.RuntimeApprovalDecision, status agentruntime.RuntimeApprovalStatus) error
	ExpireRuntimeApprovals(now int64) (int64, error)
	CreateSupervisorRun(run *agentruntime.SupervisorRun) error
	ListSupervisorRuns(filter store.SupervisorRunFilter) ([]agentruntime.SupervisorRun, error)
	CreateSubagentRun(run *agentruntime.SubagentRun) error
	ListSubagentRuns(supervisorRunID string) ([]agentruntime.SubagentRun, error)
	UpdateSupervisorRunStatus(id, status, finalResultPostID string) error
	UpdateSubagentRunResult(id, status string, result json.RawMessage, summary string) error
}

type AttachmentImporter interface {
	ImportRuntimeAttachments(ctx context.Context, session agentruntime.RuntimeSession, attachments []agentruntime.RuntimeAttachment) ([]agentruntime.RuntimeAttachment, error)
}

type Service struct {
	cfg                Config
	store              Store
	fallback           agentruntime.RuntimePolicy
	runtimeMu          sync.RWMutex
	runtimes           map[agentruntime.RuntimeType]agentruntime.AgentRuntime
	attachmentImporter AttachmentImporter
	costRatesMu        sync.RWMutex
	costRates          []agentruntime.RuntimeCostRate
}

type NewServiceOptions struct {
	Config         Config
	Store          Store
	FallbackPolicy agentruntime.RuntimePolicy
	Runtimes       map[agentruntime.RuntimeType]agentruntime.AgentRuntime
	Attachments    AttachmentImporter
	CostRates      []agentruntime.RuntimeCostRate
}

type EnsureSessionRequest struct {
	MattermostConversationID string
	ChannelID                string
	RootPostID               string
	UserID                   string
	AgentID                  string
	ServerID                 string
	TeamID                   string
	IsDM                     bool
	WorkspacePath            string
	PolicyOverride           *agentruntime.RuntimePolicy
}

type StartTurnRequest struct {
	SessionRequest    EnsureSessionRequest
	Prompt            string
	Context           *llm.Context
	ShouldExecuteTool func(llm.ToolCall) bool
	// InitialContext is prepended only when this request creates the session.
	// It lets a channel session see bounded history without resending it on every turn.
	InitialContext string
	Attachments    []agentruntime.RuntimeAttachment
	Metadata       []byte
}

type StartTurnResult struct {
	Session agentruntime.RuntimeSession
	Policy  agentruntime.EffectivePolicy
	Created bool
	Events  <-chan agentruntime.RuntimeEvent
}

type supervisorPlan struct {
	Subagents []supervisorPlanSubagent `json:"subagents"`
}

type supervisorPlanSubagent struct {
	Role          string                   `json:"role"`
	Title         string                   `json:"title,omitempty"`
	Prompt        string                   `json:"prompt"`
	RuntimeType   agentruntime.RuntimeType `json:"runtimeType,omitempty"`
	ProviderID    string                   `json:"providerID,omitempty"`
	Model         string                   `json:"model,omitempty"`
	WorkspacePath string                   `json:"workspacePath,omitempty"`
}

func NewService(opts NewServiceOptions) *Service {
	fallback := opts.FallbackPolicy
	if fallback.RuntimeType == "" {
		fallback.RuntimeType = agentruntime.RuntimeTypeCodex
	}
	if !fallback.AllowCloud && !fallback.AllowLocal {
		fallback.AllowCloud = true
		fallback.AllowLocal = true
	}

	return &Service{
		cfg:                opts.Config,
		store:              opts.Store,
		fallback:           fallback,
		runtimes:           opts.Runtimes,
		attachmentImporter: opts.Attachments,
		costRates:          normalizedRuntimeCostRates(opts.CostRates),
	}
}

func (s *Service) RegisterRuntime(runtimeType agentruntime.RuntimeType, runtime agentruntime.AgentRuntime) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimes == nil {
		s.runtimes = map[agentruntime.RuntimeType]agentruntime.AgentRuntime{}
	}
	s.runtimes[runtimeType] = runtime
}

func (s *Service) RegisterRuntimeIfAbsent(runtimeType agentruntime.RuntimeType, runtime agentruntime.AgentRuntime) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimes == nil {
		s.runtimes = map[agentruntime.RuntimeType]agentruntime.AgentRuntime{}
	}
	if s.runtimes[runtimeType] != nil {
		return
	}
	s.runtimes[runtimeType] = runtime
}

func (s *Service) SetRuntimes(runtimes map[agentruntime.RuntimeType]agentruntime.AgentRuntime) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.runtimes = cloneRuntimes(runtimes)
}

func (s *Service) SetCostRates(rates []agentruntime.RuntimeCostRate) {
	s.costRatesMu.Lock()
	defer s.costRatesMu.Unlock()
	s.costRates = normalizedRuntimeCostRates(rates)
}

func (s *Service) costRatesSnapshot() []agentruntime.RuntimeCostRate {
	s.costRatesMu.RLock()
	defer s.costRatesMu.RUnlock()
	return append([]agentruntime.RuntimeCostRate(nil), s.costRates...)
}

func (s *Service) runtimeFor(runtimeType agentruntime.RuntimeType) (agentruntime.AgentRuntime, bool) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	runtime, ok := s.runtimes[runtimeType]
	return runtime, ok
}

func (s *Service) runtimesSnapshot() map[agentruntime.RuntimeType]agentruntime.AgentRuntime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return cloneRuntimes(s.runtimes)
}

func cloneRuntimes(runtimes map[agentruntime.RuntimeType]agentruntime.AgentRuntime) map[agentruntime.RuntimeType]agentruntime.AgentRuntime {
	if len(runtimes) == 0 {
		return map[agentruntime.RuntimeType]agentruntime.AgentRuntime{}
	}
	clone := make(map[agentruntime.RuntimeType]agentruntime.AgentRuntime, len(runtimes))
	for runtimeType, runtime := range runtimes {
		if runtimeType != "" && runtime != nil {
			clone[runtimeType] = runtime
		}
	}
	return clone
}

func (s *Service) ResolvePolicy(req EnsureSessionRequest) (agentruntime.EffectivePolicy, error) {
	if err := s.requireEnabled(); err != nil {
		return agentruntime.EffectivePolicy{}, err
	}

	policies, err := s.store.ListRuntimePolicies()
	if err != nil {
		return agentruntime.EffectivePolicy{}, fmt.Errorf("failed to list runtime policies: %w", err)
	}

	return agentruntime.ResolveEffectivePolicy(policies, agentruntime.PolicyLookup{
		ServerID:  req.ServerID,
		TeamID:    req.TeamID,
		ChannelID: req.ChannelID,
		ThreadID:  req.RootPostID,
		UserID:    req.UserID,
		AgentID:   req.AgentID,
		IsDM:      req.IsDM,
	}, s.fallback)
}

func (s *Service) EnsureSession(req EnsureSessionRequest) (*agentruntime.RuntimeSession, agentruntime.EffectivePolicy, bool, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, agentruntime.EffectivePolicy{}, false, err
	}
	if req.MattermostConversationID == "" {
		return nil, agentruntime.EffectivePolicy{}, false, errors.New("mattermost conversation id is required")
	}
	if req.UserID == "" {
		return nil, agentruntime.EffectivePolicy{}, false, errors.New("user id is required")
	}
	if req.AgentID == "" {
		return nil, agentruntime.EffectivePolicy{}, false, errors.New("agent id is required")
	}

	existing, err := s.store.GetRuntimeSessionByConversationAgent(req.MattermostConversationID, req.AgentID)
	if err == nil {
		if strings.HasPrefix(req.MattermostConversationID, "channel:") {
			current, policyErr := s.effectivePolicy(req)
			if policyErr != nil {
				return nil, agentruntime.EffectivePolicy{}, false, policyErr
			}
			if current.Policy.RuntimeType != existing.RuntimeType {
				return nil, agentruntime.EffectivePolicy{}, false, errors.New("channel runtime policy changed; an existing session cannot switch runtimes")
			}
			return existing, current, false, nil
		}
		effective := agentruntime.EffectivePolicy{
			Policy: agentruntime.RuntimePolicy{
				RuntimeType: existing.RuntimeType,
				ProviderID:  existing.ProviderID,
				Model:       existing.Model,
			},
			Source: agentruntime.PolicyScopeThread,
		}
		return existing, effective, false, nil
	}
	if !errors.Is(err, store.ErrRuntimeSessionNotFound) {
		return nil, agentruntime.EffectivePolicy{}, false, fmt.Errorf("failed to get runtime session: %w", err)
	}

	effective, err := s.effectivePolicy(req)
	if err != nil {
		return nil, agentruntime.EffectivePolicy{}, false, err
	}
	workspacePath, err := s.resolveWorkspacePath(req.WorkspacePath, effective.Policy.WorkspacePolicyID)
	if err != nil {
		return nil, agentruntime.EffectivePolicy{}, false, err
	}

	session := &agentruntime.RuntimeSession{
		MattermostConversationID: req.MattermostConversationID,
		ServerID:                 req.ServerID,
		TeamID:                   req.TeamID,
		ChannelID:                req.ChannelID,
		RootPostID:               req.RootPostID,
		UserID:                   req.UserID,
		AgentID:                  req.AgentID,
		RuntimeType:              effective.Policy.RuntimeType,
		ProviderID:               effective.Policy.ProviderID,
		Model:                    effective.Policy.Model,
		WorkspacePath:            workspacePath,
		Status:                   agentruntime.SessionStatusIdle,
	}
	if err := s.store.CreateRuntimeSession(session); err != nil {
		return nil, agentruntime.EffectivePolicy{}, false, fmt.Errorf("failed to create runtime session: %w", err)
	}

	return session, effective, true, nil
}

func (s *Service) effectivePolicy(req EnsureSessionRequest) (agentruntime.EffectivePolicy, error) {
	if req.PolicyOverride != nil {
		return agentruntime.ValidatePolicy(*req.PolicyOverride, agentruntime.PolicyScopeThread)
	}
	return s.ResolvePolicy(req)
}

func (s *Service) enforceRuntimeBudget(policy agentruntime.EffectivePolicy, session agentruntime.RuntimeSession) error {
	if !cloudRuntime(session.RuntimeType) || policy.Policy.CloudBudgetCents <= 0 {
		return nil
	}
	sessions, err := s.store.ListRuntimeSessions(runtimeBudgetFilter(policy.Source, policy.Policy.CloudBudgetWindow, session, time.Now()))
	if err != nil {
		return fmt.Errorf("failed to list runtime sessions for budget: %w", err)
	}
	spentCents := runtimeUsageCostCents(sessions)
	if spentCents >= policy.Policy.CloudBudgetCents {
		return fmt.Errorf("%w: spent %d cents of %d cents for %s scope", agentruntime.ErrBudgetExceeded, spentCents, policy.Policy.CloudBudgetCents, policy.Source)
	}
	return nil
}

func runtimeBudgetFilter(scope agentruntime.PolicyScopeType, budgetWindow string, session agentruntime.RuntimeSession, now time.Time) agentruntime.RuntimeSessionFilter {
	filter := agentruntime.RuntimeSessionFilter{}
	switch scope {
	case agentruntime.PolicyScopeThread:
		filter = agentruntime.RuntimeSessionFilter{ChannelID: session.ChannelID, RootPostID: session.RootPostID}
	case agentruntime.PolicyScopeChannel:
		filter = agentruntime.RuntimeSessionFilter{ChannelID: session.ChannelID}
	case agentruntime.PolicyScopeUser:
		filter = agentruntime.RuntimeSessionFilter{UserID: session.UserID}
	case agentruntime.PolicyScopeAgent:
		filter = agentruntime.RuntimeSessionFilter{AgentID: session.AgentID}
	case agentruntime.PolicyScopeTeam:
		filter = agentruntime.RuntimeSessionFilter{TeamID: session.TeamID}
	case agentruntime.PolicyScopeServer:
		filter = agentruntime.RuntimeSessionFilter{ServerID: session.ServerID}
	}
	filter.CreatedAtFrom = runtimeBudgetWindowStart(budgetWindow, now)
	return filter
}

func runtimeBudgetWindowStart(window string, now time.Time) int64 {
	utc := now.UTC()
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "daily":
		return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
	case "weekly":
		weekday := int(utc.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(weekday - 1))
		return start.UnixMilli()
	case "monthly":
		return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	default:
		return 0
	}
}

func runtimeUsageCostCents(sessions []agentruntime.RuntimeSession) int64 {
	var cents int64
	for _, session := range sessions {
		usage := runtimeUsageFromSessionMetadata(session.Metadata)
		if usage.Cost <= 0 {
			continue
		}
		cents += int64(usage.Cost*100 + 0.999999)
	}
	return cents
}

func runtimeUsageFromSessionMetadata(raw json.RawMessage) agentruntime.RuntimeUsage {
	if len(raw) == 0 {
		return agentruntime.RuntimeUsage{}
	}
	var metadata struct {
		Usage agentruntime.RuntimeUsage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &metadata)
	return metadata.Usage
}

func cloudRuntime(runtimeType agentruntime.RuntimeType) bool {
	return runtimeType == agentruntime.RuntimeTypeCodex || runtimeType == agentruntime.RuntimeTypeOpenAI
}

func (s *Service) resolveWorkspacePath(requested, workspacePolicyID string) (string, error) {
	if workspacePolicyID == "" {
		return requested, nil
	}
	policy, err := s.store.GetWorkspacePolicy(workspacePolicyID)
	if err != nil {
		if errors.Is(err, store.ErrWorkspacePolicyNotFound) {
			return "", fmt.Errorf("%w: workspace policy %q not found", agentruntime.ErrWorkspaceNotAllowed, workspacePolicyID)
		}
		return "", fmt.Errorf("failed to get workspace policy: %w", err)
	}
	return agentruntime.ResolveWorkspacePath(requested, policy)
}

func (s *Service) StartTurn(ctx context.Context, req StartTurnRequest) (*StartTurnResult, error) {
	session, policy, created, err := s.EnsureSession(req.SessionRequest)
	if err != nil {
		return nil, err
	}
	turnSession := *session
	turnSession.UserID = req.SessionRequest.UserID
	if err := s.enforceRuntimeBudget(policy, turnSession); err != nil {
		_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitizeRuntimeErrorMessage(err))
		return nil, err
	}

	runtime, ok := s.runtimeFor(session.RuntimeType)
	if !ok || runtime == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}

	attachments := req.Attachments
	if s.attachmentImporter != nil {
		attachments, err = s.attachmentImporter.ImportRuntimeAttachments(ctx, *session, req.Attachments)
		if err != nil {
			sanitized := sanitizeRuntimeErrorMessage(err)
			_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitized)
			return nil, fmt.Errorf("failed to import runtime attachments: %s", sanitized)
		}
	}

	prompt := req.Prompt
	if created && req.InitialContext != "" {
		prompt = req.InitialContext + "\n\n" + prompt
	}
	// The stored session identifies the channel conversation. Each turn must
	// still carry the actual author for runtime authorization and attribution.
	events, err := runtime.StartTurn(ctx, agentruntime.RuntimeTurnRequest{
		Session:           turnSession,
		Prompt:            prompt,
		Context:           req.Context,
		ShouldExecuteTool: req.ShouldExecuteTool,
		Attachments:       attachments,
		Metadata:          req.Metadata,
	})
	if err != nil {
		sanitized := sanitizeRuntimeErrorMessage(err)
		_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitized)
		return nil, errors.New(sanitized)
	}

	_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusRunning, "")

	return &StartTurnResult{
		Session: *session,
		Policy:  policy,
		Created: created,
		Events:  s.persistTerminalEvents(session.ID, events, req.SessionRequest.UserID),
	}, nil
}

func (s *Service) StartSupervisorTurn(ctx context.Context, req StartTurnRequest) (*StartTurnResult, error) {
	session, policy, created, err := s.EnsureSession(req.SessionRequest)
	if err != nil {
		return nil, err
	}
	if err := s.enforceRuntimeBudget(policy, *session); err != nil {
		_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitizeRuntimeErrorMessage(err))
		return nil, err
	}
	runtimes := s.runtimesSnapshot()
	if _, ok := runtimes[session.RuntimeType]; !ok {
		return nil, agentruntime.ErrRuntimeUnavailable
	}
	if s.attachmentImporter != nil {
		if _, err := s.attachmentImporter.ImportRuntimeAttachments(ctx, *session, req.Attachments); err != nil {
			sanitized := sanitizeRuntimeErrorMessage(err)
			_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitized)
			return nil, fmt.Errorf("failed to import runtime attachments: %s", sanitized)
		}
	}

	orchestrator := supervisororchestrator.New(supervisororchestrator.Options{
		Store:    s.store,
		Runtimes: runtimes,
		Guard: func(runtimeType agentruntime.RuntimeType) error {
			if runtimeType != session.RuntimeType {
				return fmt.Errorf("%w: subagent runtime %q does not match supervisor runtime %q", agentruntime.ErrCloudNotAllowed, runtimeType, session.RuntimeType)
			}
			return nil
		},
	})
	plan, specs := buildSupervisorPlan(*session, req.Prompt, req.Metadata)
	supervisorRun, err := orchestrator.StartRun(supervisororchestrator.StartRunRequest{
		RuntimeSessionID:         session.ID,
		MattermostConversationID: session.MattermostConversationID,
		SupervisorAgentID:        session.AgentID,
		Objective:                req.Prompt,
		CreatedBy:                session.UserID,
		Plan:                     plan,
		Metadata:                 runtimeSupervisorMetadata(req.Metadata),
	})
	if err != nil {
		sanitized := sanitizeRuntimeErrorMessage(err)
		_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitized)
		return nil, errors.New(sanitized)
	}

	_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusRunning, "")
	events := s.runSupervisorSubagents(ctx, orchestrator, *session, supervisorRun.ID, specs)

	return &StartTurnResult{
		Session: *session,
		Policy:  policy,
		Created: created,
		Events:  s.persistTerminalEvents(session.ID, events),
	}, nil
}

func (s *Service) runSupervisorSubagents(ctx context.Context, orchestrator *supervisororchestrator.Orchestrator, session agentruntime.RuntimeSession, supervisorRunID string, specs []supervisororchestrator.SubagentSpec) <-chan agentruntime.RuntimeEvent {
	out := make(chan agentruntime.RuntimeEvent)
	go func() {
		defer close(out)
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: session.ID, Text: fmt.Sprintf("Supervisor started with %d subagents.\n\n", len(specs))}

		type subagentOutcome struct {
			role    string
			summary string
			err     error
			waiting bool
		}
		results := make(chan subagentOutcome, len(specs))
		var wg sync.WaitGroup
		for _, spec := range specs {
			spec := spec
			spec.SupervisorRunID = supervisorRunID
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := orchestrator.StartSubagent(ctx, spec)
				if err != nil {
					results <- subagentOutcome{role: spec.Role, err: err}
					return
				}
				var text strings.Builder
				for event := range result.Events {
					switch event.Type {
					case agentruntime.EventTypeTextDelta:
						text.WriteString(event.Text)
					case agentruntime.EventTypeApprovalRequested:
						out <- event
						results <- subagentOutcome{role: spec.Role, summary: text.String(), waiting: true}
						return
					case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
						if event.Err != nil {
							results <- subagentOutcome{role: spec.Role, err: event.Err}
							return
						}
						results <- subagentOutcome{role: spec.Role, err: errors.New(event.Text)}
						return
					}
				}
				results <- subagentOutcome{role: spec.Role, summary: text.String()}
			}()
		}
		wg.Wait()
		close(results)

		var failed bool
		var waiting bool
		var summary strings.Builder
		summary.WriteString("Subagent results:\n")
		for result := range results {
			if result.waiting {
				waiting = true
				summary.WriteString(fmt.Sprintf("- %s waiting for approval: %s\n", result.role, truncateRuntimeSummary(result.summary)))
				continue
			}
			if result.err != nil {
				failed = true
				summary.WriteString(fmt.Sprintf("- %s failed: %s\n", result.role, sanitizeRuntimeErrorMessage(result.err)))
				continue
			}
			summary.WriteString(fmt.Sprintf("- %s: %s\n", result.role, truncateRuntimeSummary(result.summary)))
		}
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: session.ID, Text: summary.String()}
		if failed {
			_ = s.store.UpdateSupervisorRunStatus(supervisorRunID, agentruntime.RunStatusFailed, "")
			out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: session.ID, Err: errors.New("one or more subagents failed")}
			return
		}
		if waiting {
			_ = s.store.UpdateSupervisorRunStatus(supervisorRunID, agentruntime.RunStatusWaitingApproval, "")
			out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: session.ID, Text: summary.String()}
			return
		}
		_ = s.store.UpdateSupervisorRunStatus(supervisorRunID, agentruntime.RunStatusCompleted, "")
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: session.ID}
	}()
	return out
}

func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	if sessionID == "" {
		return errors.New("runtime session id is required")
	}

	session, err := s.store.GetRuntimeSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get runtime session: %w", err)
	}
	runtime, ok := s.runtimeFor(session.RuntimeType)
	if !ok || runtime == nil {
		return agentruntime.ErrRuntimeUnavailable
	}
	if err := runtime.StopTurn(ctx, session.ID); err != nil {
		return fmt.Errorf("failed to stop runtime session: %w", err)
	}
	if err := s.cancelSupervisorRuns(ctx, *session); err != nil {
		return err
	}
	return s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusCancelled, "")
}

func (s *Service) RecoverInterruptedRuns(ctx context.Context) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var errs []error
	cutoff := model.GetMillis() - interruptedRecoveryStaleAfter.Milliseconds()
	for _, status := range []agentruntime.RuntimeSessionStatus{agentruntime.SessionStatusRunning, agentruntime.SessionStatusWaitingApproval} {
		sessions, err := s.store.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{Status: status})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to list interrupted runtime sessions: %w", err))
			continue
		}
		for _, session := range sessions {
			if !staleRuntimeSession(session, cutoff) {
				continue
			}
			if err := s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, "interrupted by plugin restart"); err != nil {
				errs = append(errs, fmt.Errorf("failed to mark runtime session %s failed: %w", session.ID, err))
			}
		}
	}

	seenSupervisorRuns := map[string]bool{}
	for _, status := range []string{agentruntime.RunStatusPending, agentruntime.RunStatusRunning, agentruntime.RunStatusWaitingApproval} {
		runs, err := s.store.ListSupervisorRuns(store.SupervisorRunFilter{Status: status})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to list interrupted supervisor runs: %w", err))
			continue
		}
		for _, run := range runs {
			if seenSupervisorRuns[run.ID] {
				continue
			}
			if !staleSupervisorRun(run, cutoff) {
				continue
			}
			seenSupervisorRuns[run.ID] = true
			if err := s.failInterruptedSupervisorRun(run, cutoff); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (s *Service) cancelSupervisorRuns(ctx context.Context, session agentruntime.RuntimeSession) error {
	runs, err := s.store.ListSupervisorRuns(store.SupervisorRunFilter{RuntimeSessionID: session.ID})
	if err != nil {
		return fmt.Errorf("failed to list supervisor runs: %w", err)
	}
	var errs []error
	for _, run := range runs {
		subagents, err := s.store.ListSubagentRuns(run.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to list subagent runs for supervisor %s: %w", run.ID, err))
			continue
		}
		for _, subagent := range subagents {
			if !activeRunStatus(subagent.Status) {
				continue
			}
			if runtime, ok := s.runtimeFor(subagent.RuntimeType); ok && runtime != nil {
				stopID := firstNonEmpty(subagent.ExternalSessionID, subagent.ID)
				if err := runtime.StopTurn(ctx, stopID); err != nil {
					errs = append(errs, fmt.Errorf("failed to stop subagent %s: %w", subagent.ID, err))
				}
			}
			if err := s.store.UpdateSubagentRunResult(subagent.ID, agentruntime.RunStatusCancelled, runtimeCancelPayload(), subagent.Summary); err != nil {
				errs = append(errs, fmt.Errorf("failed to mark subagent %s cancelled: %w", subagent.ID, err))
			}
		}
		if activeRunStatus(run.Status) {
			if err := s.store.UpdateSupervisorRunStatus(run.ID, agentruntime.RunStatusCancelled, ""); err != nil {
				errs = append(errs, fmt.Errorf("failed to mark supervisor %s cancelled: %w", run.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) failInterruptedSupervisorRun(run agentruntime.SupervisorRun, cutoff int64) error {
	var errs []error
	subagents, err := s.store.ListSubagentRuns(run.ID)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to list interrupted subagents for supervisor %s: %w", run.ID, err))
	} else {
		for _, subagent := range subagents {
			if !activeRunStatus(subagent.Status) {
				continue
			}
			if !staleSubagentRun(subagent, cutoff) {
				continue
			}
			payload := json.RawMessage(`{"interrupted":true,"reason":"plugin_restart"}`)
			if err := s.store.UpdateSubagentRunResult(subagent.ID, agentruntime.RunStatusFailed, payload, subagent.Summary); err != nil {
				errs = append(errs, fmt.Errorf("failed to mark subagent %s failed: %w", subagent.ID, err))
			}
		}
	}
	if activeRunStatus(run.Status) {
		if err := s.store.UpdateSupervisorRunStatus(run.ID, agentruntime.RunStatusFailed, ""); err != nil {
			errs = append(errs, fmt.Errorf("failed to mark supervisor %s failed: %w", run.ID, err))
		}
	}
	return errors.Join(errs...)
}

func staleRuntimeSession(session agentruntime.RuntimeSession, cutoff int64) bool {
	return session.LastEventAt == 0 || session.LastEventAt <= cutoff
}

func staleSupervisorRun(run agentruntime.SupervisorRun, cutoff int64) bool {
	return run.UpdatedAt == 0 || run.UpdatedAt <= cutoff
}

func staleSubagentRun(run agentruntime.SubagentRun, cutoff int64) bool {
	if run.UpdatedAt > 0 {
		return run.UpdatedAt <= cutoff
	}
	return run.StartedAt == 0 || run.StartedAt <= cutoff
}

func activeRunStatus(status string) bool {
	switch status {
	case "", agentruntime.RunStatusPending, agentruntime.RunStatusRunning, agentruntime.RunStatusWaitingApproval:
		return true
	default:
		return false
	}
}

func runtimeCancelPayload() json.RawMessage {
	return json.RawMessage(`{"cancelled":true}`)
}

func buildSupervisorPlan(session agentruntime.RuntimeSession, prompt string, metadata []byte) (json.RawMessage, []supervisororchestrator.SubagentSpec) {
	plan := supervisorPlanFromMetadata(metadata)
	if len(plan.Subagents) == 0 {
		plan = heuristicSupervisorPlan(session, prompt)
	}

	specs := make([]supervisororchestrator.SubagentSpec, 0, len(plan.Subagents))
	normalized := supervisorPlan{Subagents: make([]supervisorPlanSubagent, 0, len(plan.Subagents))}
	seen := map[string]bool{}
	for _, subagent := range plan.Subagents {
		subagent.Role = strings.ToLower(strings.TrimSpace(subagent.Role))
		subagent.Title = strings.TrimSpace(subagent.Title)
		subagent.Prompt = strings.TrimSpace(subagent.Prompt)
		if subagent.Role == "" || subagent.Prompt == "" || seen[subagent.Role] {
			continue
		}
		seen[subagent.Role] = true
		subagent.RuntimeType = session.RuntimeType
		subagent.ProviderID = session.ProviderID
		subagent.Model = session.Model
		subagent.WorkspacePath = session.WorkspacePath
		normalized.Subagents = append(normalized.Subagents, subagent)
		specs = append(specs, supervisororchestrator.SubagentSpec{
			Role:          subagent.Role,
			Title:         subagent.Title,
			Prompt:        subagent.Prompt,
			Session:       session,
			RuntimeType:   subagent.RuntimeType,
			ProviderID:    subagent.ProviderID,
			Model:         subagent.Model,
			WorkspacePath: subagent.WorkspacePath,
		})
	}
	if len(specs) == 0 {
		return buildSupervisorPlan(session, prompt, nil)
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return json.RawMessage(`{"subagents":[]}`), specs
	}
	return raw, specs
}

func supervisorPlanFromMetadata(raw []byte) supervisorPlan {
	if len(raw) == 0 {
		return supervisorPlan{}
	}
	var direct supervisorPlan
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct.Subagents) > 0 {
		return direct
	}
	var envelope struct {
		SupervisorPlan supervisorPlan `json:"supervisorPlan"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.SupervisorPlan.Subagents) > 0 {
		return envelope.SupervisorPlan
	}
	return supervisorPlan{}
}

func heuristicSupervisorPlan(session agentruntime.RuntimeSession, prompt string) supervisorPlan {
	task := strings.ToLower(prompt)
	subagents := []supervisorPlanSubagent{
		{
			Role:   "planner",
			Title:  "Plan task",
			Prompt: "Act as the planner subagent. Break down the task, identify dependencies and risks, and return a concise execution plan.\n\nTask:\n" + prompt,
		},
	}
	if containsAny(task, "implement", "code", "coding", "build", "edit", "patch", "entwick", "umsetz", "program") {
		subagents = append(subagents, supervisorPlanSubagent{
			Role:   "coder",
			Title:  "Implement task",
			Prompt: "Act as the implementation subagent. Propose concrete code changes, affected files, and integration steps. Keep the result actionable.\n\nTask:\n" + prompt,
		})
	}
	if containsAny(task, "test", "verify", "validation", "validate", "lint", "qa", "prüf", "pruef", "verifiz") {
		subagents = append(subagents, supervisorPlanSubagent{
			Role:   "tester",
			Title:  "Verify task",
			Prompt: "Act as the verification subagent. Define the tests, checks, and failure cases needed before the supervisor can accept the result.\n\nTask:\n" + prompt,
		})
	}
	if containsAny(task, "research", "docs", "document", "latest", "find", "search", "herausfind", "recherch", "dokument") {
		subagents = append(subagents, supervisorPlanSubagent{
			Role:   "researcher",
			Title:  "Research task",
			Prompt: "Act as the research subagent. Identify external facts, docs, or unknowns that must be checked and summarize what the supervisor needs to know.\n\nTask:\n" + prompt,
		})
	}
	subagents = append(subagents, supervisorPlanSubagent{
		Role:   "reviewer",
		Title:  "Review task",
		Prompt: "Act as the reviewer subagent. Inspect the task for missing requirements, safety constraints, and verification needs. Return concrete findings.\n\nTask:\n" + prompt,
	})
	for i := range subagents {
		subagents[i].RuntimeType = session.RuntimeType
		subagents[i].ProviderID = session.ProviderID
		subagents[i].Model = session.Model
		subagents[i].WorkspacePath = session.WorkspacePath
	}
	return supervisorPlan{Subagents: subagents}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func runtimeSupervisorMetadata(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{"mode":"supervisor"}`)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return json.RawMessage(`{"mode":"supervisor"}`)
	}
	metadata["mode"] = "supervisor"
	out, err := json.Marshal(metadata)
	if err != nil {
		return json.RawMessage(`{"mode":"supervisor"}`)
	}
	return out
}

func truncateRuntimeSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "(no output)"
	}
	if len(summary) <= 500 {
		return summary
	}
	return summary[:500]
}

func (s *Service) ResumeSession(ctx context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, errors.New("runtime session id is required")
	}

	session, err := s.store.GetRuntimeSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime session: %w", err)
	}
	runtime, ok := s.runtimeFor(session.RuntimeType)
	if !ok || runtime == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}
	events, err := runtime.ResumeSession(ctx, firstNonEmpty(session.ExternalSessionID, session.ID))
	if err != nil {
		sanitized := sanitizeRuntimeErrorMessage(err)
		_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusFailed, sanitized)
		return nil, fmt.Errorf("failed to resume runtime session: %s", sanitized)
	}
	_ = s.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusRunning, "")
	return s.persistTerminalEvents(session.ID, events), nil
}

func (s *Service) SubmitApproval(ctx context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	if decision.ApprovalID == "" {
		return errors.New("runtime approval id is required")
	}
	if decision.UserID == "" {
		return errors.New("approval user id is required")
	}
	if decision.Decision != agentruntime.ApprovalDecisionAccept && decision.Decision != agentruntime.ApprovalDecisionDeny {
		return errors.New("approval decision must be accept or deny")
	}

	now := model.GetMillis()
	if _, err := s.store.ExpireRuntimeApprovals(now); err != nil {
		return fmt.Errorf("failed to expire runtime approvals: %w", err)
	}
	approval, err := s.store.GetRuntimeApproval(decision.ApprovalID)
	if err != nil {
		return fmt.Errorf("failed to get runtime approval: %w", err)
	}
	if approval.Status != agentruntime.ApprovalStatusPending {
		return fmt.Errorf("runtime approval is %s", approval.Status)
	}
	session, err := s.store.GetRuntimeSession(approval.RuntimeSessionID)
	if err != nil {
		return fmt.Errorf("failed to get runtime session: %w", err)
	}
	runtime, ok := s.runtimeFor(session.RuntimeType)
	if !ok || runtime == nil {
		return agentruntime.ErrRuntimeUnavailable
	}

	decision.SessionID = session.ID
	decision.SubagentRunID = approval.SubagentRunID
	if decision.ApprovalID == approval.ID && approval.ExternalApprovalID != "" {
		decision.ApprovalID = approval.ExternalApprovalID
	}
	if err := runtime.SubmitApproval(ctx, decision); err != nil {
		return fmt.Errorf("failed to submit runtime approval: %w", err)
	}

	status := agentruntime.ApprovalStatusDenied
	if decision.Decision == agentruntime.ApprovalDecisionAccept {
		status = agentruntime.ApprovalStatusAccepted
	}
	decision.ApprovalID = approval.ID
	return s.store.UpdateRuntimeApprovalDecision(approval.ID, decision, status)
}

func (s *Service) ExpireApprovals(ctx context.Context) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := s.store.ExpireRuntimeApprovals(model.GetMillis()); err != nil {
		return fmt.Errorf("failed to expire runtime approvals: %w", err)
	}
	return nil
}

func (s *Service) GetStatus(ctx context.Context, sessionID string) (agentruntime.RuntimeStatus, error) {
	if err := s.requireEnabled(); err != nil {
		return agentruntime.RuntimeStatus{}, err
	}
	if sessionID == "" {
		return agentruntime.RuntimeStatus{}, errors.New("runtime session id is required")
	}

	session, err := s.store.GetRuntimeSession(sessionID)
	if err != nil {
		return agentruntime.RuntimeStatus{}, fmt.Errorf("failed to get runtime session: %w", err)
	}
	runtime, ok := s.runtimeFor(session.RuntimeType)
	if !ok || runtime == nil {
		return agentruntime.RuntimeStatus{
			SessionID:     session.ID,
			Status:        session.Status,
			RuntimeType:   session.RuntimeType,
			ProviderID:    session.ProviderID,
			Model:         session.Model,
			WorkspacePath: session.WorkspacePath,
			LastError:     session.LastError,
		}, nil
	}
	status, err := runtime.GetStatus(ctx, session.ID)
	if err != nil {
		return agentruntime.RuntimeStatus{}, fmt.Errorf("failed to get runtime status: %w", err)
	}
	return status, nil
}

func (s *Service) persistTerminalEvents(sessionID string, events <-chan agentruntime.RuntimeEvent, requester ...string) <-chan agentruntime.RuntimeEvent {
	out := make(chan agentruntime.RuntimeEvent)
	go func() {
		defer close(out)
		startedAt := time.Now()
		var usage agentruntime.RuntimeUsage
		hasUsage := false
		lastExternalSessionID := ""
		for event := range events {
			event = normalizeRuntimeEventSessionID(sessionID, event)
			if event.ExternalSessionID != "" && event.ExternalSessionID != lastExternalSessionID {
				_ = s.store.UpdateRuntimeSessionExternalSessionID(sessionID, event.ExternalSessionID)
				lastExternalSessionID = event.ExternalSessionID
			}
			switch event.Type {
			case agentruntime.EventTypeStatus:
				if status, ok := runtimeSessionStatusFromEventText(event.Text); ok {
					_ = s.store.UpdateRuntimeSessionStatus(sessionID, status, "")
				}
			case agentruntime.EventTypeUsage:
				if eventUsage, ok := runtimeUsageFromPayload(event.Payload); ok {
					usage = addRuntimeUsage(usage, eventUsage)
					hasUsage = true
					_ = s.persistRuntimeUsage(sessionID, usage, startedAt)
				}
			case agentruntime.EventTypeCompleted, agentruntime.EventTypeSubagentCompleted:
				_ = s.store.UpdateRuntimeSessionStatus(sessionID, agentruntime.SessionStatusCompleted, "")
				_ = s.persistRuntimeUsage(sessionID, usage, startedAt)
			case agentruntime.EventTypeCancelled:
				_ = s.store.UpdateRuntimeSessionStatus(sessionID, agentruntime.SessionStatusCancelled, "")
				_ = s.persistRuntimeUsage(sessionID, usage, startedAt)
			case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
				lastError := ""
				if event.Err != nil {
					lastError = sanitizeRuntimeErrorMessage(event.Err)
					event.Err = errors.New(lastError)
				}
				_ = s.store.UpdateRuntimeSessionStatus(sessionID, agentruntime.SessionStatusFailed, lastError)
				_ = s.persistRuntimeUsage(sessionID, usage, startedAt)
			case agentruntime.EventTypeApprovalRequested:
				_ = s.createRuntimeApproval(sessionID, event, requester...)
				_ = s.store.UpdateRuntimeSessionStatus(sessionID, agentruntime.SessionStatusWaitingApproval, "")
			}
			if isTerminalEvent(event.Type) && !hasUsage {
				_ = s.persistRuntimeUsage(sessionID, usage, startedAt)
			}
			out <- event
		}
	}()
	return out
}

func sanitizeRuntimeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return llm.SanitizeProviderErrorMessage(err.Error())
}

func runtimeSessionStatusFromEventText(text string) (agentruntime.RuntimeSessionStatus, bool) {
	switch agentruntime.RuntimeSessionStatus(strings.TrimSpace(text)) {
	case agentruntime.SessionStatusRunning:
		return agentruntime.SessionStatusRunning, true
	case agentruntime.SessionStatusWaitingApproval:
		return agentruntime.SessionStatusWaitingApproval, true
	case agentruntime.SessionStatusCompleted:
		return agentruntime.SessionStatusCompleted, true
	case agentruntime.SessionStatusFailed:
		return agentruntime.SessionStatusFailed, true
	case agentruntime.SessionStatusCancelled:
		return agentruntime.SessionStatusCancelled, true
	default:
		return "", false
	}
}

func normalizeRuntimeEventSessionID(sessionID string, event agentruntime.RuntimeEvent) agentruntime.RuntimeEvent {
	if sessionID == "" {
		return event
	}
	if event.SessionID != "" && event.SessionID != sessionID && event.ExternalSessionID == "" {
		event.ExternalSessionID = event.SessionID
	}
	event.SessionID = sessionID
	return event
}

func (s *Service) persistRuntimeUsage(sessionID string, usage agentruntime.RuntimeUsage, startedAt time.Time) error {
	if sessionID == "" || s == nil || s.store == nil {
		return nil
	}
	session, err := s.store.GetRuntimeSession(sessionID)
	if err != nil {
		return err
	}
	usage.DurationMS = maxInt64(usage.DurationMS, time.Since(startedAt).Milliseconds())
	usage = s.estimateRuntimeUsageCost(*session, usage)
	metadata := map[string]any{}
	if len(session.Metadata) > 0 {
		_ = json.Unmarshal(session.Metadata, &metadata)
	}
	metadata["usage"] = usage
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return s.store.UpdateRuntimeSessionMetadata(sessionID, raw)
}

func runtimeUsageFromPayload(payload json.RawMessage) (agentruntime.RuntimeUsage, bool) {
	if len(payload) == 0 {
		return agentruntime.RuntimeUsage{}, false
	}
	var usage agentruntime.RuntimeUsage
	if err := json.Unmarshal(payload, &usage); err == nil && nonZeroRuntimeUsage(usage) {
		return usage, true
	}
	var wrapper struct {
		Usage agentruntime.RuntimeUsage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &wrapper); err == nil && nonZeroRuntimeUsage(wrapper.Usage) {
		return wrapper.Usage, true
	}
	return agentruntime.RuntimeUsage{}, false
}

func addRuntimeUsage(a, b agentruntime.RuntimeUsage) agentruntime.RuntimeUsage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CachedReadTokens += b.CachedReadTokens
	a.CachedWriteTokens += b.CachedWriteTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.DurationMS = maxInt64(a.DurationMS, b.DurationMS)
	a.Cost += b.Cost
	return a
}

type runtimeCostRate struct {
	InputPerMillion       float64
	CachedReadPerMillion  float64
	CachedWritePerMillion float64
	OutputPerMillion      float64
}

var openAIModelCostRates = map[string]runtimeCostRate{
	"chat-latest":          {InputPerMillion: 5.00, CachedReadPerMillion: 0.50, CachedWritePerMillion: 6.25, OutputPerMillion: 30.00},
	"gpt-5.3-codex":        {InputPerMillion: 3.50, CachedReadPerMillion: 0.35, CachedWritePerMillion: 4.375, OutputPerMillion: 28.00},
	"gpt-5.6-cyber":        {InputPerMillion: 12.50, CachedReadPerMillion: 1.25, CachedWritePerMillion: 15.625, OutputPerMillion: 75.00},
	"gpt-5.6-luna":         {InputPerMillion: 0.10, CachedReadPerMillion: 0.01, CachedWritePerMillion: 0.125, OutputPerMillion: 0.60},
	"gpt-5.6-sol":          {InputPerMillion: 2.50, CachedReadPerMillion: 0.25, CachedWritePerMillion: 3.125, OutputPerMillion: 15.00},
	"gpt-5.6-terra":        {InputPerMillion: 1.00, CachedReadPerMillion: 0.10, CachedWritePerMillion: 1.25, OutputPerMillion: 6.00},
	"daybreak-blue":        {InputPerMillion: 2.50, CachedReadPerMillion: 0.25, CachedWritePerMillion: 3.125, OutputPerMillion: 15.00},
	"daybreak-red":         {InputPerMillion: 12.50, CachedReadPerMillion: 1.25, CachedWritePerMillion: 15.625, OutputPerMillion: 75.00},
	"daybreak-blue-latest": {InputPerMillion: 2.50, CachedReadPerMillion: 0.25, CachedWritePerMillion: 3.125, OutputPerMillion: 15.00},
	"daybreak-red-latest":  {InputPerMillion: 12.50, CachedReadPerMillion: 1.25, CachedWritePerMillion: 15.625, OutputPerMillion: 75.00},
}

func (s *Service) estimateRuntimeUsageCost(session agentruntime.RuntimeSession, usage agentruntime.RuntimeUsage) agentruntime.RuntimeUsage {
	if usage.Cost > 0 || session.RuntimeType == agentruntime.RuntimeTypeLocal {
		return usage
	}
	rate, ok := runtimeCostRateForSession(session, s.costRatesSnapshot())
	if !ok {
		return usage
	}
	uncachedInput := usage.InputTokens - usage.CachedReadTokens - usage.CachedWriteTokens
	if uncachedInput < 0 {
		uncachedInput = 0
	}
	usage.Cost = costForTokens(uncachedInput, rate.InputPerMillion) +
		costForTokens(usage.CachedReadTokens, rate.CachedReadPerMillion) +
		costForTokens(usage.CachedWriteTokens, rate.CachedWritePerMillion) +
		costForTokens(usage.OutputTokens, rate.OutputPerMillion)
	return usage
}

func runtimeCostRateForSession(session agentruntime.RuntimeSession, customRates []agentruntime.RuntimeCostRate) (runtimeCostRate, bool) {
	if session.RuntimeType != agentruntime.RuntimeTypeCodex && session.RuntimeType != agentruntime.RuntimeTypeOpenAI {
		return runtimeCostRate{}, false
	}
	model := strings.ToLower(strings.TrimSpace(session.Model))
	if model == "" {
		return runtimeCostRate{}, false
	}
	if rate, ok := customRuntimeCostRate(session, customRates); ok {
		return rate, true
	}
	if rate, ok := openAIModelCostRates[model]; ok {
		return rate, true
	}
	if strings.HasPrefix(model, "daybreak-blue-") {
		return openAIModelCostRates["daybreak-blue"], true
	}
	if strings.HasPrefix(model, "daybreak-red-") {
		return openAIModelCostRates["daybreak-red"], true
	}
	return runtimeCostRate{}, false
}

func customRuntimeCostRate(session agentruntime.RuntimeSession, customRates []agentruntime.RuntimeCostRate) (runtimeCostRate, bool) {
	sessionProviderID := strings.ToLower(strings.TrimSpace(session.ProviderID))
	sessionModel := strings.ToLower(strings.TrimSpace(session.Model))
	for _, custom := range customRates {
		if custom.RuntimeType != "" && custom.RuntimeType != session.RuntimeType {
			continue
		}
		if custom.ProviderID != "" && strings.ToLower(strings.TrimSpace(custom.ProviderID)) != sessionProviderID {
			continue
		}
		if strings.ToLower(strings.TrimSpace(custom.Model)) != sessionModel {
			continue
		}
		return runtimeCostRate{
			InputPerMillion:       custom.InputPerMillion,
			CachedReadPerMillion:  custom.CachedReadPerMillion,
			CachedWritePerMillion: custom.CachedWritePerMillion,
			OutputPerMillion:      custom.OutputPerMillion,
		}, true
	}
	return runtimeCostRate{}, false
}

func normalizedRuntimeCostRates(rates []agentruntime.RuntimeCostRate) []agentruntime.RuntimeCostRate {
	out := make([]agentruntime.RuntimeCostRate, 0, len(rates))
	for _, rate := range rates {
		rate.ProviderID = strings.TrimSpace(rate.ProviderID)
		rate.Model = strings.TrimSpace(rate.Model)
		if rate.Model == "" {
			continue
		}
		out = append(out, rate)
	}
	return out
}

func costForTokens(tokens int64, perMillion float64) float64 {
	if tokens <= 0 || perMillion <= 0 {
		return 0
	}
	return float64(tokens) * perMillion / 1_000_000
}

func nonZeroRuntimeUsage(usage agentruntime.RuntimeUsage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CachedReadTokens != 0 ||
		usage.CachedWriteTokens != 0 ||
		usage.ReasoningTokens != 0 ||
		usage.DurationMS != 0 ||
		usage.Cost != 0
}

func isTerminalEvent(eventType agentruntime.RuntimeEventType) bool {
	switch eventType {
	case agentruntime.EventTypeCompleted, agentruntime.EventTypeSubagentCompleted, agentruntime.EventTypeCancelled, agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
		return true
	default:
		return false
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *Service) createRuntimeApproval(sessionID string, event agentruntime.RuntimeEvent, requester ...string) error {
	session, err := s.store.GetRuntimeSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get runtime session for approval: %w", err)
	}
	externalID := externalApprovalID(event)
	requestedBy := session.UserID
	if len(requester) > 0 && requester[0] != "" {
		requestedBy = requester[0]
	}
	return s.store.CreateRuntimeApproval(&agentruntime.RuntimeApproval{
		RuntimeSessionID:   sessionID,
		ExternalApprovalID: externalID,
		SubagentRunID:      event.SubagentRunID,
		RequestPayload:     normalizedApprovalPayload(event),
		Status:             agentruntime.ApprovalStatusPending,
		RequestedBy:        requestedBy,
		ExpiresAt:          model.GetMillis() + defaultRuntimeApprovalTTL.Milliseconds(),
	})
}

func externalApprovalID(event agentruntime.RuntimeEvent) string {
	if len(event.Payload) == 0 {
		return ""
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"approval_id", "approvalId", "id"} {
		if id := rawStringID(payload[key]); id != "" {
			return id
		}
	}
	return ""
}

func rawStringID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber.String()
	}
	return ""
}

func normalizedApprovalPayload(event agentruntime.RuntimeEvent) json.RawMessage {
	if len(event.Payload) > 0 {
		return event.Payload
	}
	payload, err := json.Marshal(map[string]string{
		"text": event.Text,
	})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return payload
}

func (s *Service) requireEnabled() error {
	if s == nil || s.cfg == nil || !s.cfg.EnableAgentRuntimeControlPlane() {
		return ErrDisabled
	}
	if s.store == nil {
		return errors.New("runtime control store is not configured")
	}
	return nil
}
