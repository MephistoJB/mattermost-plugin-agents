// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/mcp"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type voicePromptConfig struct {
	enabled bool
}

func (c voicePromptConfig) EnableChannelMentionToolCalling() bool {
	return false
}

func (c voicePromptConfig) AllowNativeWebSearchInChannels() bool {
	return false
}

func (c voicePromptConfig) EnableAgentRuntimeControlPlane() bool {
	return c.enabled
}

func (c voicePromptConfig) MCP() mcp.Config {
	return mcp.Config{}
}

type voicePromptRuntimeControl struct {
	policy agentruntime.EffectivePolicy
	req    runtimecontrol.EnsureSessionRequest
}

func (r *voicePromptRuntimeControl) RegisterRuntime(agentruntime.RuntimeType, agentruntime.AgentRuntime) {
}

func (r *voicePromptRuntimeControl) RegisterRuntimeIfAbsent(agentruntime.RuntimeType, agentruntime.AgentRuntime) {
}

func (r *voicePromptRuntimeControl) ResolvePolicy(req runtimecontrol.EnsureSessionRequest) (agentruntime.EffectivePolicy, error) {
	r.req = req
	return r.policy, nil
}

func (r *voicePromptRuntimeControl) StartTurn(context.Context, runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error) {
	return nil, nil
}

func (r *voicePromptRuntimeControl) StartSupervisorTurn(context.Context, runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error) {
	return nil, nil
}

func TestRequiresLocalVoiceTranscriptionForLocalRuntimePolicy(t *testing.T) {
	rt := &voicePromptRuntimeControl{
		policy: agentruntime.EffectivePolicy{
			Policy: agentruntime.RuntimePolicy{RuntimeType: agentruntime.RuntimeTypeLocal},
		},
	}
	conversations := &Conversations{
		configProvider: voicePromptConfig{enabled: true},
		runtimeControl: rt,
		serverID:       "server-1",
	}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-user-id"}, nil)
	user := &model.User{Id: "user-id"}
	channel := &model.Channel{Id: "channel-id", TeamId: "team-id", Type: model.ChannelTypeOpen}
	post := &model.Post{Id: "post-id", ChannelId: "channel-id", RootId: "root-id"}

	requireLocal, err := conversations.requiresLocalVoiceTranscription(bot, user, channel, post)

	require.NoError(t, err)
	assert.True(t, requireLocal)
	assert.Equal(t, "channel-id", rt.req.ChannelID)
	assert.Equal(t, "root-id", rt.req.RootPostID)
	assert.Equal(t, "user-id", rt.req.UserID)
	assert.Equal(t, "bot-user-id", rt.req.AgentID)
	assert.Equal(t, "server-1", rt.req.ServerID)
	assert.Equal(t, "team-id", rt.req.TeamID)
}

func TestRequiresLocalVoiceTranscriptionSkipsWhenRuntimeControlDisabled(t *testing.T) {
	conversations := &Conversations{configProvider: voicePromptConfig{enabled: false}}

	requireLocal, err := conversations.requiresLocalVoiceTranscription(nil, nil, nil, nil)

	require.NoError(t, err)
	assert.False(t, requireLocal)
}
