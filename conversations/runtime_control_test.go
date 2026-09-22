// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/mcp"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost-plugin-agents/v2/workspacefiles"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

type runtimeControlTestConfig struct{}

func (c runtimeControlTestConfig) EnableChannelMentionToolCalling() bool {
	return false
}

func (c runtimeControlTestConfig) AllowNativeWebSearchInChannels() bool {
	return false
}

func (c runtimeControlTestConfig) EnableAgentRuntimeControlPlane() bool {
	return true
}

func (c runtimeControlTestConfig) MCP() mcp.Config {
	return mcp.Config{}
}

type runtimeTurnControl struct {
	req        runtimecontrol.StartTurnRequest
	registered []agentruntime.RuntimeType
	events     chan agentruntime.RuntimeEvent
	session    agentruntime.RuntimeSession
}

func (r *runtimeTurnControl) RegisterRuntime(agentruntime.RuntimeType, agentruntime.AgentRuntime) {
}

func (r *runtimeTurnControl) RegisterRuntimeIfAbsent(runtimeType agentruntime.RuntimeType, _ agentruntime.AgentRuntime) {
	r.registered = append(r.registered, runtimeType)
}

func (r *runtimeTurnControl) ResolvePolicy(runtimecontrol.EnsureSessionRequest) (agentruntime.EffectivePolicy, error) {
	return agentruntime.EffectivePolicy{}, nil
}

func (r *runtimeTurnControl) StartTurn(_ context.Context, req runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error) {
	r.req = req
	events := r.events
	if events == nil {
		events = make(chan agentruntime.RuntimeEvent)
		close(events)
	}
	session := r.session
	if session.WorkspacePath == "" {
		session.WorkspacePath = "/workspace/project"
	}
	return &runtimecontrol.StartTurnResult{
		Session: session,
		Events:  events,
	}, nil
}

func (r *runtimeTurnControl) StartSupervisorTurn(ctx context.Context, req runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error) {
	return r.StartTurn(ctx, req)
}

func TestStartRuntimeTurnCarriesServerAndTeamScope(t *testing.T) {
	rt := &runtimeTurnControl{}
	conversations := &Conversations{
		configProvider: runtimeControlTestConfig{},
		runtimeControl: rt,
		serverID:       "server-1",
	}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-user-id"}, nil)
	user := &model.User{Id: "user-id"}
	channel := &model.Channel{Id: "channel-id", TeamId: "team-id", Type: model.ChannelTypeOpen}
	post := &model.Post{Id: "post-id", ChannelId: "channel-id", RootId: "root-id"}

	_, err := conversations.startRuntimeTurn(context.Background(), bot, post, user, channel, "conversation-1", "hello", nil, false)

	require.NoError(t, err)
	require.Equal(t, "server-1", rt.req.SessionRequest.ServerID)
	require.Equal(t, "team-id", rt.req.SessionRequest.TeamID)
	require.Equal(t, "channel-id", rt.req.SessionRequest.ChannelID)
}

func TestStartRuntimeTurnDoesNotRegisterBotLLMAsLocalRuntime(t *testing.T) {
	rt := &runtimeTurnControl{}
	conversations := &Conversations{
		configProvider: runtimeControlTestConfig{},
		runtimeControl: rt,
	}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-user-id"}, nil)
	user := &model.User{Id: "user-id"}
	channel := &model.Channel{Id: "channel-id", TeamId: "team-id", Type: model.ChannelTypeOpen}
	post := &model.Post{Id: "post-id", ChannelId: "channel-id"}

	_, err := conversations.startRuntimeTurn(context.Background(), bot, post, user, channel, "conversation-1", "hello", nil, false)

	require.NoError(t, err)
	require.NotContains(t, rt.registered, agentruntime.RuntimeTypeLocal)
	require.Contains(t, rt.registered, agentruntime.RuntimeTypeOpenAI)
}

func TestStartRuntimeTurnStreamsRuntimeEventsAndApprovalNotice(t *testing.T) {
	events := make(chan agentruntime.RuntimeEvent, 5)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeReasoningDelta, Text: "thinking"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "hello"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeApprovalRequested}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeStatus, Text: string(agentruntime.SessionStatusWaitingApproval)}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)
	rt := &runtimeTurnControl{events: events}
	conversations := &Conversations{
		configProvider: runtimeControlTestConfig{},
		runtimeControl: rt,
	}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-user-id"}, nil)
	user := &model.User{Id: "user-id"}
	channel := &model.Channel{Id: "channel-id", TeamId: "team-id", Type: model.ChannelTypeOpen}
	post := &model.Post{Id: "post-id", ChannelId: "channel-id"}

	stream, err := conversations.startRuntimeTurn(context.Background(), bot, post, user, channel, "conversation-1", "hello", nil, false)
	require.NoError(t, err)

	got := drainTextStreamEvents(t, stream)
	require.Equal(t, []llm.EventType{llm.EventTypeReasoning, llm.EventTypeText, llm.EventTypeText, llm.EventTypeEnd}, eventTypes(got))
	require.Equal(t, "thinking", got[0].Value)
	require.Equal(t, "hello", got[1].Value)
	require.Equal(t, "Waiting for approval before continuing.", got[2].Value)
}

