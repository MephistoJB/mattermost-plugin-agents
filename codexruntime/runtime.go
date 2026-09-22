// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package codexruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
)

type Runtime struct {
	commandPath string
	extraArgs   []string
	env         []string

	mu     sync.Mutex
	active map[string]context.CancelFunc
	status map[string]agentruntime.RuntimeStatus
}

type Options struct {
	CommandPath string
	ExtraArgs   []string
	Env         []string
}

func New(opts Options) *Runtime {
	commandPath := opts.CommandPath
	if commandPath == "" {
		commandPath = "codex"
	}
	return &Runtime{
		commandPath: commandPath,
		extraArgs:   append([]string{}, opts.ExtraArgs...),
		env:         append([]string{}, opts.Env...),
		active:      map[string]context.CancelFunc{},
		status:      map[string]agentruntime.RuntimeStatus{},
	}
}

func (r *Runtime) StartTurn(ctx context.Context, req agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	if req.Session.ID == "" {
		return nil, errors.New("runtime session id is required")
	}
	args := []string{"exec", "--json"}
	args = append(args, r.commonArgs(req.Session)...)
	args = append(args, r.extraArgs...)
	args = append(args, promptWithAttachments(req.Prompt, req.Attachments))
	return r.startCommand(ctx, req.Session, args, false)
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
			switch event.Type {
			case agentruntime.EventTypeCompleted:
				event.Type = agentruntime.EventTypeSubagentCompleted
			case agentruntime.EventTypeError:
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
	r.setStatus(sessionID, func(status agentruntime.RuntimeStatus) agentruntime.RuntimeStatus {
		status.Status = agentruntime.SessionStatusCancelled
		return status
	})
	return nil
}

func (r *Runtime) ResumeSession(ctx context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error) {
	if sessionID == "" {
		return nil, errors.New("runtime session id is required")
	}
	r.mu.Lock()
	status, ok := r.status[sessionID]
	r.mu.Unlock()
	if !ok {
		status = agentruntime.RuntimeStatus{
			SessionID:   sessionID,
			RuntimeType: agentruntime.RuntimeTypeCodex,
			ProviderID:  "codex",
		}
	}

	args := []string{"exec", "resume", "--json"}
	args = append(args, r.commonArgs(agentruntime.RuntimeSession{
		ID:            sessionID,
		RuntimeType:   agentruntime.RuntimeTypeCodex,
		ProviderID:    status.ProviderID,
		Model:         status.Model,
		WorkspacePath: status.WorkspacePath,
	})...)
	args = append(args, r.extraArgs...)
	args = append(args, sessionID)
	return r.startCommand(ctx, agentruntime.RuntimeSession{
		ID:            sessionID,
		RuntimeType:   agentruntime.RuntimeTypeCodex,
		ProviderID:    status.ProviderID,
		Model:         status.Model,
		WorkspacePath: status.WorkspacePath,
	}, args, true)
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
			ID:            status.SessionID,
			RuntimeType:   status.RuntimeType,
			ProviderID:    status.ProviderID,
			Model:         status.Model,
			WorkspacePath: status.WorkspacePath,
			Status:        status.Status,
			LastError:     status.LastError,
		})
		if filter.Limit > 0 && len(sessions) >= filter.Limit {
			break
		}
	}
	return sessions, nil
}

func (r *Runtime) SubmitApproval(context.Context, agentruntime.RuntimeApprovalDecision) error {
	return agentruntime.ErrApprovalUnsupported
}

func (r *Runtime) commonArgs(session agentruntime.RuntimeSession) []string {
	args := []string{}
	if session.Model != "" {
		args = append(args, "-m", session.Model)
	}
	if session.WorkspacePath != "" {
		args = append(args, "-C", session.WorkspacePath)
	}
	return args
}

func (r *Runtime) setStatus(sessionID string, update func(agentruntime.RuntimeStatus) agentruntime.RuntimeStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.status[sessionID]
	if status.SessionID == "" {
		status.SessionID = sessionID
		status.RuntimeType = agentruntime.RuntimeTypeCodex
		status.ProviderID = "codex"
	}
	r.status[sessionID] = update(status)
}

