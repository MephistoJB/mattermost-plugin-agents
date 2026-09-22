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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
)

type AppServerRuntime struct {
	commandPath string
	extraArgs   []string
	env         []string

	mu                  sync.Mutex
	active              map[string]*appServerSession
	status              map[string]agentruntime.RuntimeStatus
	approvals           map[string]*appServerApproval
	streamedMessageText map[string]string
}

type appServerSession struct {
	sessionID string
	threadID  string
	stdin     io.WriteCloser
	cancel    context.CancelFunc
	writeMu   sync.Mutex
}

type appServerApproval struct {
	sessionID string
	requestID any
	method    string
}

func NewAppServer(opts Options) *AppServerRuntime {
	commandPath := opts.CommandPath
	if commandPath == "" {
		commandPath = "codex"
	}
	return &AppServerRuntime{
		commandPath:         commandPath,
		extraArgs:           append([]string{}, opts.ExtraArgs...),
		env:                 append([]string{}, opts.Env...),
		active:              map[string]*appServerSession{},
		status:              map[string]agentruntime.RuntimeStatus{},
		approvals:           map[string]*appServerApproval{},
		streamedMessageText: map[string]string{},
	}
}

func (r *AppServerRuntime) StartTurn(ctx context.Context, req agentruntime.RuntimeTurnRequest) (<-chan agentruntime.RuntimeEvent, error) {
	if req.Session.ID == "" {
		return nil, errors.New("runtime session id is required")
	}
	return r.startAppServerTurn(ctx, req.Session, promptWithAttachments(req.Prompt, req.Attachments), "", false)
}

func (r *AppServerRuntime) StartSubagent(ctx context.Context, req agentruntime.SubagentRequest) (<-chan agentruntime.RuntimeEvent, error) {
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

func (r *AppServerRuntime) StopTurn(_ context.Context, sessionID string) error {
	r.mu.Lock()
	session, ok := r.active[sessionID]
	r.mu.Unlock()
	if !ok {
		return agentruntime.ErrSessionNotFound
	}
	session.cancel()
	r.setStatus(sessionID, func(status agentruntime.RuntimeStatus) agentruntime.RuntimeStatus {
		status.Status = agentruntime.SessionStatusCancelled
		return status
	})
	return nil
}

func (r *AppServerRuntime) ResumeSession(ctx context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error) {
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
			ProviderID:  "codex-app-server",
		}
	}
	return r.startAppServerTurn(ctx, agentruntime.RuntimeSession{
		ID:            sessionID,
		RuntimeType:   agentruntime.RuntimeTypeCodex,
		ProviderID:    status.ProviderID,
		Model:         status.Model,
		WorkspacePath: status.WorkspacePath,
	}, "", sessionID, true)
}

func (r *AppServerRuntime) GetStatus(_ context.Context, sessionID string) (agentruntime.RuntimeStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.status[sessionID]
	if !ok {
		return agentruntime.RuntimeStatus{}, agentruntime.ErrSessionNotFound
	}
	return status, nil
}

