// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package taskscheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
	"github.com/mattermost/mattermost-plugin-agents/v2/runtimecontrol"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost/server/public/model"
)

const DefaultBatchSize = 10
const interruptedTaskRecoveryStaleAfter = 2 * time.Minute

type Store interface {
	ClaimDueTasks(now int64, limit int) ([]agentruntime.Task, error)
	ListTasks(filter store.TaskFilter) ([]agentruntime.Task, error)
	ListTaskRuns(taskID string) ([]agentruntime.TaskRun, error)
	CreateTaskRun(run *agentruntime.TaskRun) error
	UpdateTaskRunRuntimeSession(id, runtimeSessionID string) error
	UpdateTaskRunResult(id string, status agentruntime.TaskStatus, resultPostID, errorText string, usage json.RawMessage) error
	UpdateTaskStatus(id string, status agentruntime.TaskStatus, lastRunAt, nextRunAt int64) error
}

type RuntimeControl interface {
	StartTurn(ctx context.Context, req runtimecontrol.StartTurnRequest) (*runtimecontrol.StartTurnResult, error)
}

type ScheduleParser interface {
	NextRunAfter(scheduleSpec string, after time.Time) (time.Time, bool)
}

type PostClient interface {
	CreatePost(post *model.Post) error
}

type ThreadReader interface {
	GetPostThread(postID string) (*model.PostList, error)
}

type Service struct {
	store          Store
	runtimeControl RuntimeControl
	scheduleParser ScheduleParser
	postClient     PostClient
	threadReader   ThreadReader
	now            func() time.Time
	batchSize      int
	serverID       string
	teamID         string
}

type Options struct {
	Store          Store
	RuntimeControl RuntimeControl
	ScheduleParser ScheduleParser
	PostClient     PostClient
	ThreadReader   ThreadReader
	Now            func() time.Time
	BatchSize      int
	ServerID       string
	TeamID         string
}

func New(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	return &Service{
		store:          opts.Store,
		runtimeControl: opts.RuntimeControl,
		scheduleParser: opts.ScheduleParser,
		postClient:     opts.PostClient,
		threadReader:   opts.ThreadReader,
		now:            now,
		batchSize:      batchSize,
		serverID:       opts.ServerID,
		teamID:         opts.TeamID,
	}
}

func (s *Service) Start(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	go func() {
		s.runDueAndReport(ctx, onError)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDueAndReport(ctx, onError)
			}
		}
	}()
}

func (s *Service) RunDueOnce(ctx context.Context) (int, error) {
	if s == nil || s.store == nil {
		return 0, fmt.Errorf("task scheduler store is not configured")
	}
	if s.runtimeControl == nil {
		return 0, fmt.Errorf("task scheduler runtime control is not configured")
	}

	nowMillis := s.now().UnixMilli()
	tasks, err := s.store.ClaimDueTasks(nowMillis, s.batchSize)
	if err != nil {
		return 0, err
	}

	for _, task := range tasks {
		if err := s.runTask(ctx, task, nowMillis); err != nil {
			return 0, err
		}
	}
	return len(tasks), nil
}

func (s *Service) RecoverInterruptedTasks(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("task scheduler store is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	nowMillis := s.now().UnixMilli()
	cutoff := nowMillis - interruptedTaskRecoveryStaleAfter.Milliseconds()
	var errs []error
	for _, status := range []agentruntime.TaskStatus{agentruntime.TaskStatusRunning, agentruntime.TaskStatusWaitingApproval} {
		tasks, err := s.store.ListTasks(store.TaskFilter{Status: status})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to list interrupted tasks: %w", err))
			continue
		}
		for _, task := range tasks {
			if !staleTask(task, cutoff) {
				continue
			}
			if err := s.failInterruptedTask(task, nowMillis); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) runDueAndReport(ctx context.Context, onError func(error)) {
	if _, err := s.RunDueOnce(ctx); err != nil && onError != nil && ctx.Err() == nil {
		onError(err)
	}
}

func (s *Service) failInterruptedTask(task agentruntime.Task, nowMillis int64) error {
	var errs []error
	runs, err := s.store.ListTaskRuns(task.ID)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to list interrupted task runs for task %s: %w", task.ID, err))
	} else if len(runs) > 0 && activeTaskStatus(runs[0].Status) && staleTaskRun(runs[0], nowMillis-interruptedTaskRecoveryStaleAfter.Milliseconds()) {
		usage := json.RawMessage(`{"interrupted":true,"reason":"plugin_restart"}`)
		if err := s.store.UpdateTaskRunResult(runs[0].ID, agentruntime.TaskStatusFailed, runs[0].ResultPostID, "interrupted by plugin restart", usage); err != nil {
			errs = append(errs, fmt.Errorf("failed to mark task run %s failed: %w", runs[0].ID, err))
		}
	}

	if err := s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1); err != nil {
		errs = append(errs, fmt.Errorf("failed to mark task %s failed: %w", task.ID, err))
	}
	return errors.Join(errs...)
}

