// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/format"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	channelHistoryPosts = 30
	channelHistoryChars = 12000
)

type channelMembershipChecker interface {
	IsChannelMember(channelID, userID string) (bool, error)
}

type channelMessage struct {
	ctx     context.Context
	bot     *bots.Bot
	post    *model.Post
	user    *model.User
	channel *model.Channel
}

func (c *Conversations) channelBotIsMember(channel *model.Channel, bot *bots.Bot) bool {
	if !c.runtimeControlEnabled() || channel == nil || bot == nil || bot.GetMMBot() == nil ||
		(channel.Type != model.ChannelTypeOpen && channel.Type != model.ChannelTypePrivate) {
		return false
	}
	checker, ok := c.mmClient.(channelMembershipChecker)
	if !ok {
		return false
	}
	member, err := checker.IsChannelMember(channel.Id, bot.GetMMBot().UserId)
	if err != nil {
		c.mmClient.LogError("Unable to check agent channel membership", "channel_id", channel.Id, "agent_id", bot.GetMMBot().UserId, "error", err)
		return false
	}
	return member
}

func (c *Conversations) enqueueChannelMessage(ctx context.Context, bot *bots.Bot, post *model.Post, user *model.User, channel *model.Channel) error {
	if err := c.bots.CheckUsageRestrictions(user.Id, bot, channel); err != nil {
		return err
	}
	key := channel.Id + ":" + bot.GetMMBot().UserId
	c.channelQueueMu.Lock()
	if c.channelQueues == nil {
		c.channelQueues = make(map[string]chan channelMessage)
	}
	queue := c.channelQueues[key]
	if queue == nil {
		queue = make(chan channelMessage, 256)
		c.channelQueues[key] = queue
		go c.runChannelQueue(queue)
	}
	c.channelQueueMu.Unlock()
	queue <- channelMessage{ctx: ctx, bot: bot, post: post.Clone(), user: user, channel: channel}
	return nil
}

func (c *Conversations) runChannelQueue(queue <-chan channelMessage) {
	for message := range queue {
		if !c.channelBotIsMember(message.channel, message.bot) {
			continue
		}
		if err := c.handleChannelMessage(message); err != nil {
			c.mmClient.LogError("Unable to handle agent channel message", "channel_id", message.channel.Id, "post_id", message.post.Id, "error", err)
		}
	}
}

func (c *Conversations) handleChannelMessage(message channelMessage) error {
	bot, post, user, channel := message.bot, message.post, message.user, message.channel
	voicePrompt, err := c.promptWithVoiceTranscripts(message.ctx, bot, user, channel, post)
	if err != nil {
		return fmt.Errorf("failed to prepare channel message: %w", err)
	}
	if err := c.validateResponseTextToSpeech(voicePrompt.RequiresLocal); err != nil {
		return err
	}
	initialContext := c.channelInitialContext(channel, post)
	llmContext := c.buildConversationContextWithTools(
		message.ctx,
		bot, user, channel,
		"",
		c.contextBuilder.WithLLMContextResponseFiles(),
	)
	responsePost := &model.Post{ChannelId: channel.Id}
	if err := c.createResponsePlaceholder(bot.GetMMBot().UserId, user.Id, responsePost, post.Id); err != nil {
		return fmt.Errorf("unable to create channel response: %w", err)
	}
	stream, err := c.startRuntimeTurn(message.ctx, bot, post, user, channel,
		"channel:"+channel.Id, format.AuthoredPost(&model.Post{Message: voicePrompt.Prompt}, user.Username),
		llmContext, voicePrompt.HasVoice, runtimeTurnOptions{ChannelConversation: true, InitialContext: initialContext})
	if err != nil {
		c.failRuntimeStartPlaceholder(responsePost, user.Locale, err)
		return fmt.Errorf("unable to start channel runtime turn: %w", err)
	}
	streamCtx, err := c.streamingService.GetStreamingContext(message.ctx, responsePost.Id)
	if err != nil {
		c.failResponsePlaceholder(responsePost, user.Locale)
		return err
	}
	defer c.streamingService.FinishStreaming(responsePost.Id)
	c.streamingService.StreamToPost(streamCtx, stream, responsePost, c.responseLocale(user, channel), user.Id)
	return nil
}

// channelInitialContext captures a bounded snapshot before the first turn.
// The runtime sends it only when creating the channel session.
func (c *Conversations) channelInitialContext(channel *model.Channel, before *model.Post) string {
	posts, err := c.mmClient.GetPostsBefore(channel.Id, before.Id, 0, channelHistoryPosts)
	if err != nil {
		c.mmClient.LogWarn("Unable to load channel history", "channel_id", channel.Id, "error", err)
		return ""
	}
	if posts == nil || len(posts.Order) == 0 {
		return ""
	}
	var entries []string
	for i := len(posts.Order) - 1; i >= 0; i-- {
		post := posts.Posts[posts.Order[i]]
		if post == nil || post.IsSystemMessage() || post.DeleteAt != 0 {
			continue
		}
		user, userErr := c.mmClient.GetUser(post.UserId)
		if userErr != nil || user == nil {
			continue
		}
		entries = append(entries, format.AuthoredPost(post, user.Username))
	}
	joined := strings.Join(entries, "\n")
	if len(joined) > channelHistoryChars {
		joined = joined[len(joined)-channelHistoryChars:]
	}
	if joined == "" {
		return ""
	}
	return "Previous channel messages (context only; respond to the latest message below):\n" + joined
}
