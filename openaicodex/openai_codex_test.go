// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package openaicodex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
	mmapimocks "github.com/mattermost/mattermost-plugin-agents/v2/mmapi/mocks"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLanguageModelAssertion(t *testing.T) {
	t.Parallel()
	var _ llm.LanguageModel = (*LLM)(nil)
}

func TestNewDefaultsModelAndLimits(t *testing.T) {
	t.Parallel()

	lm := New(Config{})
	require.Equal(t, DefaultModel, lm.defaultModel)
	require.Equal(t, 200000, lm.InputTokenLimit())
	require.Equal(t, 8192, lm.OutputTokenLimit())
}

func TestNewUsesConfiguredModelAndLimits(t *testing.T) {
	t.Parallel()

	lm := New(Config{
		Service: llm.ServiceConfig{
			DefaultModel:     "codex-service-model",
			InputTokenLimit:  123,
			OutputTokenLimit: 456,
		},
		Bot: llm.BotConfig{
			Model: "codex-bot-model",
		},
	})

	require.Equal(t, "codex-bot-model", lm.defaultModel)
	require.Equal(t, 123, lm.InputTokenLimit())
	require.Equal(t, 456, lm.OutputTokenLimit())
}

func TestNewOnlyAllowsCodexBackendBaseURL(t *testing.T) {
	t.Parallel()

	lm := New(Config{Service: llm.ServiceConfig{APIURL: "http://169.254.169.254/latest/meta-data"}})
	require.Equal(t, DefaultBaseURL, lm.baseURL)

	lm = New(Config{Service: llm.ServiceConfig{APIURL: DefaultBaseURL + "/"}})
	require.Equal(t, DefaultBaseURL, lm.baseURL)
}

func TestMissingRequestUserReturnsNeedsOAuthError(t *testing.T) {
	t.Parallel()

	lm := New(Config{})
	_, err := lm.ChatCompletionNoStream(context.Background(), llm.CompletionRequest{})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNeedsOAuth))
	require.Contains(t, err.Error(), "missing Mattermost user context")
}

func TestMissingTokenReturnsNeedsOAuthError(t *testing.T) {
	t.Parallel()

	store := mmapimocks.NewMockClient(t)
	store.On("KVGet", tokenKey(ProviderCredentialSubject), mock.AnythingOfType("*[]uint8")).Return(mmapi.ErrKVNotFound)
	lm := New(Config{Store: store})
	_, err := lm.ChatCompletionNoStream(context.Background(), llm.CompletionRequest{
		Context: &llm.Context{
			RequestingUser: &model.User{Id: "requesting-user-id"},
		},
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNeedsOAuth))
}