func staleTask(task agentruntime.Task, cutoff int64) bool {
	return task.UpdatedAt == 0 || task.UpdatedAt <= cutoff
}

func staleTaskRun(run agentruntime.TaskRun, cutoff int64) bool {
	return run.StartedAt == 0 || run.StartedAt <= cutoff
}

func activeTaskStatus(status agentruntime.TaskStatus) bool {
	return status == agentruntime.TaskStatusRunning || status == agentruntime.TaskStatusWaitingApproval
}

func (s *Service) runTask(ctx context.Context, task agentruntime.Task, nowMillis int64) error {
	run := &agentruntime.TaskRun{
		TaskID: task.ID,
		Status: agentruntime.TaskStatusRunning,
	}
	if err := s.store.CreateTaskRun(run); err != nil {
		return fmt.Errorf("failed to create task run: %w", err)
	}

	shouldRun, err := s.shouldRunTask(task)
	if err != nil {
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusFailed, "", sanitizeTaskErrorMessage(err), nil)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1)
		return nil
	}
	if !shouldRun {
		usage := json.RawMessage(`{"skipped":true,"reason":"reply_detected"}`)
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusCompleted, "", "", usage)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusCompleted, nowMillis, 0)
		return nil
	}

	policyOverride, err := runtimePolicyFromSnapshot(task.RuntimePolicySnapshot)
	if err != nil {
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusFailed, "", sanitizeTaskErrorMessage(err), nil)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1)
		return nil
	}

	result, err := s.runtimeControl.StartTurn(ctx, runtimecontrol.StartTurnRequest{
		SessionRequest: runtimecontrol.EnsureSessionRequest{
			MattermostConversationID: task.ID,
			ChannelID:                task.ChannelID,
			RootPostID:               task.RootPostID,
			UserID:                   task.UserID,
			AgentID:                  task.AgentID,
			ServerID:                 s.serverID,
			TeamID:                   s.teamID,
			WorkspacePath:            task.WorkspacePath,
			PolicyOverride:           policyOverride,
		},
		Prompt:   task.Prompt,
		Metadata: task.Metadata,
	})
	if err != nil {
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusFailed, "", sanitizeTaskErrorMessage(err), nil)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1)
		return nil
	}

	run.RuntimeSessionID = result.Session.ID
	if err := s.store.UpdateTaskRunRuntimeSession(run.ID, result.Session.ID); err != nil {
		return fmt.Errorf("failed to attach runtime session to task run: %w", err)
	}
	output, err := collectRuntimeOutput(result.Events)
	if err != nil {
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusFailed, "", sanitizeTaskErrorMessage(err), nil)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1)
		return nil
	}
	resultPostID, err := s.postTaskResult(task, output)
	if err != nil {
		_ = s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusFailed, "", sanitizeTaskErrorMessage(err), nil)
		_ = s.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusFailed, nowMillis, -1)
		return nil
	}

	nextRunAt := int64(0)
	nextStatus := agentruntime.TaskStatusCompleted
	if task.TaskType == agentruntime.TaskTypeRecurring || (task.TaskType == agentruntime.TaskTypeWatcher && task.ScheduleSpec != "") {
		nextRunAt, nextStatus = s.nextScheduledRun(task, nowMillis)
	}

	if err := s.store.UpdateTaskRunResult(run.ID, agentruntime.TaskStatusCompleted, resultPostID, "", nil); err != nil {
		return fmt.Errorf("failed to complete task run: %w", err)
	}
	if err := s.store.UpdateTaskStatus(task.ID, nextStatus, nowMillis, nextRunAt); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	return nil
}