func (r *Runtime) startCommand(ctx context.Context, session agentruntime.RuntimeSession, args []string, resumed bool) (<-chan agentruntime.RuntimeEvent, error) {
	if r == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, r.commandPath, args...) // #nosec G204 -- command path and args are plugin configuration, not shell-expanded.
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open codex stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open codex stderr: %w", err)
	}
	if session.WorkspacePath != "" {
		cmd.Dir = session.WorkspacePath
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start codex: %w", err)
	}

	r.mu.Lock()
	r.active[session.ID] = cancel
	r.status[session.ID] = runtimeStatus(session, agentruntime.SessionStatusRunning, "")
	r.mu.Unlock()

	out := make(chan agentruntime.RuntimeEvent)
	go r.forwardCommand(ctx, session, stdout, stderr, cmd, out, resumed)
	return out, nil
}

func (r *Runtime) forwardCommand(ctx context.Context, session agentruntime.RuntimeSession, stdout io.Reader, stderr io.Reader, cmd *exec.Cmd, out chan<- agentruntime.RuntimeEvent, resumed bool) {
	defer close(out)

	errs := make(chan string, 1)
	go func() {
		payload, _ := io.ReadAll(io.LimitReader(stderr, 1<<20))
		errs <- strings.TrimSpace(llm.SanitizeProviderErrorMessage(string(payload)))
	}()

	finalStatus := agentruntime.SessionStatusCompleted
	lastError := ""
	emittedTerminal := false
	if resumed {
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: session.ID, Text: "resumed"}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		event, emit := parseJSONLine(session.ID, scanner.Bytes())
		if !emit {
			continue
		}
		if event.Type == agentruntime.EventTypeError {
			finalStatus = agentruntime.SessionStatusFailed
			if event.Err != nil {
				lastError = event.Err.Error()
			}
		}
		if isTerminal(event.Type) {
			emittedTerminal = true
		}
		out <- event
	}
	if err := scanner.Err(); err != nil {
		finalStatus = agentruntime.SessionStatusFailed
		lastError = err.Error()
		emittedTerminal = true
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: session.ID, Err: err}
	}

	waitErr := cmd.Wait()
	stderrText := <-errs
	if waitErr != nil {
		finalStatus = agentruntime.SessionStatusFailed
		lastError = strings.TrimSpace(stderrText)
		if lastError == "" {
			lastError = llm.SanitizeProviderErrorMessage(waitErr.Error())
		}
		if ctx.Err() != nil {
			finalStatus = agentruntime.SessionStatusCancelled
			if !emittedTerminal {
				out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCancelled, SessionID: session.ID, Err: ctx.Err()}
			}
		} else {
			if !emittedTerminal {
				out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: session.ID, Text: stderrText, Err: errors.New(lastError)}
			}
		}
	} else if !emittedTerminal {
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: session.ID}
	}

	r.mu.Lock()
	delete(r.active, session.ID)
	r.status[session.ID] = runtimeStatus(session, finalStatus, lastError)
	r.mu.Unlock()
}

func isTerminal(eventType agentruntime.RuntimeEventType) bool {
	return eventType == agentruntime.EventTypeCompleted ||
		eventType == agentruntime.EventTypeCancelled ||
		eventType == agentruntime.EventTypeError ||
		eventType == agentruntime.EventTypeSubagentCompleted ||
		eventType == agentruntime.EventTypeSubagentFailed
}

type codexJSONEvent struct {
	Type      string          `json:"type"`
	Message   string          `json:"message"`
	Msg       string          `json:"msg"`
	Text      string          `json:"text"`
	Delta     string          `json:"delta"`
	Summary   string          `json:"summary"`
	Error     json.RawMessage `json:"error"`
	Event     json.RawMessage `json:"event"`
	Item      json.RawMessage `json:"item"`
	SessionID string          `json:"session_id"`
	ID        string          `json:"id"`
	Path      string          `json:"path"`
	File      string          `json:"file"`
	FileName  string          `json:"fileName"`
}

