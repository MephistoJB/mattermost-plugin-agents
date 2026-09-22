// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/bots"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/llmruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost-plugin-agents/v2/workspacefiles"
	"github.com/mattermost/mattermost/server/public/model"
)

func (c *Conversations) runtimeControlEnabled() bool {
	return c != nil &&
		c.runtimeControl != nil &&
		c.configProvider != nil &&
		c.configProvider.EnableAgentRuntimeControlPlane()
}

func (c *Conversations) startRuntimeTurn(
	ctx context.Context,
	bot *bots.Bot,
	post *model.Post,
	postingUser *model.User,
	channel *model.Channel,
	conversationID string,
	prompt string,
	llmContext *llm.Context,
	skipAttachments bool,
	options ...runtimeTurnOptions,
) (*llm.TextStreamResult, error) {
	if !c.runtimeControlEnabled() {
		return nil, errors.New("runtime control is not enabled")
	}
	if bot == nil || bot.GetMMBot() == nil {
		return nil, errors.New("bot is required")
	}
	if post == nil || postingUser == nil || channel == nil {
		return nil, errors.New("post, posting user, and channel are required")
	}

	c.runtimeControl.RegisterRuntimeIfAbsent(agentruntime.RuntimeTypeOpenAI, llmruntime.New(llmruntime.Options{
		LLM:         bot.LLM(),
		RuntimeType: agentruntime.RuntimeTypeOpenAI,
		ProviderID:  "openai",
	}))

	attachments := runtimeAttachmentsFromPost(post)
	if skipAttachments {
		attachments = nil
	}
	var opts runtimeTurnOptions
	if len(options) > 0 {
		opts = options[0]
	}
	rootPostID := responseRootID(post)
	if opts.ChannelConversation {
		rootPostID = ""
	}
	isDM := channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup
	req := runtimecontrol.StartTurnRequest{
		SessionRequest: runtimecontrol.EnsureSessionRequest{
			MattermostConversationID: conversationID,
			ChannelID:                channel.Id,
			RootPostID:               rootPostID,
			UserID:                   postingUser.Id,
			AgentID:                  bot.GetMMBot().UserId,
			ServerID:                 c.serverID,
			TeamID:                   channel.TeamId,
			IsDM:                     isDM,
		},
		Prompt:            prompt,
		Context:           llmContext,
		ShouldExecuteTool: c.shouldAutoExecuteTool(llmContext, isDM),
		InitialContext:    opts.InitialContext,
		Attachments:       attachments,
	}
	var result *runtimecontrol.StartTurnResult
	var err error
	if bot.GetConfig().SupervisorMode {
		result, err = c.runtimeControl.StartSupervisorTurn(ctx, req)
	} else {
		result, err = c.runtimeControl.StartTurn(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	return runtimeEventsToTextStream(ctx, result.Events, workspacefiles.New(workspacefiles.Options{Client: c.mmClient}), channel.Id, result.Session.WorkspacePath), nil
}

type runtimeTurnOptions struct {
	ChannelConversation bool
	InitialContext      string
}

func runtimeEventsToTextStream(ctx context.Context, events <-chan agentruntime.RuntimeEvent, fileBroker *workspacefiles.Service, channelID, workspacePath string) *llm.TextStreamResult {
	stream := make(chan llm.TextStreamEvent)
	go func() {
		defer close(stream)
		approvalNoticeSent := false
		sendApprovalNotice := func() {
			if approvalNoticeSent {
				return
			}
			approvalNoticeSent = true
			stream <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: "Waiting for approval before continuing."}
		}
		for event := range events {
			switch event.Type {
			case agentruntime.EventTypeTextDelta:
				if event.Text != "" {
					text, files := runtimeAttachmentDirectives(event.Text)
					if text != "" {
						stream <- llm.TextStreamEvent{Type: llm.EventTypeText, Value: text}
					}
					for _, file := range files {
						fileID, err := uploadRuntimeFile(ctx, fileBroker, channelID, workspacePath, agentruntime.RuntimeFileCreatedPayload{Path: file})
						if err != nil {
							stream <- runtimeErrorEvent(err)
							return
						}
						if fileID != "" {
							stream <- llm.TextStreamEvent{Type: llm.EventTypeFiles, Value: []string{fileID}}
						}
					}
				}
			case agentruntime.EventTypeReasoningDelta:
				if event.Text != "" {
					stream <- llm.TextStreamEvent{Type: llm.EventTypeReasoning, Value: event.Text}
				}
			case agentruntime.EventTypeFileCreated:
				payload, err := workspacefiles.PayloadFromEvent(event)
				if err != nil {
					stream <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: err}
					return
				}
				fileID, err := uploadRuntimeFile(ctx, fileBroker, channelID, workspacePath, payload)
				if err != nil {
					stream <- runtimeErrorEvent(err)
					return
				}
				if fileID != "" {
					stream <- llm.TextStreamEvent{Type: llm.EventTypeFiles, Value: []string{fileID}}
				}
			case agentruntime.EventTypeApprovalRequested:
				sendApprovalNotice()
			case agentruntime.EventTypeStatus:
				if event.Text == string(agentruntime.SessionStatusWaitingApproval) {
					sendApprovalNotice()
				}
			case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
				if event.Err != nil {
					stream <- runtimeErrorEvent(event.Err)
				} else {
					stream <- llm.TextStreamEvent{Type: llm.EventTypeError, Value: event.Text}
				}
				for range events {
				}
				return
			case agentruntime.EventTypeCompleted, agentruntime.EventTypeSubagentCompleted, agentruntime.EventTypeCancelled:
				for range events {
				}
				stream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
				return
			}
		}
		stream <- llm.TextStreamEvent{Type: llm.EventTypeEnd}
	}()
	return &llm.TextStreamResult{Stream: stream}
}