func (r *AppServerRuntime) ListSessions(_ context.Context, filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error) {
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

func (r *AppServerRuntime) SubmitApproval(_ context.Context, decision agentruntime.RuntimeApprovalDecision) error {
	if decision.ApprovalID == "" {
		return errors.New("runtime approval id is required")
	}
	r.mu.Lock()
	approval, ok := r.approvals[decision.ApprovalID]
	session := (*appServerSession)(nil)
	if ok {
		session = r.active[approval.sessionID]
	}
	r.mu.Unlock()
	if !ok || session == nil {
		return agentruntime.ErrApprovalUnsupported
	}

	result, err := appServerApprovalResult(approval.method, decision)
	if err != nil {
		return err
	}
	return session.send(map[string]any{
		"id":     approval.requestID,
		"result": result,
	})
}

func (r *AppServerRuntime) startAppServerTurn(ctx context.Context, session agentruntime.RuntimeSession, prompt string, externalThreadID string, resumed bool) (<-chan agentruntime.RuntimeEvent, error) {
	if r == nil {
		return nil, agentruntime.ErrRuntimeUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	args := []string{"app-server", "--stdio"}
	args = append(args, r.extraArgs...)
	cmd := exec.CommandContext(ctx, r.commandPath, args...) // #nosec G204 -- command path and args are plugin configuration, not shell-expanded.
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open codex app-server stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open codex app-server stderr: %w", err)
	}
	if session.WorkspacePath != "" {
		cmd.Dir = session.WorkspacePath
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start codex app-server: %w", err)
	}

	r.mu.Lock()
	r.active[session.ID] = &appServerSession{sessionID: session.ID, threadID: externalThreadID, stdin: stdin, cancel: cancel}
	r.status[session.ID] = appServerRuntimeStatus(session, agentruntime.SessionStatusRunning, "")
	r.mu.Unlock()

	out := make(chan agentruntime.RuntimeEvent)
	go r.forwardAppServer(ctx, session, prompt, externalThreadID, stdout, stderr, cmd, out, resumed)
	if err := r.startAppServerProtocol(session, prompt, externalThreadID); err != nil {
		cancel()
		return nil, err
	}
	return out, nil
}

func (r *AppServerRuntime) startAppServerProtocol(session agentruntime.RuntimeSession, prompt string, externalThreadID string) error {
	if err := r.sendToSession(session.ID, map[string]any{
		"id":     1,
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "mattermost_agents",
				"title":   "Mattermost Agents",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		},
	}); err != nil {
		return err
	}
	if err := r.sendToSession(session.ID, map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	if externalThreadID != "" {
		params := appServerThreadParams(session)
		params["threadId"] = externalThreadID
		return r.sendToSession(session.ID, map[string]any{
			"id":     2,
			"method": "thread/resume",
			"params": params,
		})
	}
	_ = prompt
	return r.sendToSession(session.ID, map[string]any{
		"id":     2,
		"method": "thread/start",
		"params": appServerThreadParams(session),
	})
}

func (r *AppServerRuntime) sendToSession(sessionID string, message map[string]any) error {
	r.mu.Lock()
	session := r.active[sessionID]
	r.mu.Unlock()
	if session == nil {
		return agentruntime.ErrSessionNotFound
	}
	return session.send(message)
}

func (s *appServerSession) send(message map[string]any) error {
	if s == nil {
		return agentruntime.ErrSessionNotFound
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return appServerSend(s.stdin, message)
}

func appServerSend(stdin io.Writer, message map[string]any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = stdin.Write(payload)
	return err
}

func (r *AppServerRuntime) forwardAppServer(ctx context.Context, session agentruntime.RuntimeSession, prompt string, externalThreadID string, stdout io.Reader, stderr io.Reader, cmd *exec.Cmd, out chan<- agentruntime.RuntimeEvent, resumed bool) {
	defer close(out)

	errs := make(chan string, 1)
	go func() {
		payload, _ := io.ReadAll(io.LimitReader(stderr, 1<<20))
		errs <- strings.TrimSpace(llm.SanitizeProviderErrorMessage(string(payload)))
	}()

	finalStatus := agentruntime.SessionStatusCompleted
	lastError := ""
	emittedTerminal := false
	var shutdownTimer *time.Timer
	if resumed {
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: session.ID, Text: "resumed", ExternalSessionID: externalThreadID}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		event, emit := r.handleAppServerMessage(session.ID, prompt, scanner.Bytes())
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
		if emittedTerminal {
			// Give Codex a brief chance to flush its thread state after the
			// terminal event, then stop this turn's app-server process.
			r.mu.Lock()
			active := r.active[session.ID]
			r.mu.Unlock()
			if active != nil {
				_ = active.stdin.Close()
			}
			shutdownTimer = time.AfterFunc(3*time.Second, func() { _ = cmd.Process.Kill() })
			break
		}
	}
	if err := scanner.Err(); err != nil {
		finalStatus = agentruntime.SessionStatusFailed
		lastError = err.Error()
		emittedTerminal = true
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: session.ID, Err: err}
	}

	waitErr := cmd.Wait()
	if shutdownTimer != nil {
		shutdownTimer.Stop()
	}
	stderrText := <-errs
	if waitErr != nil && !emittedTerminal {
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
		} else if !emittedTerminal {
			out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: session.ID, Text: stderrText, Err: errors.New(lastError)}
		}
	} else if !emittedTerminal {
		out <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: session.ID}
	}

	r.mu.Lock()
	delete(r.active, session.ID)
	delete(r.streamedMessageText, session.ID)
	for id, approval := range r.approvals {
		if approval.sessionID == session.ID {
			delete(r.approvals, id)
		}
	}
	r.status[session.ID] = appServerRuntimeStatus(session, finalStatus, lastError)
	r.mu.Unlock()
}