func TestStartRuntimeTurnPassesLLMContext(t *testing.T) {
	rt := &runtimeTurnControl{}
	conversations := &Conversations{
		configProvider: runtimeControlTestConfig{},
		runtimeControl: rt,
	}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-user-id"}, nil)
	user := &model.User{Id: "user-id"}
	channel := &model.Channel{Id: "channel-id", TeamId: "team-id", Type: model.ChannelTypeDirect}
	post := &model.Post{Id: "post-id", ChannelId: "channel-id"}
	tools := llm.NewNoTools()
	tools.AddTools([]llm.Tool{{
		Name:        "nexus__search_memory",
		Description: "Search Nexus memory",
	}})
	llmContext := &llm.Context{Tools: tools}

	_, err := conversations.startRuntimeTurn(context.Background(), bot, post, user, channel, "conversation-1", "hello", llmContext, false)

	require.NoError(t, err)
	require.Same(t, llmContext, rt.req.Context)
	require.Len(t, rt.req.Context.Tools.GetTools(), 1)
	require.Equal(t, "nexus__search_memory", rt.req.Context.Tools.GetTools()[0].Name)
}

func TestRuntimeStartErrorMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "budget",
			err:  agentruntime.ErrBudgetExceeded,
			want: "configured cloud runtime budget",
		},
		{
			name: "cloud not allowed",
			err:  agentruntime.ErrCloudNotAllowed,
			want: "cloud runtimes are not allowed",
		},
		{
			name: "local not allowed",
			err:  agentruntime.ErrLocalNotAllowed,
			want: "local runtimes are not allowed",
		},
		{
			name: "workspace not allowed",
			err:  agentruntime.ErrWorkspaceNotAllowed,
			want: "selected workspace is outside",
		},
		{
			name: "approval unsupported",
			err:  agentruntime.ErrApprovalUnsupported,
			want: "does not support external approval decisions yet",
		},
		{
			name: "attachment not allowed",
			err:  workspacefiles.ErrAttachmentNotAllowed,
			want: "attachment was blocked",
		},
		{
			name: "wrapped",
			err:  errors.New("other"),
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runtimeStartErrorMessage(tc.err)
			if tc.want == "" {
				require.Empty(t, got)
				return
			}
			require.Contains(t, got, tc.want)
		})
	}

	require.Contains(t, runtimeStartErrorMessage(errors.Join(errors.New("wrapped"), agentruntime.ErrBudgetExceeded)), "configured cloud runtime budget")
}

func TestRuntimeEventsToTextStreamUploadsCreatedFiles(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "result.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("result"), 0o600))
	payload, err := json.Marshal(agentruntime.RuntimeFileCreatedPayload{Path: filePath})
	require.NoError(t, err)
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeFileCreated, Payload: payload}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)
	client := &runtimeFileUploadClient{}

	stream := runtimeEventsToTextStream(context.Background(), events, workspacefiles.New(workspacefiles.Options{UploadClient: client}), "channel-id", workspace)
	got := drainTextStreamEvents(t, stream)

	require.Equal(t, []llm.EventType{llm.EventTypeFiles, llm.EventTypeEnd}, eventTypes(got))
	require.Equal(t, []string{"uploaded-file-id"}, got[0].Value)
	require.Equal(t, "channel-id", client.channelID)
	require.Equal(t, "result.txt", client.fileName)
	require.Equal(t, "result", client.content)
}

func TestRuntimeEventsToTextStreamUploadsAttachmentDirective(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "directive-result.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("directive result"), 0o600))
	events := make(chan agentruntime.RuntimeEvent, 2)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeTextDelta, Text: "done\nMM_AGENTS_ATTACH:" + filePath + "\n"}
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeCompleted}
	close(events)
	client := &runtimeFileUploadClient{}

	stream := runtimeEventsToTextStream(context.Background(), events, workspacefiles.New(workspacefiles.Options{UploadClient: client}), "channel-id", workspace)
	got := drainTextStreamEvents(t, stream)

	require.Equal(t, []llm.EventType{llm.EventTypeText, llm.EventTypeFiles, llm.EventTypeEnd}, eventTypes(got))
	require.Equal(t, "done\n", got[0].Value)
	require.Equal(t, []string{"uploaded-file-id"}, got[1].Value)
	require.Equal(t, "directive-result.txt", client.fileName)
	require.Equal(t, "directive result", client.content)
}

func TestRuntimeEventsToTextStreamBlocksCreatedFilesOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	payload, err := json.Marshal(agentruntime.RuntimeFileCreatedPayload{Path: outside})
	require.NoError(t, err)
	events := make(chan agentruntime.RuntimeEvent, 1)
	events <- agentruntime.RuntimeEvent{Type: agentruntime.EventTypeFileCreated, Payload: payload}
	close(events)

	stream := runtimeEventsToTextStream(context.Background(), events, workspacefiles.New(workspacefiles.Options{UploadClient: &runtimeFileUploadClient{}}), "channel-id", workspace)
	got := drainTextStreamEvents(t, stream)

	require.Equal(t, []llm.EventType{llm.EventTypeError}, eventTypes(got))
}

type runtimeFileUploadClient struct {
	channelID string
	fileName  string
	content   string
}

func (c *runtimeFileUploadClient) UploadFile(content io.Reader, fileName, channelID string) (*model.FileInfo, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	c.channelID = channelID
	c.fileName = fileName
	c.content = string(body)
	return &model.FileInfo{Id: "uploaded-file-id"}, nil
}
