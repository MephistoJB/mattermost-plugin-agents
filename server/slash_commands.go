// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"fmt"

	"github.com/mattermost/mattermost-plugin-agents/v2/api"
	"github.com/mattermost/mattermost-plugin-agents/v2/audit"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
	"github.com/mattermost/mattermost-plugin-agents/v2/slashcommands"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

type runtimeCommandPermissions struct {
	client mmapi.Client
}

type runtimeCommandAudit struct {
	pluginAPI *pluginapi.Client
}

func (p runtimeCommandPermissions) CanManageRuntimePolicy(userID, channelID string) bool {
	if p.client == nil || userID == "" || channelID == "" {
		return false
	}
	channel, err := p.client.GetChannel(channelID)
	if err != nil || channel == nil {
		return false
	}
	if !p.client.HasPermissionToChannel(userID, channel.Id, model.PermissionReadChannel) {
		return false
	}
	permission := model.PermissionManagePublicChannelProperties
	if channel.Type == model.ChannelTypePrivate || channel.Type == model.ChannelTypeGroup || channel.Type == model.ChannelTypeDirect {
		permission = model.PermissionManagePrivateChannelProperties
	}
	return p.client.HasPermissionToChannel(userID, channel.Id, permission)
}

func (a runtimeCommandAudit) RecordRuntimePolicyChange(event slashcommands.RuntimePolicyAuditEvent) {
	if a.pluginAPI == nil {
		return
	}
	rec := plugin.MakeAuditRecord(api.AuditEventUpsertRuntimePolicy, model.AuditStatusSuccess)
	rec.Actor.UserId = event.UserID
	model.AddEventParameterToAuditRec(rec, audit.KeyUserID, audit.TruncateID(event.UserID))
	model.AddEventParameterToAuditRec(rec, audit.KeyChannelID, audit.TruncateID(event.ChannelID))
	if event.RootPostID != "" {
		model.AddEventParameterToAuditRec(rec, audit.KeyThreadRootPostID, audit.TruncateID(event.RootPostID))
	}
	model.AddEventParameterToAuditRec(rec, "scope_type", string(event.ScopeType))
	model.AddEventParameterToAuditRec(rec, "scope_id", audit.TruncateID(event.ScopeID))
	model.AddEventParameterToAuditRec(rec, "runtime_type", string(event.RuntimeType))
	model.AddEventParameterToAuditRec(rec, "provider_id", audit.TruncateID(event.ProviderID))
	model.AddEventParameterToAuditRec(rec, "changed_fields", audit.TruncateIDs(event.ChangedFields))
	a.pluginAPI.Audit.Record(rec)
}

func (a runtimeCommandAudit) RecordTaskChange(event slashcommands.TaskAuditEvent) {
	if a.pluginAPI == nil {
		return
	}
	eventName := api.AuditEventUpsertTask
	if event.Action == "delete" {
		eventName = api.AuditEventDeleteTask
	}
	rec := plugin.MakeAuditRecord(eventName, model.AuditStatusSuccess)
	rec.Actor.UserId = event.UserID
	model.AddEventParameterToAuditRec(rec, audit.KeyUserID, audit.TruncateID(event.UserID))
	model.AddEventParameterToAuditRec(rec, audit.KeyChannelID, audit.TruncateID(event.ChannelID))
	if event.RootPostID != "" {
		model.AddEventParameterToAuditRec(rec, audit.KeyThreadRootPostID, audit.TruncateID(event.RootPostID))
	}
	model.AddEventParameterToAuditRec(rec, "task_id", audit.TruncateID(event.TaskID))
	model.AddEventParameterToAuditRec(rec, audit.KeyAgentID, audit.TruncateID(event.AgentID))
	model.AddEventParameterToAuditRec(rec, "task_type", string(event.TaskType))
	model.AddEventParameterToAuditRec(rec, "task_status", string(event.TaskStatus))
	model.AddEventParameterToAuditRec(rec, "task_action", audit.TruncateID(event.Action))
	model.AddEventParameterToAuditRec(rec, "next_run_at", event.NextRunAt)
	model.AddEventParameterToAuditRec(rec, "changed_fields", audit.TruncateIDs(event.ChangedFields))
	a.pluginAPI.Audit.Record(rec)
}

func (p *Plugin) registerSlashCommands() error {
	for _, command := range agentSlashCommands() {
		if err := p.API.RegisterCommand(command); err != nil {
			return fmt.Errorf("failed to register slash command /%s: %w", command.Trigger, err)
		}
	}
	return nil
}

func (p *Plugin) unregisterSlashCommands() {
	for _, command := range agentSlashCommands() {
		if err := p.API.UnregisterCommand("", command.Trigger); err != nil && p.pluginAPI != nil {
			p.pluginAPI.Log.Warn("Failed to unregister slash command", "trigger", command.Trigger, "error", err.Error())
		}
	}
}

