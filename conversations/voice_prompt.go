// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"fmt"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost-plugin-agents/v2/voicebroker"
	"github.com/mattermost/mattermost/server/public/model"
)

type voicePromptResult struct {
	Prompt        string
	HasVoice      bool
	RequiresLocal bool
}

func (c *Conversations) promptWithVoiceTranscripts(ctx context.Context, bot *bots.Bot, postingUser *model.User, channel *model.Channel, post *model.Post) (voicePromptResult, error) {
	if post == nil || len(post.FileIds) == 0 {
		if post == nil {
			return voicePromptResult{}, nil
		}
		return voicePromptResult{Prompt: post.Message}, nil
	}

	requireLocalSTT, err := c.requiresLocalVoiceTranscription(bot, postingUser, channel, post)
	if err != nil {
		return voicePromptResult{}, err
	}
	broker := voicebroker.New(voicebroker.Options{
		Files: c.mmClient,
		TranscriberProvider: func() voicebroker.Transcriber {
			if c.bots == nil {
				return nil
			}
			if requireLocalSTT {
				return c.bots.GetLocalTranscribe()
			}
			return c.bots.GetTranscribe()
		},
	})
	result, err := broker.TranscribePost(ctx, post)
	if err != nil {
		return voicePromptResult{}, err
	}
	return voicePromptResult{
		Prompt:        voicebroker.ComposePrompt(post.Message, result),
		HasVoice:      len(result.Transcripts) > 0,
		RequiresLocal: requireLocalSTT,
	}, nil
}

func (c *Conversations) validateResponseTextToSpeech(requireLocal bool) error {
	if c.textToSpeech == nil || !c.textToSpeech.Enabled() {
		return nil
	}
	if requireLocal && !c.textToSpeech.IsLocal() {
		return fmt.Errorf("text-to-speech provider is not local")
	}
	return nil
}

func (c *Conversations) requiresLocalVoiceTranscription(bot *bots.Bot, postingUser *model.User, channel *model.Channel, post *model.Post) (bool, error) {
	if !c.runtimeControlEnabled() || bot == nil || bot.GetMMBot() == nil || postingUser == nil || channel == nil || post == nil {
		return false, nil
	}
	policy, err := c.runtimeControl.ResolvePolicy(runtimecontrol.EnsureSessionRequest{
		MattermostConversationID: responseRootID(post),
		ChannelID:                channel.Id,
		RootPostID:               responseRootID(post),
		UserID:                   postingUser.Id,
		AgentID:                  bot.GetMMBot().UserId,
		ServerID:                 c.serverID,
		TeamID:                   channel.TeamId,
		IsDM:                     channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup,
	})
	if err != nil {
		return false, err
	}
	return policy.Policy.RuntimeType == agentruntime.RuntimeTypeLocal, nil
}