func (s *Service) shouldRunTask(task agentruntime.Task) (bool, error) {
	if task.TaskType != agentruntime.TaskTypeWatcher {
		return true, nil
	}
	var metadata struct {
		Kind       string `json:"kind"`
		Condition  string `json:"condition"`
		RootPostID string `json:"rootPostID"`
	}
	if len(task.Metadata) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(task.Metadata, &metadata); err != nil {
		return false, fmt.Errorf("failed to decode watcher metadata: %w", err)
	}
	if metadata.Kind != "followup" || metadata.Condition != "no_reply" {
		return true, nil
	}
	if s.threadReader == nil {
		return true, nil
	}
	rootPostID := metadata.RootPostID
	if rootPostID == "" {
		rootPostID = task.RootPostID
	}
	if rootPostID == "" {
		return true, nil
	}
	thread, err := s.threadReader.GetPostThread(rootPostID)
	if err != nil {
		return false, fmt.Errorf("failed to inspect follow-up thread: %w", err)
	}
	for _, postID := range thread.Order {
		post := thread.Posts[postID]
		if post == nil || post.DeleteAt > 0 || post.CreateAt <= task.CreatedAt {
			continue
		}
		if post.UserId != "" && post.UserId != task.AgentID {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) postTaskResult(task agentruntime.Task, output string) (string, error) {
	if s.postClient == nil || task.ChannelID == "" {
		return "", nil
	}

	message := strings.TrimSpace(output)
	if message == "" {
		message = fmt.Sprintf("Task `%s` completed.", task.Title)
	}
	post := &model.Post{
		UserId:    task.AgentID,
		ChannelId: task.ChannelID,
		RootId:    task.RootPostID,
		Message:   message,
	}
	if err := s.postClient.CreatePost(post); err != nil {
		return "", fmt.Errorf("failed to post task result: %w", err)
	}
	return post.Id, nil
}

func runtimePolicyFromSnapshot(snapshot json.RawMessage) (*agentruntime.RuntimePolicy, error) {
	if len(snapshot) == 0 || string(snapshot) == "{}" {
		return nil, nil
	}
	var policy agentruntime.RuntimePolicy
	if err := json.Unmarshal(snapshot, &policy); err != nil {
		return nil, fmt.Errorf("failed to decode runtime policy snapshot: %w", err)
	}
	if policy.RuntimeType == "" || policy.RuntimeType == agentruntime.RuntimeTypeInherit {
		return nil, nil
	}
	return &policy, nil
}

func (s *Service) nextScheduledRun(task agentruntime.Task, nowMillis int64) (int64, agentruntime.TaskStatus) {
	if s.scheduleParser == nil || task.ScheduleSpec == "" {
		return 0, agentruntime.TaskStatusPaused
	}
	next, ok := s.scheduleParser.NextRunAfter(task.ScheduleSpec, time.UnixMilli(nowMillis))
	if !ok {
		return 0, agentruntime.TaskStatusPaused
	}
	return next.UnixMilli(), agentruntime.TaskStatusQueued
}

func collectRuntimeOutput(events <-chan agentruntime.RuntimeEvent) (string, error) {
	var output strings.Builder
	for event := range events {
		switch event.Type {
		case agentruntime.EventTypeTextDelta:
			output.WriteString(event.Text)
		case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
			if event.Err != nil {
				return "", event.Err
			}
			return "", fmt.Errorf("runtime failed")
		case agentruntime.EventTypeCancelled:
			return "", context.Canceled
		}
	}
	return output.String(), nil
}

func sanitizeTaskErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return llm.SanitizeProviderErrorMessage(err.Error())
}

func DrainEventsTo(w io.Writer, events <-chan agentruntime.RuntimeEvent) error {
	for event := range events {
		if event.Text != "" {
			if _, err := io.WriteString(w, event.Text); err != nil {
				return err
			}
		}
	}
	return nil
}
