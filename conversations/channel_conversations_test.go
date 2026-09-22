// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
	"github.com/mattermost/mattermost-plugin-agents/v2/streaming"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

type channelTestClient struct {
	mmapi.Client
	members map[string]bool
	posts   []*model.Post
	users   map[string]*model.User
	history *model.PostList
}

func (c *channelTestClient) IsChannelMember(channelID, userID string) (bool, error) {
	return c.members[channelID+":"+userID], nil
}

func (c *channelTestClient) CreatePost(post *model.Post) error {
	post.Id = "response-id"
	c.posts = append(c.posts, post.Clone())
	return nil
}

func (c *channelTestClient) GetPostsBefore(string, string, int, int) (*model.PostList, error) {
	return c.history, nil
}

func (c *channelTestClient) GetUser(id string) (*model.User, error) {
	return c.users[id], nil
}

func (c *channelTestClient) GetConfig() *model.Config { return nil }

type channelTestStreaming struct {
	streaming.Service
	post *model.Post
}

func (s *channelTestStreaming) GetStreamingContext(ctx context.Context, _ string) (context.Context, error) {
	return ctx, nil
}

func (s *channelTestStreaming) FinishStreaming(string) {}

func (s *channelTestStreaming) StreamToPost(_ context.Context, stream *llm.TextStreamResult, post *model.Post, _ string, _ string) {
	s.post = post.Clone()
	for range stream.Stream {
	}
}

func TestJoinedChannelResponseIsTopLevelAndUsesChannelSession(t *testing.T) {
	client := &channelTestClient{users: map[string]*model.User{}}
	streamer := &channelTestStreaming{}
	runtime := &runtimeTurnControl{}
	c := &Conversations{mmClient: client, streamingService: streamer, runtimeControl: runtime, configProvider: runtimeControlTestConfig{}}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-id"}, nil)
	user := &model.User{Id: "user-id", Username: "alice", Locale: "en"}
	channel := &model.Channel{Id: "channel-id", Type: model.ChannelTypeOpen}
	post := &model.Post{Id: "source-id", ChannelId: channel.Id, Message: "hello"}

	err := c.handleChannelMessage(channelMessage{ctx: context.Background(), bot: bot, post: post, user: user, channel: channel})
	require.NoError(t, err)
	require.Len(t, client.posts, 1)
	require.Empty(t, client.posts[0].RootId)
	require.Empty(t, streamer.post.RootId)
	require.Equal(t, "channel:channel-id", runtime.req.SessionRequest.MattermostConversationID)
	require.Empty(t, runtime.req.SessionRequest.RootPostID)
	require.Equal(t, "@alice: hello", runtime.req.Prompt)
}

func TestChannelMembershipControlsAutomaticResponse(t *testing.T) {
	client := &channelTestClient{members: map[string]bool{"channel-id:bot-id": true}}
	c := &Conversations{mmClient: client, runtimeControl: &runtimeTurnControl{}, configProvider: runtimeControlTestConfig{}}
	bot := bots.NewBot(llm.BotConfig{}, llm.ServiceConfig{}, &model.Bot{UserId: "bot-id"}, nil)
	channel := &model.Channel{Id: "channel-id", Type: model.ChannelTypeOpen}
	require.True(t, c.channelBotIsMember(channel, bot))
	delete(client.members, "channel-id:bot-id")
	require.False(t, c.channelBotIsMember(channel, bot))
	channel.Type = model.ChannelTypeDirect
	client.members["channel-id:bot-id"] = true
	require.False(t, c.channelBotIsMember(channel, bot))
}

func TestInitialChannelContextIsBoundedAndInOrder(t *testing.T) {
	client := &channelTestClient{
		users: map[string]*model.User{"alice": {Id: "alice", Username: "alice"}},
		history: &model.PostList{Order: []string{"new", "old"}, Posts: map[string]*model.Post{
			"new": {Id: "new", UserId: "alice", Message: "second"},
			"old": {Id: "old", UserId: "alice", Message: "first"},
		}},
	}
	c := &Conversations{mmClient: client}
	got := c.channelInitialContext(&model.Channel{Id: "channel-id"}, &model.Post{Id: "current"})
	require.Contains(t, got, "@alice: first\n@alice: second")
	require.NotContains(t, got, "current")
}
