// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package llmruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/toolrunner"
)

type Runtime struct {
	llm         llm.LanguageModel
	providers   map[string]llm.LanguageModel
	runtimeType agentruntime.RuntimeType
	providerID  string

	mu     sync.Mutex
	active map[string]context.CancelFunc
	status map[string]agentruntime.RuntimeStatus
}

type Options struct {
	LLM         llm.LanguageModel
	Providers   map[string]llm.LanguageModel
	RuntimeType agentruntime.RuntimeType
	ProviderID  string
}

func New(opts Options) *Runtime {
	runtimeType := opts.RuntimeType
	if runtimeType == "" {
		runtimeType = agentruntime.RuntimeTypeLocal
	}

	return &Runtime{
		llm:         opts.LLM,
		providers:   cloneProviders(opts.Providers),
		runtimeType: runtimeType,
		providerID:  opts.ProviderID,
		active:      map[string]context.CancelFunc{},
		status:      map[string]agentruntime.RuntimeStatus{},
	}
}

func (r *Runtime) StartTurn(ctx context.Context, req agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	model := r.selectLLM(req.Session.ProviderID)
	if r == nil || model == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}
	if req.Session.ID == "" {
		return nil, errors.New("runtime session id is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	out := make(chan agentruntime.RuntimeEvent)
	r.setActive(req.Session, cancel, agentruntime.SessionStatusRunning, "")

	completionRequest := llm.CompletionRequest{
		Posts: []llm.Post{
			{
				Role:    llm.PostRoleUser,
				Message: req.Prompt,
			},
		},
		Context:   req.Context,
		Operation: "agent_runtime",
	}
	var result *llm.TextStreamResult
	var err error
	if req.Context != nil && req.Context.Tools != nil {
		shouldExecute := req.ShouldExecuteTool
		if shouldExecute == nil {
			shouldExecute = func(llm.ToolCall) bool { return false }
		}
		runResult, runErr := toolrunner.New(model).Run(ctx, completionRequest, shouldExecute, nil, llm.WithModel(req.Session.Model))
		if runErr == nil {
			result = runResult.Stream
		}
		err = runErr
	} else {
		result, err = model.ChatCompletion(ctx, completionRequest, llm.WithModel(req.Session.Model))
	}
	if err != nil {
		cancel()
		r.clearActive(req.Session.ID, agentruntime.SessionStatusFailed, sanitizeLocalRuntimeError(err))
		return nil, fmt.Errorf("failed to start local runtime turn: %w", err)
	}

	go r.forwardEvents(ctx, req.Session, result, out)
	return out, nil
}

func (r *Runtime) selectLLM(providerID string) llm.LanguageModel {
	if r == nil {
		return nil
	}
	if providerID != "" {
		if r.providers != nil {
			if model := r.providers[providerID]; model != nil {
				return model
			}
		}
		if providerID == r.providerID && r.llm != nil {
			return r.llm
		}
		return nil
	}
	if r.llm != nil {
		return r.llm
	}
	if len(r.providers) == 1 {
		for _, model := range r.providers {
			return model
		}
	}
	return nil
}

func cloneProviders(providers map[string]llm.LanguageModel) map[string]llm.LanguageModel {
	if len(providers) == 0 {
		return nil
	}
	clone := make(map[string]llm.LanguageModel, len(providers))
	for id, model := range providers {
		if id != "" && model != nil {
			clone[id] = model
		}
	}
	return clone
}

func (r *Runtime) StartSubagent(ctx context.Context, req agentruntime.SubagentRequest) (<-chan agentruntime.RuntimeEvent, error) {
	events, err := r.StartTurn(ctx, agentruntime.RuntimeTurnRequest{
		Session:  req.Session,
		Prompt:   req.Prompt,
		Metadata: req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	out := make(chan agentruntime.RuntimeEvent)
	go func() {
		defer close(out)
		out <- agentruntime.RuntimeEvent{
			Type:          agentruntime.EventTypeSubagentStarted,
			SessionID:     req.Session.ID,
			SubagentRunID: req.SubagentRunID,
		}
		for event := range events {
			event.SubagentRunID = req.SubagentRunID
			if event.Type == agentruntime.EventTypeCompleted {
				event.Type = agentruntime.EventTypeSubagentCompleted
			}
			if event.Type == agentruntime.EventTypeError {
				event.Type = agentruntime.EventTypeSubagentFailed
			}
			out <- event
		}
	}()
	return out, nil
}

func (r *Runtime) StopTurn(_ context.Context, sessionID string) error {
	r.mu.Lock()
	cancel, ok := r.active[sessionID]
	r.mu.Unlock()
	if !ok {
		return agentruntime.ErrSessionNotFound
	}
	cancel()
	r.clearActive(sessionID, agentruntime.SessionStatusCancelled, "")
	return nil
}

func (r *Runtime) ResumeSession(_ context.Context, _ string) (<-chan agentruntime.RuntimeEvent, error) {
	return nil, errors.New("llm runtime does not support resume")
}

func (r *Runtime) GetStatus(_ context.Context, sessionID string) (agentruntime.RuntimeStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.status[sessionID]
	if !ok {
		return agentruntime.RuntimeStatus{}, agentruntime.ErrSessionNotFound
	}
	return status, nil
}

func (r *Runtime) ListSessions(_ context.Context, filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sessions := []agentruntime.RuntimeSession{}
	for _, status := range r.status {
		if filter.Status != "" && status.Status != filter.Status {
			continue
		}
		sessions = append(sessions, agentruntime.RuntimeSession{
			ID:          status.SessionID,
			RuntimeType: status.RuntimeType,
			ProviderID:  status.ProviderID,
			Model:       status.Model,
			Status:      status.Status,
			LastError:   status.LastError,
		})
		if filter.Limit > 0 && len(sessions) >= filter.Limit {
			break
		}
	}
	return sessions, nil
}

func (r *Runtime) SubmitApproval(context.Context, agentruntime.RuntimeApprovalDecision) error {
	return errors.New("llm runtime does not support approvals")
}

func (r *Runtime) forwardEvents(ctx context.Context, session agentruntime.RuntimeSession, result *llm.TextStreamResult, out chan<- agentruntime.RuntimeEvent) {
	defer close(out)

	finalStatus := agentruntime.SessionStatusCompleted
	lastError := ""
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.active, session.ID)
		r.status[session.ID] = runtimeStatus(session, finalStatus, lastError)
	}()

	for {
		select {
		case <-ctx.Done():
			finalStatus = agentruntime.SessionStatusCancelled
			out <- agentruntime.RuntimeEvent{
				Type:      agentruntime.EventTypeCancelled,
				SessionID: session.ID,
				Err:       ctx.Err(),
			}
			return
		case event, ok := <-result.Stream:
			if !ok {
				out <- agentruntime.RuntimeEvent{
					Type:      agentruntime.EventTypeCompleted,
					SessionID: session.ID,
				}
				return
			}
			runtimeEvent, emit := convertEvent(session.ID, event)
			if runtimeEvent.Type == agentruntime.EventTypeError {
				finalStatus = agentruntime.SessionStatusFailed
				if runtimeEvent.Err != nil {
					lastError = sanitizeLocalRuntimeError(runtimeEvent.Err)
				}
			}
			if emit {
				out <- runtimeEvent
			}
			if runtimeEvent.Type == agentruntime.EventTypeCompleted {
				return
			}
		}
	}
}

func convertEvent(sessionID string, event llm.TextStreamEvent) (agentruntime.RuntimeEvent, bool) {
	switch event.Type {
	case llm.EventTypeText:
		text, _ := event.Value.(string)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: sessionID, Text: text}, true
	case llm.EventTypeReasoning:
		text, _ := event.Value.(string)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeReasoningDelta, SessionID: sessionID, Text: text}, true
	case llm.EventTypeToolCalls:
		payload, _ := json.Marshal(event.Value)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallRequested, SessionID: sessionID, Payload: payload}, true
	case llm.EventTypeFiles:
		payload, _ := json.Marshal(event.Value)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeFileCreated, SessionID: sessionID, Payload: payload}, true
	case llm.EventTypeError:
		if err, ok := event.Value.(error); ok {
			return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, Err: err}, true
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, Err: fmt.Errorf("%v", event.Value)}, true
	case llm.EventTypeEnd:
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: sessionID}, true
	case llm.EventTypeUsage:
		payload, _ := json.Marshal(event.Value)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeUsage, SessionID: sessionID, Payload: payload}, true
	case llm.EventTypeAnnotations, llm.EventTypeReasoningEnd, llm.EventTypeServerToolUse:
		payload, _ := json.Marshal(event.Value)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, Payload: payload}, true
	default:
		return agentruntime.RuntimeEvent{}, false
	}
}

func sanitizeLocalRuntimeError(err error) string {
	if err == nil {
		return ""
	}
	return llm.SanitizeProviderErrorMessage(err.Error())
}

func (r *Runtime) setActive(session agentruntime.RuntimeSession, cancel context.CancelFunc, status agentruntime.RuntimeSessionStatus, lastError string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[session.ID] = cancel
	r.status[session.ID] = runtimeStatus(session, status, lastError)
}

func (r *Runtime) clearActive(sessionID string, status agentruntime.RuntimeSessionStatus, lastError string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, sessionID)
	existing := r.status[sessionID]
	existing.Status = status
	existing.LastError = lastError
	r.status[sessionID] = existing
}

func runtimeStatus(session agentruntime.RuntimeSession, status agentruntime.RuntimeSessionStatus, lastError string) agentruntime.RuntimeStatus {
	return agentruntime.RuntimeStatus{
		SessionID:     session.ID,
		Status:        status,
		RuntimeType:   session.RuntimeType,
		ProviderID:    session.ProviderID,
		Model:         session.Model,
		WorkspacePath: session.WorkspacePath,
		LastError:     lastError,
	}
}
