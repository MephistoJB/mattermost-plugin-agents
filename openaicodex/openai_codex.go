// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package openaicodex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
)

const (
	DefaultBaseURL = "https://chatgpt.com/backend-api/codex"
	DefaultModel   = "gpt-5.5"
)

var ErrNeedsOAuth = errors.New("openai codex login required")

type Config struct {
	Service    llm.ServiceConfig
	Bot        llm.BotConfig
	Store      mmapi.Client
	HTTPClient *http.Client
}

type LLM struct {
	manager          *Manager
	defaultModel     string
	inputTokenLimit  int
	outputTokenLimit int
	baseURL          string
}

func New(cfg Config) *LLM {
	model := cfg.Bot.Model
	if model == "" {
		model = cfg.Service.DefaultModel
	}
	if model == "" {
		model = DefaultModel
	}

	baseURL := strings.TrimRight(cfg.Service.APIURL, "/")
	if baseURL == "" || !isAllowedBaseURL(baseURL) {
		baseURL = DefaultBaseURL
	}

	return &LLM{
		manager: NewManager(ManagerConfig{
			Store:      cfg.Store,
			HTTPClient: cfg.HTTPClient,
		}),
		defaultModel:     model,
		inputTokenLimit:  cfg.Service.InputTokenLimit,
		outputTokenLimit: cfg.Service.OutputTokenLimit,
		baseURL:          baseURL,
	}
}

func (l *LLM) ChatCompletion(ctx context.Context, request llm.CompletionRequest, opts ...llm.LanguageModelOption) (*llm.TextStreamResult, error) {
	cfg := l.createConfig(opts)
	if err := requireRequestUser(request); err != nil {
		return nil, err
	}

	token, err := l.manager.AccessToken(ctx, ProviderCredentialSubject)
	if err != nil {
		return nil, err
	}

	stream := make(chan llm.TextStreamEvent)
	go func() {
		defer close(stream)
		l.streamResponses(ctx, token, request, cfg, stream)
	}()
	return &llm.TextStreamResult{Stream: stream}, nil
}

func (l *LLM) ChatCompletionNoStream(ctx context.Context, request llm.CompletionRequest, opts ...llm.LanguageModelOption) (string, error) {
	result, err := l.ChatCompletion(ctx, request, opts...)
	if err != nil {
		return "", err
	}
	return result.ReadAll()
}

func (l *LLM) CountTokens(ctx context.Context, request llm.CompletionRequest, opts ...llm.LanguageModelOption) (int, error) {
	return 0, llm.ErrUnsupportedTokenCount
}

func (l *LLM) InputTokenLimit() int {
	if l.inputTokenLimit > 0 {
		return l.inputTokenLimit
	}
	return 200000
}

func (l *LLM) OutputTokenLimit() int {
	if l.outputTokenLimit > 0 {
		return l.outputTokenLimit
	}
	return 8192
}

func (l *LLM) createConfig(opts []llm.LanguageModelOption) llm.LanguageModelConfig {
	cfg := llm.LanguageModelConfig{
		Model:              l.defaultModel,
		MaxGeneratedTokens: l.OutputTokenLimit(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.Model == "" {
		cfg.Model = l.defaultModel
	}
	return cfg
}

func requireRequestUser(request llm.CompletionRequest) error {
	if request.Context == nil || request.Context.RequestingUser == nil || request.Context.RequestingUser.Id == "" {
		return fmt.Errorf("%w: missing Mattermost user context", ErrNeedsOAuth)
	}
	return nil
}

func isAllowedBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" &&
		parsed.Host == "chatgpt.com" &&
		(parsed.Path == "/backend-api/codex" || strings.HasPrefix(parsed.Path, "/backend-api/codex/")) &&
		parsed.RawQuery == "" &&
		parsed.Fragment == ""
}

func (l *LLM) streamResponses(ctx context.Context, accessToken string, request llm.CompletionRequest, cfg llm.LanguageModelConfig, output chan<- llm.TextStreamEvent) {
	body, err := buildResponsesRequest(request, cfg)
	if err != nil {
		output <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: err}
		return
	}

	resp, err := l.doResponsesRequest(ctx, accessToken, body)
	if err != nil {
		output <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: sanitizeError(err, accessToken)}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		output <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: classifyProviderError(resp.StatusCode, payload)}
		return
	}

	if err := parseSSE(resp.Body, output); err != nil {
		output <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: sanitizeError(err, accessToken)}
		return
	}
	output <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
}

