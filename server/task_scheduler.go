// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/api"
	"github.com/mattermost/mattermost-plugin-agents/v2/mmapi"
	"github.com/mattermost/mattermost-plugin-agents/v2/taskscheduler"
)

const taskSchedulerInterval = 30 * time.Second

func (p *Plugin) reconcileTaskScheduler() {
	if p == nil || p.store == nil || p.runtimeControl == nil {
		return
	}
	if !p.configuration.EnableAgentRuntimeControlPlane() {
		p.stopTaskScheduler()
		return
	}
	if p.taskSchedulerCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.taskSchedulerCancel = cancel
	mmClient := mmapi.NewClient(p.pluginAPI)
	p.taskScheduler = taskscheduler.New(taskscheduler.Options{
		Store:          p.store,
		RuntimeControl: p.runtimeControl,
		ScheduleParser: taskscheduler.DurationScheduleParser{},
		PostClient:     mmClient,
		ThreadReader:   mmClient,
		ServerID:       manifest.Id,
	})
	recoveryStatus := api.RuntimeRecoveryStatus{StartedAt: time.Now().UnixMilli()}
	if p.apiService != nil {
		p.apiService.SetRuntimeRecoveryStatus(recoveryStatus)
	}
	if err := p.runtimeControl.RecoverInterruptedRuns(ctx); err != nil {
		recoveryStatus.RuntimeRecoveryError = err.Error()
		if p.pluginAPI != nil {
			p.pluginAPI.Log.Warn("Failed to recover interrupted runtime runs", "error", err)
		}
	}
	if err := p.taskScheduler.RecoverInterruptedTasks(ctx); err != nil {
		recoveryStatus.TaskRecoveryError = err.Error()
		if p.pluginAPI != nil {
			p.pluginAPI.Log.Warn("Failed to recover interrupted agent tasks", "error", err)
		}
	}
	recoveryStatus.CompletedAt = time.Now().UnixMilli()
	if p.apiService != nil {
		p.apiService.SetRuntimeRecoveryStatus(recoveryStatus)
	}
	p.taskScheduler.Start(ctx, taskSchedulerInterval, func(err error) {
		if p.pluginAPI != nil {
			p.pluginAPI.Log.Error("Failed to run due agent tasks", "error", err)
		}
	})
}

func (p *Plugin) stopTaskScheduler() {
	if p == nil || p.taskSchedulerCancel == nil {
		return
	}
	p.taskSchedulerCancel()
	p.taskSchedulerCancel = nil
	p.taskScheduler = nil
}
