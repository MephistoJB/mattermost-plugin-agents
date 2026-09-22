// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/audit"
	mmapimocks "github.com/mattermost/mattermost-plugin-agents/v2/mmapi/mocks"
	"github.com/mattermost/mattermost-plugin-agents/v2/slashcommands"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAgentSlashCommands(t *testing.T) {
	commands := agentSlashCommands()
	require.Len(t, commands, 15)

	triggers := []string{}
	for _, command := range commands {
		triggers = append(triggers, command.Trigger)
		assert.True(t, command.AutoComplete)
		assert.NotEmpty(t, command.DisplayName)
		assert.NotEmpty(t, command.Description)
	}

	assert.ElementsMatch(t, []string{
		slashcommands.TriggerRuntime,
		slashcommands.TriggerModel,
		slashcommands.TriggerNew,
		slashcommands.TriggerStop,
		slashcommands.TriggerApprove,
		slashcommands.TriggerDeny,
		slashcommands.TriggerStatus,
		slashcommands.TriggerResume,
		slashcommands.TriggerUsage,
		slashcommands.TriggerApprovals,
		slashcommands.TriggerTask,
		slashcommands.TriggerTasks,
		slashcommands.TriggerRemind,
		slashcommands.TriggerFollowup,
		slashcommands.TriggerSessions,
	}, triggers)
}

func TestRuntimeCommandPermissions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		channelType      model.ChannelType
		managePermission *model.Permission
		canManage        bool
		expected         bool
	}{
		{
			name:             "public channel manager",
			channelType:      model.ChannelTypeOpen,
			managePermission: model.PermissionManagePublicChannelProperties,
			canManage:        true,
			expected:         true,
		},
		{
			name:             "public channel reader only",
			channelType:      model.ChannelTypeOpen,
			managePermission: model.PermissionManagePublicChannelProperties,
			canManage:        false,
			expected:         false,
		},
		{
			name:             "private channel manager",
			channelType:      model.ChannelTypePrivate,
			managePermission: model.PermissionManagePrivateChannelProperties,
			canManage:        true,
			expected:         true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mmapimocks.NewMockClient(t)
			client.EXPECT().GetChannel("channel-1").Return(&model.Channel{Id: "channel-1", Type: tc.channelType}, nil)
			client.EXPECT().HasPermissionToChannel("user-1", "channel-1", model.PermissionReadChannel).Return(true)
			client.EXPECT().HasPermissionToChannel("user-1", "channel-1", tc.managePermission).Return(tc.canManage)

			permissions := runtimeCommandPermissions{client: client}
			assert.Equal(t, tc.expected, permissions.CanManageRuntimePolicy("user-1", "channel-1"))
		})
	}
}

func TestRuntimeCommandAuditRecordsPolicyChange(t *testing.T) {
	mockAPI := &plugintest.API{}
	mockAPI.On("LogAuditRec", mock.Anything).Run(func(args mock.Arguments) {
		rec := args.Get(0).(*model.AuditRecord)
		assert.Equal(t, "upsertRuntimePolicy", rec.EventName)
		assert.Equal(t, model.AuditStatusSuccess, rec.Status)
		assert.Equal(t, "user-1", rec.Actor.UserId)
		assert.Equal(t, "user-1", rec.EventData.Parameters[audit.KeyUserID])
		assert.Equal(t, "channel-1", rec.EventData.Parameters[audit.KeyChannelID])
		assert.Equal(t, "root-1", rec.EventData.Parameters[audit.KeyThreadRootPostID])
		assert.Equal(t, "thread", rec.EventData.Parameters["scope_type"])
		assert.Equal(t, "root-1", rec.EventData.Parameters["scope_id"])
		assert.Equal(t, "local", rec.EventData.Parameters["runtime_type"])
		assert.Equal(t, "local", rec.EventData.Parameters["provider_id"])
		assert.Equal(t, []string{"runtimeType", "providerID"}, rec.EventData.Parameters["changed_fields"])
	}).Once()

	auditLogger := runtimeCommandAudit{pluginAPI: pluginapi.NewClient(mockAPI, nil)}
	auditLogger.RecordRuntimePolicyChange(slashcommands.RuntimePolicyAuditEvent{
		UserID:        "user-1",
		ChannelID:     "channel-1",
		RootPostID:    "root-1",
		ScopeType:     agentruntime.PolicyScopeThread,
		ScopeID:       "root-1",
		RuntimeType:   agentruntime.RuntimeTypeLocal,
		ProviderID:    "local",
		ChangedFields: []string{"runtimeType", "providerID"},
	})

	mockAPI.AssertExpectations(t)
}

func TestRuntimeCommandAuditRecordsTaskChange(t *testing.T) {
	mockAPI := &plugintest.API{}
	mockAPI.On("LogAuditRec", mock.Anything).Run(func(args mock.Arguments) {
		rec := args.Get(0).(*model.AuditRecord)
		assert.Equal(t, "upsertTask", rec.EventName)
		assert.Equal(t, model.AuditStatusSuccess, rec.Status)
		assert.Equal(t, "user-1", rec.Actor.UserId)
		assert.Equal(t, "user-1", rec.EventData.Parameters[audit.KeyUserID])
		assert.Equal(t, "channel-1", rec.EventData.Parameters[audit.KeyChannelID])
		assert.Equal(t, "root-1", rec.EventData.Parameters[audit.KeyThreadRootPostID])
		assert.Equal(t, "task-1", rec.EventData.Parameters["task_id"])
		assert.Equal(t, "agent-1", rec.EventData.Parameters[audit.KeyAgentID])
		assert.Equal(t, "reminder", rec.EventData.Parameters["task_type"])
		assert.Equal(t, "queued", rec.EventData.Parameters["task_status"])
		assert.Equal(t, "snooze", rec.EventData.Parameters["task_action"])
		assert.Equal(t, int64(2000), rec.EventData.Parameters["next_run_at"])
		assert.Equal(t, []string{"status", "nextRunAt"}, rec.EventData.Parameters["changed_fields"])
	}).Once()

	auditLogger := runtimeCommandAudit{pluginAPI: pluginapi.NewClient(mockAPI, nil)}
	auditLogger.RecordTaskChange(slashcommands.TaskAuditEvent{
		UserID:        "user-1",
		ChannelID:     "channel-1",
		RootPostID:    "root-1",
		TaskID:        "task-1",
		TaskType:      agentruntime.TaskTypeReminder,
		TaskStatus:    agentruntime.TaskStatusQueued,
		AgentID:       "agent-1",
		Action:        "snooze",
		NextRunAt:     2000,
		ChangedFields: []string{"status", "nextRunAt"},
	})

	mockAPI.AssertExpectations(t)
}

func TestRuntimeCommandAuditRecordsTaskDelete(t *testing.T) {
	mockAPI := &plugintest.API{}
	mockAPI.On("LogAuditRec", mock.Anything).Run(func(args mock.Arguments) {
		rec := args.Get(0).(*model.AuditRecord)
		assert.Equal(t, "deleteTask", rec.EventName)
		assert.Equal(t, "delete", rec.EventData.Parameters["task_action"])
	}).Once()

	auditLogger := runtimeCommandAudit{pluginAPI: pluginapi.NewClient(mockAPI, nil)}
	auditLogger.RecordTaskChange(slashcommands.TaskAuditEvent{
		UserID:    "user-1",
		ChannelID: "channel-1",
		TaskID:    "task-1",
		Action:    "delete",
	})

	mockAPI.AssertExpectations(t)
}