func (r *AppServerRuntime) handleAppServerMessage(sessionID string, prompt string, line []byte) (agentruntime.RuntimeEvent, bool) {
	var msg appServerMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		text := strings.TrimSpace(string(line))
		if text == "" {
			return agentruntime.RuntimeEvent{}, false
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: sessionID, Text: text}, true
	}
	payload := json.RawMessage(append([]byte{}, line...))
	if msg.Error != nil {
		text := msg.Error.Message
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, Text: text, Payload: payload, Err: errors.New(text)}, true
	}

	if msg.ID != nil && msg.Result != nil {
		if idString(msg.ID) == "2" {
			threadID := appServerThreadID(msg.Result)
			if threadID != "" {
				r.mu.Lock()
				if session := r.active[sessionID]; session != nil {
					session.threadID = threadID
				}
				r.mu.Unlock()
				if prompt != "" {
					_ = r.sendToSession(sessionID, map[string]any{
						"id":     3,
						"method": "turn/start",
						"params": map[string]any{
							"threadId": threadID,
							"input": []map[string]any{
								{"type": "text", "text": prompt},
							},
						},
					})
				}
				return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: threadID, Payload: payload}, true
			}
		}
		return agentruntime.RuntimeEvent{}, false
	}

	if msg.ID != nil && appServerApprovalRequest(msg.Method) {
		approvalID := appServerApprovalID(msg)
		r.mu.Lock()
		r.approvals[approvalID] = &appServerApproval{sessionID: sessionID, requestID: msg.ID, method: msg.Method}
		r.mu.Unlock()
		approvalPayload := appServerApprovalPayload(line, approvalID)
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeApprovalRequested, SessionID: sessionID, ExternalSessionID: appServerThreadIDFromParams(msg.Params), Payload: approvalPayload}, true
	}

	switch msg.Method {
	case "item/agentMessage/delta", "item/reasoning/textDelta", "item/reasoning/summaryTextDelta", "item/plan/delta":
		text := appServerText(msg.Params)
		if text == "" {
			return agentruntime.RuntimeEvent{}, false
		}
		if msg.Method == "item/agentMessage/delta" {
			r.mu.Lock()
			r.streamedMessageText[sessionID] += text
			r.mu.Unlock()
		}
		eventType := agentruntime.EventTypeTextDelta
		if strings.Contains(strings.ToLower(msg.Method), "reasoning") {
			eventType = agentruntime.EventTypeReasoningDelta
		}
		return agentruntime.RuntimeEvent{Type: eventType, SessionID: sessionID, ExternalSessionID: appServerThreadIDFromParams(msg.Params), Text: text, Payload: payload}, true
	case "item/commandExecution/outputDelta":
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallStarted, SessionID: sessionID, ExternalSessionID: appServerThreadIDFromParams(msg.Params), Text: appServerText(msg.Params), Payload: payload}, true
	case "item/completed":
		event := appServerCompletedItemEvent(sessionID, msg.Params, payload)
		if appServerIsAgentMessage(msg.Params) {
			r.mu.Lock()
			streamed := r.streamedMessageText[sessionID]
			delete(r.streamedMessageText, sessionID)
			r.mu.Unlock()
			if event.Type != agentruntime.EventTypeTextDelta {
				return event, true
			}
			if strings.HasPrefix(event.Text, streamed) {
				event.Text = strings.TrimPrefix(event.Text, streamed)
				if event.Text == "" {
					return agentruntime.RuntimeEvent{}, false
				}
			} else if streamed != "" {
				return agentruntime.RuntimeEvent{}, false
			}
		}
		return event, true
	case "turn/completed":
		return appServerTurnCompletedEvent(sessionID, msg.Params, payload), true
	case "thread/status/changed":
		return appServerThreadStatusEvent(sessionID, msg.Params, payload), true
	case "error":
		text := appServerText(msg.Params)
		if text == "" {
			text = string(payload)
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, Text: text, Payload: payload, Err: errors.New(text)}, true
	default:
		return agentruntime.RuntimeEvent{}, false
	}
}

