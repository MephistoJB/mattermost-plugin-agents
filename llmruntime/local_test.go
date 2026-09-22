// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package llmruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLanguageModel struct {
	result  *llm.TextStreamResult
	results []*llm.TextStreamResult
	err     error
	req     llm.CompletionRequest
	reqs    []llm.CompletionRequest
	opts    []llm.LanguageModelOption
}

func (f *fakeLanguageModel) ChatCompletion(_ context.Context, req llm.CompletionRequest, opts ...llm.LanguageModelOption) (*llm.TextStreamResult, error) {
	f.req = req
	f.reqs = append(f.reqs, req)
	f.opts = opts
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		return result, f.err
	}
	return f.result, f.err
}

func (f *fakeLanguageModel) ChatCompletionNoStream(context.Context, llm.CompletionRequest, ...llm.LanguageModelOption) (string, error) {
	return "", nil
}

func (f *fakeLanguageModel) CountTokens(context.Context, llm.CompletionRequest, ...llm.LanguageModelOption) (int, error) {
	return 0, llm.ErrUnsupportedTokenCount
}

func (f *fakeLanguageModel) InputTokenLimit() int {
	return 0
}

func (f *fakeLanguageModel) OutputTokenLimit() int {
	return 0
}

func TestRuntimeStartTurnForwardsStreamEvents(t *testing.T) {
	stream := make(chan llm.TextStreamEvent, 4)
	stream <- llm.TextStreamEvent{Type: llm.EventTypeReasoning, Value: "thinking"}
	stream <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: "hello"}
	stream <- llm.TextStreamEvent{Type: llm.EventTypeUsage, Value: llm.TokenUsage{InputTokens: 10, OutputTokens: 4, Cost: 0.002}}
	stream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(stream)

	model := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: stream}}
	runtime := New(Options{LLM: model, RuntimeType: agentruntime.RuntimeTypeLocal, ProviderID: "ollama"})

	events, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:          "session-1",
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "ollama",
			Model:       "gpt-oss:20b",
		},
		Prompt: "hi",
	})
	require.NoError(t, err)

	got := drainEvents(events)
	require.Len(t, got, 4)
	assert.Equal(t, agentruntime.EventTypeReasoningDelta, got[0].Type)
	assert.Equal(t, "thinking", got[0].Text)
	assert.Equal(t, agentruntime.EventTypeTextDelta, got[1].Type)
	assert.Equal(t, "hello", got[1].Text)
	assert.Equal(t, agentruntime.EventTypeUsage, got[2].Type)
	assert.JSONEq(t, `{"input_tokens":10,"output_tokens":4,"cost":0.002}`, string(got[2].Payload))
	assert.Equal(t, agentruntime.EventTypeCompleted, got[3].Type)
	assert.Equal(t, "hi", model.req.Posts[0].Message)

	status, err := runtime.GetStatus(context.Background(), "session-1")
	require.NoError(t, err)
	assert.Equal(t, agentruntime.SessionStatusCompleted, status.Status)
}

func TestRuntimeStartTurnPassesLLMContext(t *testing.T) {
	stream := make(chan llm.TextStreamEvent, 1)
	stream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(stream)

	model := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: stream}}
	runtime := New(Options{LLM: model})
	tools := llm.NewNoTools()
	tools.AddTools([]llm.Tool{{Name: "nexus__search_memory"}})
	llmContext := &llm.Context{Tools: tools}

	events, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1"},
		Prompt:  "hi",
		Context: llmContext,
	})
	require.NoError(t, err)
	drainEvents(events)

	require.Same(t, llmContext, model.req.Context)
	require.Len(t, model.req.Context.Tools.GetTools(), 1)
	assert.Equal(t, "nexus__search_memory", model.req.Context.Tools.GetTools()[0].Name)
}

func TestRuntimeStartTurnExecutesApprovedTools(t *testing.T) {
	first := make(chan llm.TextStreamEvent, 2)
	first <- llm.TextStreamEvent{Type: llm.EventTypeToolCalls, Value: []llm.ToolCall{{
		ID:        "call-1",
		Name:      "nexus__search_memory",
		Arguments: []byte(`{}`),
	}}}
	first <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(first)
	second := make(chan llm.TextStreamEvent, 2)
	second <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: "answer from memory"}
	second <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(second)

	model := &fakeLanguageModel{results: []*llm.TextStreamResult{
		{Stream: first},
		{Stream: second},
	}}
	runtime := New(Options{LLM: model})
	resolved := false
	tools := llm.NewNoTools()
	tools.AddTools([]llm.Tool{{
		Name: "nexus__search_memory",
		Resolver: func(context.Context, *llm.Context, llm.ToolArgumentGetter) (string, error) {
			resolved = true
			return "memory result", nil
		},
	}})
	llmContext := &llm.Context{Tools: tools}

	events, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1"},
		Prompt:  "hi",
		Context: llmContext,
		ShouldExecuteTool: func(tc llm.ToolCall) bool {
			return tc.Name == "nexus__search_memory"
		},
	})
	require.NoError(t, err)
	got := drainEvents(events)

	require.True(t, resolved)
	require.Len(t, model.reqs, 2)
	require.Len(t, model.reqs[1].Posts, 2)
	require.Len(t, model.reqs[1].Posts[1].ToolUse, 1)
	assert.Equal(t, "memory result", model.reqs[1].Posts[1].ToolUse[0].Result)
	assert.Equal(t, agentruntime.EventTypeTextDelta, got[len(got)-2].Type)
	assert.Equal(t, "answer from memory", got[len(got)-2].Text)
	assert.Equal(t, agentruntime.EventTypeCompleted, got[len(got)-1].Type)
}