func (l *LLM) doResponsesRequest(ctx context.Context, accessToken string, body []byte) (*http.Response, error) {
	client := l.manager.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(l.baseURL, "/")+"/responses", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("session_id", requestID())
		req.Header.Set("x-client-request-id", promptCacheKey(body))

		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

type responsesRequest struct {
	Model        string          `json:"model"`
	Instructions string          `json:"instructions,omitempty"`
	Input        []responsesItem `json:"input"`
	Store        bool            `json:"store"`
	Stream       bool            `json:"stream"`
	Tools        []responsesTool `json:"tools,omitempty"`
	Reasoning    *reasoning      `json:"reasoning,omitempty"`
}

type responsesItem struct {
	Type      string        `json:"type,omitempty"`
	Role      string        `json:"role,omitempty"`
	Content   []contentItem `json:"content,omitempty"`
	CallID    string        `json:"call_id,omitempty"`
	Name      string        `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Output    string        `json:"output,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

func buildResponsesRequest(request llm.CompletionRequest, cfg llm.LanguageModelConfig) ([]byte, error) {
	out := responsesRequest{
		Model:  cfg.Model,
		Store:  false,
		Stream: true,
		Input:  make([]responsesItem, 0, len(request.Posts)),
	}
	for _, post := range request.Posts {
		switch post.Role {
		case llm.PostRoleSystem:
			if post.Message != "" {
				if out.Instructions != "" {
					out.Instructions += "\n\n"
				}
				out.Instructions += post.Message
			}
		case llm.PostRoleUser:
			out.Input = append(out.Input, messageItem("user", "input_text", post.Message))
		case llm.PostRoleBot:
			if post.Message != "" {
				out.Input = append(out.Input, messageItem("assistant", "output_text", post.Message))
			}
			for _, tc := range post.ToolUse {
				args := tc.Arguments
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				out.Input = append(out.Input, responsesItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(args),
				})
				if tc.Result != "" {
					out.Input = append(out.Input, responsesItem{
						Type:   "function_call_output",
						CallID: tc.ID,
						Output: tc.Result,
					})
				}
			}
		}
	}

	if request.Context != nil && request.Context.Tools != nil && !cfg.ToolsDisabled {
		for _, tool := range request.Context.Tools.GetTools() {
			out.Tools = append(out.Tools, responsesTool{
				Type:        "function",
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  toolSchema(tool.Schema),
			})
		}
	}
	if !cfg.ReasoningDisabled {
		out.Reasoning = &reasoning{Effort: "medium", Summary: "auto"}
	}
	return json.Marshal(out)
}

func messageItem(role, contentType, text string) responsesItem {
	if text == "" {
		text = " "
	}
	return responsesItem{
		Type: "message",
		Role: role,
		Content: []contentItem{{
			Type: contentType,
			Text: text,
		}},
	}
}

func parseSSE(r io.Reader, output chan<- llm.TextStreamEvent) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var eventName string
	textSeen := false
	var toolCalls []llm.ToolCall
	buffers := map[string]*strings.Builder{}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			if err := handleSSEEvent(eventName, []byte(data), output, &toolCalls, buffers, &textSeen); err != nil {
				return err
			}
		case line == "":
			eventName = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(toolCalls) > 0 {
		output <- llm.TextStreamEvent{Type: llm.EventTypeToolCalls, Value: toolCalls}
	}
	return nil
}