func uploadRuntimeFile(ctx context.Context, fileBroker *workspacefiles.Service, channelID, workspacePath string, payload agentruntime.RuntimeFileCreatedPayload) (string, error) {
	if fileBroker == nil || !fileBroker.Enabled() {
		return "", nil
	}
	return fileBroker.UploadRuntimeFile(ctx, channelID, workspacePath, payload)
}

func runtimeAttachmentDirectives(text string) (string, []string) {
	if !strings.Contains(text, "MM_AGENTS_ATTACH:") {
		return text, nil
	}
	lines := strings.SplitAfter(text, "\n")
	cleaned := strings.Builder{}
	files := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "MM_AGENTS_ATTACH:") {
			path := strings.TrimSpace(strings.TrimPrefix(trimmed, "MM_AGENTS_ATTACH:"))
			if path != "" {
				files = append(files, path)
			}
			continue
		}
		cleaned.WriteString(line)
	}
	return cleaned.String(), files
}

func runtimeErrorEvent(err error) llm.TextStreamEvent {
	if message := runtimeUserErrorMessage(err); message != "" {
		return llm.TextStreamEvent{Type: llm.EventTypeError, Value: message}
	}
	return llm.TextStreamEvent{Type: llm.EventTypeError, Value: err}
}

func runtimeUserErrorMessage(err error) string {
	switch {
	case errors.Is(err, agentruntime.ErrBudgetExceeded):
		return "This request was not started because the configured cloud runtime budget for this scope has been reached."
	case errors.Is(err, agentruntime.ErrCloudNotAllowed):
		return "This request was not started because cloud runtimes are not allowed in this scope."
	case errors.Is(err, agentruntime.ErrLocalNotAllowed):
		return "This request was not started because local runtimes are not allowed in this scope."
	case errors.Is(err, agentruntime.ErrWorkspaceNotAllowed):
		return "This request was not started because the selected workspace is outside the configured workspace policy."
	case errors.Is(err, agentruntime.ErrApprovalUnsupported):
		return "This approval cannot be completed because the selected runtime does not support external approval decisions yet."
	case errors.Is(err, workspacefiles.ErrFileNotAllowed):
		return "The runtime produced a file outside the configured workspace, so it was not attached."
	case errors.Is(err, workspacefiles.ErrAttachmentNotAllowed):
		return "An attachment was blocked by the runtime attachment policy."
	default:
		return ""
	}
}

func runtimeAttachmentsFromPost(post *model.Post) []agentruntime.RuntimeAttachment {
	if post == nil || len(post.FileIds) == 0 {
		return nil
	}
	attachments := make([]agentruntime.RuntimeAttachment, 0, len(post.FileIds))
	for _, fileID := range post.FileIds {
		attachments = append(attachments, agentruntime.RuntimeAttachment{FileID: fileID})
	}
	return attachments
}

func responseRootID(post *model.Post) string {
	if post == nil {
		return ""
	}
	if post.RootId != "" {
		return post.RootId
	}
	return post.Id
}

func runtimeMetadata(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