func TestRuntimeStartTurnReturnsStartError(t *testing.T) {
	model := &fakeLanguageModel{err: errors.New("provider down")}
	runtime := New(Options{LLM: model})

	_, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1"},
		Prompt:  "hi",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider down")

	status, statusErr := runtime.GetStatus(context.Background(), "session-1")
	require.NoError(t, statusErr)
	assert.Equal(t, agentruntime.SessionStatusFailed, status.Status)
}

func TestRuntimeStartTurnSanitizesStatusError(t *testing.T) {
	const leakedKey = "sk-proj-1234567890abcdefghijklmnop"
	model := &fakeLanguageModel{err: errors.New("provider leaked " + leakedKey)}
	runtime := New(Options{LLM: model})

	_, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1"},
		Prompt:  "hi",
	})
	require.Error(t, err)

	status, statusErr := runtime.GetStatus(context.Background(), "session-1")
	require.NoError(t, statusErr)
	assert.Equal(t, agentruntime.SessionStatusFailed, status.Status)
	assert.NotContains(t, status.LastError, leakedKey)
	assert.Contains(t, status.LastError, "[REDACTED]")
}

func TestRuntimeSelectsProviderBySessionProviderID(t *testing.T) {
	firstStream := make(chan llm.TextStreamEvent, 1)
	firstStream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(firstStream)
	secondStream := make(chan llm.TextStreamEvent, 1)
	secondStream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(secondStream)

	first := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: firstStream}}
	second := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: secondStream}}
	runtime := New(Options{Providers: map[string]llm.LanguageModel{
		"ollama":    first,
		"lm-studio": second,
	}})

	events, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{
			ID:         "session-1",
			ProviderID: "lm-studio",
			Model:      "local-model",
		},
		Prompt: "use selected provider",
	})
	require.NoError(t, err)
	drainEvents(events)

	assert.Empty(t, first.req.Posts)
	require.Len(t, second.req.Posts, 1)
	assert.Equal(t, "use selected provider", second.req.Posts[0].Message)
}

func TestRuntimeRejectsUnknownExplicitProviderID(t *testing.T) {
	stream := make(chan llm.TextStreamEvent, 1)
	stream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	close(stream)
	local := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: stream}}
	runtime := New(Options{Providers: map[string]llm.LanguageModel{
		"local-llm": local,
	}})

	_, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1", ProviderID: "cloud-compatible"},
		Prompt:  "must not fall back to local provider",
	})
	require.ErrorIs(t, err, agentruntime.ErrRuntimeUnavailable)
	assert.Empty(t, local.req.Posts)
}

func TestRuntimeStartTurnRequiresProviderWhenAmbiguous(t *testing.T) {
	runtime := New(Options{Providers: map[string]llm.LanguageModel{
		"ollama":    &fakeLanguageModel{},
		"lm-studio": &fakeLanguageModel{},
	}})

	_, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1", ProviderID: "missing"},
		Prompt:  "hi",
	})
	require.ErrorIs(t, err, agentruntime.ErrRuntimeUnavailable)
}

func TestRuntimeStopTurnCancelsActiveStream(t *testing.T) {
	stream := make(chan llm.TextStreamEvent)
	model := &fakeLanguageModel{result: &llm.TextStreamResult{Stream: stream}}
	runtime := New(Options{LLM: model})

	events, err := runtime.StartTurn(context.Background(), agentruntime.RuntimeTurnRequest{
		Session: agentruntime.RuntimeSession{ID: "session-1"},
		Prompt:  "hi",
	})
	require.NoError(t, err)
	require.NoError(t, runtime.StopTurn(context.Background(), "session-1"))

	got := drainEvents(events)
	require.NotEmpty(t, got)
	assert.Equal(t, agentruntime.EventTypeCancelled, got[len(got)-1].Type)
}

func drainEvents(events <-chan agentruntime.RuntimeEvent) []agentruntime.RuntimeEvent {
	got := []agentruntime.RuntimeEvent{}
	for event := range events {
		got = append(got, event)
	}
	return got
}