func handleSSEEvent(eventName string, data []byte, output chan<- llm.TextStreamEvent, toolCalls *[]llm.ToolCall, buffers map[string]*strings.Builder, textSeen *bool) error {
	var ev struct {
		Type     string          `json:"type"`
		Delta    string          `json:"delta"`
		Item     json.RawMessage `json:"item"`
		Response *struct {
			Usage *struct {
				InputTokens     int64 `json:"input_tokens"`
				OutputTokens    int64 `json:"output_tokens"`
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"usage"`
		} `json:"response"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	if ev.Type == "" {
		ev.Type = eventName
	}
	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta != "" {
			*textSeen = true
			output <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: ev.Delta}
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if ev.Delta != "" {
			output <- llm.TextStreamEvent{Type: llm.EventTypeReasoning, Value: ev.Delta}
		}
	case "response.function_call_arguments.delta":
		var d struct {
			OutputIndex int    `json:"output_index"`
			CallID      string `json:"call_id"`
			Delta       string `json:"delta"`
		}
		_ = json.Unmarshal(data, &d)
		key := d.CallID
		if key == "" {
			key = fmt.Sprintf("%d", d.OutputIndex)
		}
		if buffers[key] == nil {
			buffers[key] = &strings.Builder{}
		}
		buffers[key].WriteString(d.Delta)
	case "response.output_item.added", "response.output_item.done":
		if len(ev.Item) > 0 {
			if tc, ok := parseToolCallItem(ev.Item, buffers); ok {
				upsertToolCall(toolCalls, tc)
			} else if ev.Type == "response.output_item.done" && !*textSeen {
				if text := parseMessageText(ev.Item); text != "" {
					*textSeen = true
					output <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: text}
				}
			}
		}
	case "response.completed":
		if ev.Response != nil && ev.Response.Usage != nil {
			output <- llm.TextStreamEvent{
				Type: llm.EventTypeUsage,
				Value: llm.TokenUsage{
					InputTokens:     ev.Response.Usage.InputTokens,
					OutputTokens:    ev.Response.Usage.OutputTokens,
					ReasoningTokens: ev.Response.Usage.ReasoningTokens,
				},
			}
		}
	case "response.failed", "response.incomplete", "error":
		if ev.Error != nil {
			return fmt.Errorf("provider error: %s", providerErrorMessage(ev.Error))
		}
		return fmt.Errorf("provider returned %s", ev.Type)
	}
	return nil
}

func parseMessageText(raw json.RawMessage) string {
	var item struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || item.Type != "message" {
		return ""
	}
	var out strings.Builder
	for _, content := range item.Content {
		switch content.Type {
		case "output_text", "text":
			out.WriteString(content.Text)
		}
	}
	return out.String()
}

func parseToolCallItem(raw json.RawMessage, buffers map[string]*strings.Builder) (llm.ToolCall, bool) {
	var item struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || item.Type != "function_call" || item.Name == "" {
		return llm.ToolCall{}, false
	}
	id := item.CallID
	if id == "" {
		id = item.ID
	}
	if id == "" {
		id = requestID()
	}
	args := item.Arguments
	if len(args) == 0 || string(args) == `""` {
		if b := buffers[id]; b != nil && b.Len() > 0 {
			args = json.RawMessage(b.String())
		}
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return llm.ToolCall{
		ID:        id,
		Name:      item.Name,
		Arguments: toolArgsToJSON(string(args)),
		Status:    llm.ToolCallStatusPending,
	}, true
}

func upsertToolCall(calls *[]llm.ToolCall, call llm.ToolCall) {
	for i := range *calls {
		if (*calls)[i].ID == call.ID {
			(*calls)[i] = call
			return
		}
	}
	*calls = append(*calls, call)
}

func toolArgsToJSON(args string) json.RawMessage {
	args = strings.TrimSpace(args)
	if args == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(args)) {
		return json.RawMessage(args)
	}
	b, _ := json.Marshal(args)
	return b
}

func providerErrorMessage(value any) string {
	switch v := value.(type) {
	case string:
		if v != "" {
			return v
		}
	case map[string]any:
		for _, key := range []string{"code", "type"} {
			if raw, ok := v[key].(string); ok && raw != "" {
				return raw
			}
		}
	}
	return "request failed"
}

func classifyProviderError(status int, body []byte) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrNeedsOAuth
	}
	return fmt.Errorf("provider request failed with HTTP %d: %s", status, sanitizeProviderBody(body))
}

func sanitizeProviderBody(body []byte) string {
	var parsed struct {
		Error any    `json:"error"`
		Code  string `json:"code"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return http.StatusText(http.StatusBadGateway)
	}
	if parsed.Code != "" {
		return parsed.Code
	}
	if parsed.Type != "" {
		return parsed.Type
	}
	if parsed.Error != nil {
		return providerErrorMessage(parsed.Error)
	}
	return http.StatusText(http.StatusBadGateway)
}

func toolSchema(schema any) any {
	if schema != nil {
		return schema
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func sanitizeError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	return llm.SanitizeProviderError(err, secrets...)
}

func promptCacheKey(body []byte) string {
	sum := sha256.Sum256(body)
	return "pck_" + hex.EncodeToString(sum[:12])
}

func requestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

var _ llm.LanguageModel = (*LLM)(nil)