func TestChatCompletionUsesGlobalProviderCredential(t *testing.T) {
	t.Parallel()

	env := &tokenEnvelope{
		Version:      tokenVersion,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}
	raw, err := json.Marshal(env)
	require.NoError(t, err)

	store := mmapimocks.NewMockClient(t)
	store.On("KVGet", tokenKey(ProviderCredentialSubject), mock.AnythingOfType("*[]uint8")).
		Run(func(args mock.Arguments) {
			target := args.Get(1).(*[]byte)
			*target = raw
		}).
		Return(nil)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, DefaultBaseURL+"/responses", req.URL.String())
		require.Equal(t, "Bearer access-token", req.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`event: response.output_text.delta`,
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				``,
			}, "\n"))),
		}, nil
	})}

	lm := New(Config{Store: store, HTTPClient: client})
	result, err := lm.ChatCompletionNoStream(context.Background(), llm.CompletionRequest{
		Context: &llm.Context{
			RequestingUser: &model.User{Id: "requesting-user-id"},
		},
		Posts: []llm.Post{{Role: llm.PostRoleUser, Message: "hello"}},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", result)
}

func TestOAuthDeviceCodeFlowExchangesDeviceCodeWithMockTokenEndpoint(t *testing.T) {
	t.Parallel()

	var storedSession *deviceSession
	var storedToken *tokenEnvelope
	store := mmapimocks.NewMockClient(t)
	store.On("KVSetWithExpiry", mock.MatchedBy(func(key string) bool {
		return strings.HasPrefix(key, "openai_codex_device_session_v1_user-id_")
	}), mock.AnythingOfType("*openaicodex.deviceSession"), sessionTTL).
		Run(func(args mock.Arguments) {
			storedSession = args.Get(1).(*deviceSession)
		}).
		Return(nil)
	store.On("KVGet", mock.AnythingOfType("string"), mock.AnythingOfType("*openaicodex.deviceSession")).
		Run(func(args mock.Arguments) {
			target := args.Get(1).(*deviceSession)
			*target = *storedSession
		}).
		Return(nil)
	store.On("KVSet", tokenKey(ProviderCredentialSubject), mock.AnythingOfType("*openaicodex.tokenEnvelope")).
		Run(func(args mock.Arguments) {
			storedToken = args.Get(1).(*tokenEnvelope)
		}).
		Return(nil)
	store.On("KVDelete", mock.AnythingOfType("string")).Return(nil)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case deviceUserCodeURL:
			require.Equal(t, http.MethodPost, req.Method)
			return jsonResponse(http.StatusOK, `{"user_code":"ABCD-EFGH","device_auth_id":"device-id","interval":1}`), nil
		case deviceTokenURL:
			require.Equal(t, http.MethodPost, req.Method)
			return jsonResponse(http.StatusOK, `{"authorization_code":"auth-code","code_verifier":"verifier"}`), nil
		case oauthTokenURL:
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			form := string(body)
			require.Contains(t, form, "grant_type=authorization_code")
			require.Contains(t, form, "code=auth-code")
			require.Contains(t, form, "code_verifier=verifier")
			return jsonResponse(http.StatusOK, `{"access_token":"`+testJWT(time.Now().Add(time.Hour))+`","refresh_token":"refresh-1","token_type":"Bearer"}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}
	manager := NewManager(ManagerConfig{Store: store, HTTPClient: client})

	start, err := manager.StartDeviceFlow(context.Background(), "user-id")
	require.NoError(t, err)
	require.Equal(t, "ABCD-EFGH", start.UserCode)
	require.Equal(t, deviceVerifyURL, start.VerificationURI)
	require.NotNil(t, storedSession)

	status, err := manager.PollDeviceFlow(context.Background(), "user-id", start.SessionID)
	require.NoError(t, err)
	require.True(t, status.Connected)
	require.NotNil(t, storedToken)
	require.Equal(t, "refresh-1", storedToken.RefreshToken)
}

func TestTokenRefreshPersistsRotatedToken(t *testing.T) {
	t.Parallel()

	expired := &tokenEnvelope{
		Version:      tokenVersion,
		AccessToken:  testJWT(time.Now().Add(-time.Minute)),
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Minute),
	}
	expiredRaw, err := json.Marshal(expired)
	require.NoError(t, err)

	store := mmapimocks.NewMockClient(t)
	store.On("KVGet", tokenKey("user-id"), mock.AnythingOfType("*[]uint8")).
		Run(func(args mock.Arguments) {
			target := args.Get(1).(*[]byte)
			*target = expiredRaw
		}).
		Return(nil)
	store.On("KVCompareAndSetWithExpiry", refreshLeaseKey("user-id"), nil, mock.AnythingOfType("string"), refreshLeaseTTL).Return(true, nil)
	store.On("KVCompareAndSet", refreshLeaseKey("user-id"), mock.AnythingOfType("string"), nil).Return(true, nil)
	store.On("KVCompareAndSet", tokenKey("user-id"), expiredRaw, mock.MatchedBy(func(value any) bool {
		raw, ok := value.([]byte)
		if !ok {
			return false
		}
		var env tokenEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return false
		}
		return env.AccessToken != "" && env.RefreshToken == "refresh-new"
	})).Return(true, nil)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, oauthTokenURL, req.URL.String())
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		form := string(body)
		require.Contains(t, form, "grant_type=refresh_token")
		require.Contains(t, form, "refresh_token=refresh-old")
		return jsonResponse(http.StatusOK, `{"access_token":"`+testJWT(time.Now().Add(time.Hour))+`","refresh_token":"refresh-new","token_type":"Bearer"}`), nil
	})}

	token, err := NewManager(ManagerConfig{Store: store, HTTPClient: client}).AccessToken(context.Background(), "user-id")
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

func TestTokenRefreshRaceUsesSingleTokenEndpointCall(t *testing.T) {
	t.Parallel()

	oldAccess := testJWT(time.Now().Add(-time.Minute))
	newAccess := testJWT(time.Now().Add(time.Hour))
	expired := &tokenEnvelope{
		Version:      tokenVersion,
		AccessToken:  oldAccess,
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Minute),
	}
	currentRaw, err := json.Marshal(expired)
	require.NoError(t, err)

	var mu sync.Mutex
	leaseHeld := false
	refreshCalls := 0

	store := mmapimocks.NewMockClient(t)
	store.On("KVGet", tokenKey("user-id"), mock.AnythingOfType("*[]uint8")).
		Run(func(args mock.Arguments) {
			mu.Lock()
			defer mu.Unlock()
			target := args.Get(1).(*[]byte)
			*target = append([]byte(nil), currentRaw...)
		}).
		Return(nil)
	store.On("KVCompareAndSetWithExpiry", refreshLeaseKey("user-id"), nil, mock.AnythingOfType("string"), refreshLeaseTTL).
		Run(func(args mock.Arguments) {}).
		Return(func(string, interface{}, interface{}, time.Duration) bool {
			mu.Lock()
			defer mu.Unlock()
			if leaseHeld {
				return false
			}
			leaseHeld = true
			return true
		}, nil)
	store.On("KVCompareAndSet", refreshLeaseKey("user-id"), mock.AnythingOfType("string"), nil).
		Run(func(args mock.Arguments) {
			mu.Lock()
			leaseHeld = false
			mu.Unlock()
		}).
		Return(true, nil)
	store.On("KVCompareAndSet", tokenKey("user-id"), mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			mu.Lock()
			defer mu.Unlock()
			oldValue, _ := args.Get(1).([]byte)
			newValue, _ := args.Get(2).([]byte)
			if string(oldValue) == string(currentRaw) {
				currentRaw = append([]byte(nil), newValue...)
			}
		}).
		Return(true, nil)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		refreshCalls++
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		return jsonResponse(http.StatusOK, `{"access_token":"`+newAccess+`","refresh_token":"refresh-new","token_type":"Bearer"}`), nil
	})}
	manager := NewManager(ManagerConfig{Store: store, HTTPClient: client})

	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := manager.AccessToken(context.Background(), "user-id")
			if err != nil {
				errs <- err
				return
			}
			results <- token
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	require.Empty(t, errs)
	for token := range results {
		require.Equal(t, newAccess, token)
	}
	require.Equal(t, 1, refreshCalls)
}

func TestExpiredTokenWithoutRefreshReturnsNeedsOAuthError(t *testing.T) {
	t.Parallel()

	expired := &tokenEnvelope{
		Version:     tokenVersion,
		AccessToken: testJWT(time.Now().Add(-time.Minute)),
		Expiry:      time.Now().Add(-time.Minute),
	}
	raw, err := json.Marshal(expired)
	require.NoError(t, err)

	store := mmapimocks.NewMockClient(t)
	store.On("KVGet", tokenKey("user-id"), mock.AnythingOfType("*[]uint8")).
		Run(func(args mock.Arguments) {
			target := args.Get(1).(*[]byte)
			*target = raw
		}).
		Return(nil)
	store.On("KVDelete", tokenKey("user-id")).Return(nil)

	_, err = NewManager(ManagerConfig{Store: store}).AccessToken(context.Background(), "user-id")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNeedsOAuth))
}

func TestProviderSelectionValidatesOpenAICodexConfig(t *testing.T) {
	t.Parallel()

	require.True(t, llm.IsValidService(llm.ServiceConfig{ID: "svc", Type: llm.ServiceTypeOpenAICodex}))
	require.False(t, llm.IsValidService(llm.ServiceConfig{ID: "svc", Type: "openai-codex-unknown"}))
}

func TestResponsesRequestEncodingMatchesCodexContract(t *testing.T) {
	t.Parallel()

	tools := llm.NewToolStore()
	tools.AddTools([]llm.Tool{{
		Name:        "lookup_ticket",
		Description: "Look up a ticket",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
		},
	}})
	body, err := buildResponsesRequest(llm.CompletionRequest{
		Context: &llm.Context{Tools: tools},
		Posts: []llm.Post{
			{Role: llm.PostRoleSystem, Message: "system"},
			{Role: llm.PostRoleUser, Message: "hello"},
			{Role: llm.PostRoleBot, Message: "calling", ToolUse: []llm.ToolCall{{
				ID:        "call-1",
				Name:      "lookup_ticket",
				Arguments: json.RawMessage(`{"id":"MM-1"}`),
				Result:    "done",
			}}},
		},
	}, llm.LanguageModelConfig{Model: "gpt-test"})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "gpt-test", payload["model"])
	require.Equal(t, "system", payload["instructions"])
	require.Equal(t, false, payload["store"])
	require.Equal(t, true, payload["stream"])
	require.NotContains(t, payload, "parallel_tool_calls")
	require.NotContains(t, payload, "tool_choice")
	input := payload["input"].([]any)
	require.Len(t, input, 4)
	call := input[2].(map[string]any)
	require.Equal(t, "function_call", call["type"])
	require.Equal(t, `{"id":"MM-1"}`, call["arguments"])
	tool := payload["tools"].([]any)[0].(map[string]any)
	require.Equal(t, "function", tool["type"])
	require.Equal(t, "lookup_ticket", tool["name"])
}

func TestStreamingParserEmitsTextToolCallsAndErrors(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`event: response.function_call_arguments.delta`,
		`data: {"type":"response.function_call_arguments.delta","call_id":"call-1","delta":"{\"id\":\"MM-1\"}"}`,
		``,
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call-1","name":"lookup_ticket"}}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":5,"reasoning_tokens":2}}}`,
		``,
	}, "\n"))

	output := make(chan llm.TextStreamEvent, 8)
	require.NoError(t, parseSSE(stream, output))
	close(output)

	var events []llm.TextStreamEvent
	for event := range output {
		events = append(events, event)
	}
	require.Equal(t, llm.EventTypeText, events[0].Type)
	require.Equal(t, "hi", events[0].Value)
	require.Equal(t, llm.EventTypeUsage, events[1].Type)
	require.Equal(t, llm.EventTypeToolCalls, events[2].Type)
	calls := events[2].Value.([]llm.ToolCall)
	require.Len(t, calls, 1)
	require.Equal(t, "lookup_ticket", calls[0].Name)
	require.JSONEq(t, `{"id":"MM-1"}`, string(calls[0].Arguments))
}

func TestStreamingParserUsesCompletedMessageWhenNoTextDeltaArrives(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(strings.Join([]string{
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"final answer"}]}}`,
		``,
	}, "\n"))

	output := make(chan llm.TextStreamEvent, 2)
	require.NoError(t, parseSSE(stream, output))
	close(output)

	event := <-output
	require.Equal(t, llm.EventTypeText, event.Type)
	require.Equal(t, "final answer", event.Value)
}

func TestProviderLogsRedactOAuthAndCodexSecrets(t *testing.T) {
	t.Parallel()

	err := classifyProviderError(http.StatusBadGateway, []byte(`{"error":{"message":"access-secret refresh-secret","code":"bad_gateway"}}`))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "access-secret")
	require.NotContains(t, err.Error(), "refresh-secret")
	require.Contains(t, err.Error(), "bad_gateway")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func testJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}