func parseJSONLine(sessionID string, line []byte) (agentruntime.RuntimeEvent, bool) {
	var event codexJSONEvent
	if err := json.Unmarshal(line, &event); err != nil {
		text := strings.TrimSpace(string(line))
		if text == "" {
			return agentruntime.RuntimeEvent{}, false
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: sessionID, Text: text}, true
	}

	eventType := strings.ToLower(event.Type)
	text := firstNonEmpty(event.Text, event.Delta, event.Message, event.Msg, event.Summary)
	payload := json.RawMessage(append([]byte{}, line...))
	externalSessionID := ""
	if event.SessionID != "" && event.SessionID != sessionID {
		externalSessionID = event.SessionID
	}

	switch {
	case strings.Contains(eventType, "file") && (strings.Contains(eventType, "create") || strings.Contains(eventType, "change") || strings.Contains(eventType, "write")):
		if filePayload := runtimeFilePayloadFromCodexEvent(event); len(filePayload) > 0 {
			return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeFileCreated, SessionID: sessionID, ExternalSessionID: externalSessionID, Payload: filePayload}, true
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "usage"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeUsage, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "reason"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeReasoningDelta, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "tool") && strings.Contains(eventType, "start"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallStarted, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "tool") && (strings.Contains(eventType, "complete") || strings.Contains(eventType, "result")):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallCompleted, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "tool"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallRequested, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "approval") && (strings.Contains(eventType, "resolved") || strings.Contains(eventType, "decision") || strings.Contains(eventType, "approved") || strings.Contains(eventType, "denied") || strings.Contains(eventType, "complete")):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeApprovalResolved, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "approval"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeApprovalRequested, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "error"):
		if text == "" {
			text = string(event.Error)
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload, Err: errors.New(text)}, true
	case strings.Contains(eventType, "complete") || strings.Contains(eventType, "done") || eventType == "end":
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case strings.Contains(eventType, "cancel"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCancelled, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	case text != "":
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: sessionID, ExternalSessionID: externalSessionID, Text: text, Payload: payload}, true
	default:
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: externalSessionID, Payload: payload}, true
	}
}

func promptWithAttachments(prompt string, attachments []agentruntime.RuntimeAttachment) string {
	paths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.LocalPath) != "" {
			paths = append(paths, attachment.LocalPath)
		}
	}
	if len(paths) == 0 {
		return prompt
	}
	var builder strings.Builder
	builder.WriteString(prompt)
	builder.WriteString("\n\nRuntime attachments imported into the workspace:\n")
	for _, path := range paths {
		builder.WriteString("- ")
		builder.WriteString(path)
		builder.WriteString("\n")
	}
	return builder.String()
}

func runtimeFilePayloadFromCodexEvent(event codexJSONEvent) json.RawMessage {
	var payload struct {
		Path     string `json:"path"`
		File     string `json:"file"`
		FileName string `json:"fileName"`
		Item     struct {
			Path     string `json:"path"`
			File     string `json:"file"`
			FileName string `json:"fileName"`
		} `json:"item"`
	}
	_ = json.Unmarshal(event.Item, &payload.Item)
	path := firstNonEmpty(payload.Path, payload.File, payload.Item.Path, payload.Item.File)
	fileName := firstNonEmpty(payload.FileName, payload.Item.FileName)
	path = firstNonEmpty(path, event.Path, event.File)
	fileName = firstNonEmpty(fileName, event.FileName)
	if path == "" && fileName == "" {
		return nil
	}
	out, err := json.Marshal(agentruntime.RuntimeFileCreatedPayload{Path: path, FileName: fileName})
	if err != nil {
		return nil
	}
	return out
}

func runtimeStatus(session agentruntime.RuntimeSession, status agentruntime.RuntimeSessionStatus, lastError string) agentruntime.RuntimeStatus {
	return agentruntime.RuntimeStatus{
		SessionID:     session.ID,
		Status:        status,
		RuntimeType:   agentruntime.RuntimeTypeCodex,
		ProviderID:    firstNonEmpty(session.ProviderID, "codex"),
		Model:         session.Model,
		WorkspacePath: session.WorkspacePath,
		LastError:     lastError,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
