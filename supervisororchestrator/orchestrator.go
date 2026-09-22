// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package supervisororchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost/server/public/model"
)

type Store interface {
	CreateSupervisorRun(run *agentruntime.SupervisorRun) error
	UpdateSupervisorRunStatus(id, status, finalResultPostID string) error
	CreateSubagentRun(run *agentruntime.SubagentRun) error
	UpdateSubagentRunResult(id, status string, result json.RawMessage, summary string) error
}

type RuntimePolicyGuard func(runtimeType agentruntime.RuntimeType) error

type Orchestrator struct {
	store    Store
	runtimes map[agentruntime.RuntimeType]agentruntime.AgentRuntime
	guard    RuntimePolicyGuard
}

type Options struct {
	Store    Store
	Runtimes map[agentruntime.RuntimeType]agentruntime.AgentRuntime
	Guard    RuntimePolicyGuard
}

type StartRunRequest struct {
	RuntimeSessionID         string
	MattermostConversationID string
	RootTaskID               string
	SupervisorAgentID        string
	Objective                string
	CreatedBy                string
	Plan                     json.RawMessage
	Metadata                 json.RawMessage
}

type SubagentSpec struct {
	SupervisorRunID     string
	SubagentRunID       string
	ParentSubagentRunID string
	Role                string
	Title               string
	Prompt              string
	Session             agentruntime.RuntimeSession
	RuntimeType         agentruntime.RuntimeType
	ProviderID          string
	Model               string
	WorkspacePath       string
	Metadata            json.RawMessage
}

type SubagentResult struct {
	Run    agentruntime.SubagentRun
	Events <-chan agentruntime.RuntimeEvent
}

func New(opts Options) *Orchestrator {
	return &Orchestrator{
		store:    opts.Store,
		runtimes: opts.Runtimes,
		guard:    opts.Guard,
	}
}

func (o *Orchestrator) StartRun(req StartRunRequest) (*agentruntime.SupervisorRun, error) {
	if o == nil || o.store == nil {
		return nil, errors.New("supervisor orchestrator store is not configured")
	}
	if req.SupervisorAgentID == "" {
		return nil, errors.New("supervisor agent id is required")
	}
	if req.Objective == "" {
		return nil, errors.New("supervisor objective is required")
	}

	run := &agentruntime.SupervisorRun{
		RuntimeSessionID:         req.RuntimeSessionID,
		MattermostConversationID: req.MattermostConversationID,
		RootTaskID:               req.RootTaskID,
		SupervisorAgentID:        req.SupervisorAgentID,
		Status:                   agentruntime.RunStatusRunning,
		Objective:                req.Objective,
		Plan:                     req.Plan,
		CreatedBy:                req.CreatedBy,
		Metadata:                 req.Metadata,
	}
	if err := o.store.CreateSupervisorRun(run); err != nil {
		return nil, fmt.Errorf("failed to create supervisor run: %w", err)
	}
	return run, nil
}

func (o *Orchestrator) CompleteRun(runID, finalResultPostID string) error {
	if o == nil || o.store == nil {
		return errors.New("supervisor orchestrator store is not configured")
	}
	if runID == "" {
		return errors.New("supervisor run id is required")
	}
	return o.store.UpdateSupervisorRunStatus(runID, agentruntime.RunStatusCompleted, finalResultPostID)
}

func (o *Orchestrator) FailRun(runID string, _ error) error {
	if o == nil || o.store == nil {
		return errors.New("supervisor orchestrator store is not configured")
	}
	if runID == "" {
		return errors.New("supervisor run id is required")
	}
	return o.store.UpdateSupervisorRunStatus(runID, agentruntime.RunStatusFailed, "")
}