type appServerMessage struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *appServerError `json:"error,omitempty"`
}

type appServerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func appServerThreadParams(session agentruntime.RuntimeSession) map[string]any {
	params := map[string]any{}
	if session.ExternalSessionID != "" {
		params["threadId"] = session.ExternalSessionID
	}
	if session.Model != "" {
		params["model"] = session.Model
	}
	if session.WorkspacePath != "" {
		params["cwd"] = session.WorkspacePath
	}
	params["approvalPolicy"] = "on-request"
	params["approvalsReviewer"] = "user"
	return params
}

func appServerApprovalRequest(method string) bool {
	return method == "item/commandExecution/requestApproval" ||
		method == "item/fileChange/requestApproval" ||
		method == "item/permissions/requestApproval" ||
		method == "execCommandApproval" ||
		method == "applyPatchApproval"
}

func appServerApprovalID(msg appServerMessage) string {
	type approvalParams struct {
		ApprovalID string `json:"approvalId"`
		ItemID     string `json:"itemId"`
	}
	var params approvalParams
	_ = json.Unmarshal(msg.Params, &params)
	if params.ApprovalID != "" {
		return params.ApprovalID
	}
	if params.ItemID != "" {
		return params.ItemID
	}
	return idString(msg.ID)
}

func appServerApprovalPayload(line []byte, approvalID string) json.RawMessage {
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		return json.RawMessage(append([]byte{}, line...))
	}
	payload["approval_id"] = approvalID
	payload["approvalId"] = approvalID
	out, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(append([]byte{}, line...))
	}
	return out
}

func appServerApprovalResult(method string, decision agentruntime.RuntimeApprovalDecision) (map[string]any, error) {
	approvalDecision := "decline"
	if decision.Decision == agentruntime.ApprovalDecisionAccept {
		approvalDecision = "accept"
	}
	if method == "item/permissions/requestApproval" && approvalDecision == "accept" {
		return nil, agentruntime.ErrApprovalUnsupported
	}
	if method == "execCommandApproval" || method == "applyPatchApproval" {
		if approvalDecision == "accept" {
			return map[string]any{"decision": "approved"}, nil
		}
		return map[string]any{"decision": map[string]any{"denied": map[string]any{"rejection": decision.Reason}}}, nil
	}
	return map[string]any{"decision": approvalDecision}, nil
}

func appServerThreadID(result json.RawMessage) string {
	var payload struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		ID string `json:"id"`
	}
	_ = json.Unmarshal(result, &payload)
	return firstNonEmpty(payload.Thread.ID, payload.ID)
}

func appServerThreadIDFromParams(params json.RawMessage) string {
	var payload struct {
		ThreadID       string `json:"threadId"`
		ThreadIDSnake  string `json:"thread_id"`
		ConversationID string `json:"conversationId"`
	}
	_ = json.Unmarshal(params, &payload)
	return firstNonEmpty(payload.ThreadID, payload.ThreadIDSnake, payload.ConversationID)
}

func appServerText(params json.RawMessage) string {
	var payload struct {
		Delta   string `json:"delta"`
		Text    string `json:"text"`
		Message string `json:"message"`
		Summary string `json:"summary"`
	}
	_ = json.Unmarshal(params, &payload)
	return firstNonEmpty(payload.Delta, payload.Text, payload.Message, payload.Summary)
}

func appServerIsAgentMessage(params json.RawMessage) bool {
	var completed struct {
		Item struct {
			Type string `json:"type"`
		} `json:"item"`
	}
	_ = json.Unmarshal(params, &completed)
	return strings.EqualFold(completed.Item.Type, "agentMessage")
}