func agentSlashCommands() []*model.Command {
	return []*model.Command{
		{
			Trigger:          slashcommands.TriggerRuntime,
			AutoComplete:     true,
			AutoCompleteDesc: "Set or inspect agent runtime for this channel or thread.",
			AutoCompleteHint: "local|cloud|openai|inherit|status",
			DisplayName:      "Agent runtime",
			Description:      "Configure whether an agent uses local or cloud runtime in this scope.",
		},
		{
			Trigger:          slashcommands.TriggerModel,
			AutoComplete:     true,
			AutoCompleteDesc: "Set the model for this channel or thread.",
			AutoCompleteHint: "<model>",
			DisplayName:      "Agent model",
			Description:      "Configure the runtime model in this scope.",
		},
		{
			Trigger:          slashcommands.TriggerNew,
			AutoComplete:     true,
			AutoCompleteDesc: "Create a fresh runtime session.",
			AutoCompleteHint: "[agent_id]",
			DisplayName:      "Agent new session",
			Description:      "Start a new runtime session for this scope.",
		},
		{
			Trigger:          slashcommands.TriggerStop,
			AutoComplete:     true,
			AutoCompleteDesc: "Stop an active runtime session.",
			AutoCompleteHint: "[session_id]",
			DisplayName:      "Agent stop",
			Description:      "Cancel the current or selected runtime session.",
		},
		{
			Trigger:          slashcommands.TriggerApprove,
			AutoComplete:     true,
			AutoCompleteDesc: "Approve a pending runtime action.",
			AutoCompleteHint: "<approval_id> [reason]",
			DisplayName:      "Agent approve",
			Description:      "Approve a pending Codex or local runtime action.",
		},
		{
			Trigger:          slashcommands.TriggerDeny,
			AutoComplete:     true,
			AutoCompleteDesc: "Deny a pending runtime action.",
			AutoCompleteHint: "<approval_id> [reason]",
			DisplayName:      "Agent deny",
			Description:      "Deny a pending Codex or local runtime action.",
		},
		{
			Trigger:          slashcommands.TriggerStatus,
			AutoComplete:     true,
			AutoCompleteDesc: "Show runtime status for this scope.",
			AutoCompleteHint: "[session_id]",
			DisplayName:      "Agent status",
			Description:      "Show current runtime policy and session status.",
		},
		{
			Trigger:          slashcommands.TriggerResume,
			AutoComplete:     true,
			AutoCompleteDesc: "Resume a runtime session.",
			AutoCompleteHint: "[session_id]",
			DisplayName:      "Agent resume",
			Description:      "Resume the current or selected runtime session.",
		},
		{
			Trigger:          slashcommands.TriggerUsage,
			AutoComplete:     true,
			AutoCompleteDesc: "Show runtime usage summary.",
			DisplayName:      "Agent usage",
			Description:      "Summarize runtime sessions by runtime type.",
		},
		{
			Trigger:          slashcommands.TriggerApprovals,
			AutoComplete:     true,
			AutoCompleteDesc: "List pending runtime approvals.",
			DisplayName:      "Agent approvals",
			Description:      "Show runtime sessions waiting for approval.",
		},
		{
			Trigger:          slashcommands.TriggerTask,
			AutoComplete:     true,
			AutoCompleteDesc: "Create autonomous agent tasks and reminders.",
			AutoCompleteHint: "create|reminder|every|run|pause|resume|stop|cancel|delete|snooze",
			DisplayName:      "Agent task",
			Description:      "Create a one-shot task, reminder, or recurring agent task.",
		},
		{
			Trigger:          slashcommands.TriggerTasks,
			AutoComplete:     true,
			AutoCompleteDesc: "List your autonomous agent tasks.",
			DisplayName:      "Agent tasks",
			Description:      "List autonomous agent tasks visible to you.",
		},
		{
			Trigger:          slashcommands.TriggerRemind,
			AutoComplete:     true,
			AutoCompleteDesc: "Create a reminder using natural time syntax.",
			AutoCompleteHint: "[me|channel|thread] in <duration>|tomorrow HH:MM|every friday 15:00 <prompt>",
			DisplayName:      "Agent remind",
			Description:      "Create personal, channel, thread, or recurring agent reminders.",
		},
		{
			Trigger:          slashcommands.TriggerFollowup,
			AutoComplete:     true,
			AutoCompleteDesc: "Create a no-reply follow-up watcher.",
			AutoCompleteHint: "in <duration> if no reply [prompt]",
			DisplayName:      "Agent follow-up",
			Description:      "Create a watcher that can follow up when a thread receives no reply.",
		},
		{
			Trigger:          slashcommands.TriggerSessions,
			AutoComplete:     true,
			AutoCompleteDesc: "List your runtime sessions.",
			DisplayName:      "Agent sessions",
			Description:      "List active and recent agent runtime sessions.",
		},
	}
}