func (o *Orchestrator) StartSubagent(ctx context.Context, spec SubagentSpec) (*SubagentResult, error) {
	if o == nil || o.store == nil {
		return nil, errors.New("supervisor orchestrator store is not configured")
	}
	if spec.SupervisorRunID == "" {
		return nil, errors.New("supervisor run id is required")
	}
	if spec.Role == "" {
		return nil, errors.New("subagent role is required")
	}
	if spec.Prompt == "" {
		return nil, errors.New("subagent prompt is required")
	}

	runtimeType := spec.RuntimeType
	if runtimeType == "" {
		runtimeType = spec.Session.RuntimeType
	}
	if o.guard != nil {
		if err := o.guard(runtimeType); err != nil {
			return nil, err
		}
	}
	runtime := o.runtimes[runtimeType]
	if runtime == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}

	runID := spec.SubagentRunID
	if runID == "" {
		runID = model.NewId()
	}
	run := &agentruntime.SubagentRun{
		ID:                  runID,
		SupervisorRunID:     spec.SupervisorRunID,
		ParentSubagentRunID: spec.ParentSubagentRunID,
		Role:                spec.Role,
		Title:               spec.Title,
		Prompt:              spec.Prompt,
		RuntimeType:         runtimeType,
		ProviderID:          firstNonEmpty(spec.ProviderID, spec.Session.ProviderID),
		Model:               firstNonEmpty(spec.Model, spec.Session.Model),
		WorkspacePath:       firstNonEmpty(spec.WorkspacePath, spec.Session.WorkspacePath),
		Status:              agentruntime.RunStatusRunning,
		Metadata:            spec.Metadata,
	}
	childSession := spec.Session
	childSession.ID = run.ID
	childSession.RuntimeType = runtimeType
	childSession.ProviderID = run.ProviderID
	childSession.Model = run.Model
	childSession.WorkspacePath = run.WorkspacePath
	run.ExternalSessionID = childSession.ID
	if err := o.store.CreateSubagentRun(run); err != nil {
		return nil, fmt.Errorf("failed to create subagent run: %w", err)
	}

	events, err := runtime.StartSubagent(ctx, agentruntime.SubagentRequest{
		SupervisorRunID: spec.SupervisorRunID,
		SubagentRunID:   run.ID,
		ParentRunID:     spec.ParentSubagentRunID,
		Role:            spec.Role,
		Title:           spec.Title,
		Prompt:          spec.Prompt,
		Session:         childSession,
		Metadata:        spec.Metadata,
	})
	if err != nil {
		_ = o.store.UpdateSubagentRunResult(run.ID, agentruntime.RunStatusFailed, resultPayload(nil, err), "")
		return nil, fmt.Errorf("failed to start subagent runtime: %w", err)
	}

	return &SubagentResult{
		Run:    *run,
		Events: o.persistSubagentEvents(run.ID, events),
	}, nil
}

func (o *Orchestrator) persistSubagentEvents(runID string, events <-chan agentruntime.RuntimeEvent) <-chan agentruntime.RuntimeEvent {
	out := make(chan agentruntime.RuntimeEvent)
	go func() {
		defer close(out)
		var text strings.Builder
		for event := range events {
			if event.Type == agentruntime.EventTypeTextDelta {
				text.WriteString(event.Text)
			}
			switch event.Type {
			case agentruntime.EventTypeApprovalRequested:
				if event.SubagentRunID == "" {
					event.SubagentRunID = runID
				}
				_ = o.store.UpdateSubagentRunResult(runID, agentruntime.RunStatusWaitingApproval, resultPayload(event.Payload, nil), truncateSummary(text.String()))
			case agentruntime.EventTypeCompleted, agentruntime.EventTypeSubagentCompleted:
				_ = o.store.UpdateSubagentRunResult(runID, agentruntime.RunStatusCompleted, resultPayload(event.Payload, nil), truncateSummary(text.String()))
			case agentruntime.EventTypeCancelled:
				_ = o.store.UpdateSubagentRunResult(runID, agentruntime.RunStatusCancelled, resultPayload(event.Payload, nil), truncateSummary(text.String()))
			case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
				_ = o.store.UpdateSubagentRunResult(runID, agentruntime.RunStatusFailed, resultPayload(event.Payload, event.Err), truncateSummary(text.String()))
			}
			out <- event
		}
	}()
	return out
}

func resultPayload(payload json.RawMessage, err error) json.RawMessage {
	if len(payload) > 0 && err == nil {
		return payload
	}
	result := map[string]string{}
	if len(payload) > 0 {
		result["payload"] = string(payload)
	}
	if err != nil {
		result["error"] = err.Error()
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func truncateSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if len(summary) <= 500 {
		return summary
	}
	return summary[:500]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