func appServerCompletedItemEvent(sessionID string, params json.RawMessage, payload json.RawMessage) agentruntime.RuntimeEvent {
	var completed struct {
		ThreadID string `json:"threadId"`
		Item     struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Path     string `json:"path"`
			File     string `json:"file"`
			FileName string `json:"fileName"`
		} `json:"item"`
	}
	_ = json.Unmarshal(params, &completed)
	if strings.Contains(strings.ToLower(completed.Item.Type), "file") {
		path := firstNonEmpty(completed.Item.Path, completed.Item.File)
		if path == "" && completed.Item.FileName == "" {
			return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: completed.ThreadID, Payload: payload}
		}
		filePayload, _ := json.Marshal(agentruntime.RuntimeFileCreatedPayload{
			Path:     path,
			FileName: completed.Item.FileName,
		})
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeFileCreated, SessionID: sessionID, ExternalSessionID: completed.ThreadID, Payload: filePayload}
	}
	switch {
	case strings.Contains(strings.ToLower(completed.Item.Type), "command"):
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeToolCallCompleted, SessionID: sessionID, ExternalSessionID: completed.ThreadID, Text: completed.Item.Text, Payload: payload}
	case completed.Item.Text != "":
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, SessionID: sessionID, ExternalSessionID: completed.ThreadID, Text: completed.Item.Text, Payload: payload}
	default:
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: completed.ThreadID, Payload: payload}
	}
}

func appServerTurnCompletedEvent(sessionID string, params json.RawMessage, payload json.RawMessage) agentruntime.RuntimeEvent {
	var completed struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"turn"`
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(params, &completed)
	status := strings.ToLower(firstNonEmpty(completed.Turn.Status, completed.Status))
	threadID := completed.ThreadID
	errText := firstNonEmpty(appServerErrorText(completed.Turn.Error), appServerErrorText(completed.Error))
	switch status {
	case "failed", "error":
		if errText == "" {
			errText = "codex app-server turn failed"
		}
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeError, SessionID: sessionID, ExternalSessionID: threadID, Text: errText, Payload: payload, Err: errors.New(errText)}
	case "interrupted", "cancelled", "canceled":
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCancelled, SessionID: sessionID, ExternalSessionID: threadID, Payload: payload}
	default:
		return agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted, SessionID: sessionID, ExternalSessionID: threadID, Payload: payload}
	}
}

func appServerErrorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var payload struct {
		Message           string `json:"message"`
		AdditionalDetails string `json:"additionalDetails"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return string(raw)
	}
	return firstNonEmpty(payload.Message, payload.AdditionalDetails, string(raw))
}

func appServerThreadStatusEvent(sessionID string, params json.RawMessage, payload json.RawMessage) agentruntime.RuntimeEvent {
	var changed struct {
		ThreadID string `json:"threadId"`
		Status   struct {
			Active bool     `json:"active"`
			Flags  []string `json:"flags"`
		} `json:"status"`
	}
	_ = json.Unmarshal(params, &changed)
	event := agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, SessionID: sessionID, ExternalSessionID: changed.ThreadID, Payload: payload}
	for _, flag := range changed.Status.Flags {
		if flag == "waitingOnApproval" {
			event.Text = string(agentruntime.SessionStatusWaitingApproval)
			return event
		}
	}
	if changed.Status.Active {
		event.Text = string(agentruntime.SessionStatusRunning)
	}
	return event
}

func (r *AppServerRuntime) setStatus(sessionID string, update func(agentruntime.RuntimeStatus) agentruntime.RuntimeStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.status[sessionID]
	if status.SessionID == "" {
		status.SessionID = sessionID
		status.RuntimeType = agentruntime.RuntimeTypeCodex
		status.ProviderID = "codex-app-server"
	}
	r.status[sessionID] = update(status)
}

func appServerRuntimeStatus(session agentruntime.RuntimeSession, status agentruntime.RuntimeSessionStatus, lastError string) agentruntime.RuntimeStatus {
	return agentruntime.RuntimeStatus{
		SessionID:     session.ID,
		Status:        status,
		RuntimeType:   agentruntime.RuntimeTypeCodex,
		ProviderID:    firstNonEmpty(session.ProviderID, "codex-app-server"),
		Model:         session.Model,
		WorkspacePath: session.WorkspacePath,
		LastError:     lastError,
	}
}

func idString(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
