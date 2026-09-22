// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package slashcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	TriggerRuntime   = "runtime"
	TriggerModel     = "model"
	TriggerNew       = "new"
	TriggerStop      = "stop"
	TriggerApprove   = "approve"
	TriggerDeny      = "deny"
	TriggerStatus    = "status"
	TriggerResume    = "resume"
	TriggerUsage     = "usage"
	TriggerApprovals = "approvals"
	TriggerTask      = "task"
	TriggerTasks     = "tasks"
	TriggerSessions  = "sessions"
	TriggerRemind    = "remind"
	TriggerFollowup  = "followup"

	DefaultTaskAgentID = "default"
)

type Config interface {
	EnableAgentRuntimeControlPlane() bool
}

type Store interface {
	CreateRuntimeSession(session *agentruntime.RuntimeSession) error
	GetRuntimeSession(id string) (*agentruntime.RuntimeSession, error)
	UpsertRuntimePolicy(policy *agentruntime.RuntimePolicy) error
	GetRuntimePolicy(scopeType agentruntime.PolicyScopeType, scopeID string) (*agentruntime.RuntimePolicy, error)
	ListRuntimeSessions(filter agentruntime.RuntimeSessionFilter) ([]agentruntime.RuntimeSession, error)
	UpdateRuntimeSessionStatus(id string, status agentruntime.RuntimeSessionStatus, lastError string) error
	CreateTask(task *agentruntime.Task) error
	GetTask(id string) (*agentruntime.Task, error)
	ListTasks(filter store.TaskFilter) ([]agentruntime.Task, error)
	UpdateTaskStatus(id string, status agentruntime.TaskStatus, lastRunAt, nextRunAt int64) error
	DeleteTask(id string) error
	ListRuntimeApprovals(filter store.RuntimeApprovalFilter) ([]agentruntime.RuntimeApproval, error)
	ListSupervisorRuns(filter store.SupervisorRunFilter) ([]agentruntime.SupervisorRun, error)
	ListSubagentRuns(supervisorRunID string) ([]agentruntime.SubagentRun, error)
}

type RuntimeControl interface {
	StopSession(ctx context.Context, sessionID string) error
	ResumeSession(ctx context.Context, sessionID string) (<-chan agentruntime.RuntimeEvent, error)
	GetStatus(ctx context.Context, sessionID string) (agentruntime.RuntimeStatus, error)
	SubmitApproval(ctx context.Context, decision agentruntime.RuntimeApprovalDecision) error
	ExpireApprovals(ctx context.Context) error
}

type PermissionChecker interface {
	CanManageRuntimePolicy(userID, channelID string) bool
}

type AuditLogger interface {
	RecordRuntimePolicyChange(event RuntimePolicyAuditEvent)
	RecordTaskChange(event TaskAuditEvent)
}

type RuntimePolicyAuditEvent struct {
	UserID        string
	ChannelID     string
	RootPostID    string
	ScopeType     agentruntime.PolicyScopeType
	ScopeID       string
	RuntimeType   agentruntime.RuntimeType
	ProviderID    string
	ChangedFields []string
}

type TaskAuditEvent struct {
	UserID        string
	ChannelID     string
	RootPostID    string
	TaskID        string
	TaskType      agentruntime.TaskType
	TaskStatus    agentruntime.TaskStatus
	AgentID       string
	Action        string
	NextRunAt     int64
	ChangedFields []string
}

type Handler struct {
	cfg            Config
	store          Store
	runtimeControl RuntimeControl
	permissions    PermissionChecker
	audit          AuditLogger
	now            func() time.Time
}

type Options struct {
	Config         Config
	Store          Store
	RuntimeControl RuntimeControl
	Permissions    PermissionChecker
	Audit          AuditLogger
	Now            func() time.Time
}

func New(opts Options) *Handler {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		cfg:            opts.Config,
		store:          opts.Store,
		runtimeControl: opts.RuntimeControl,
		permissions:    opts.Permissions,
		audit:          opts.Audit,
		now:            now,
	}
}

func (h *Handler) Execute(args *model.CommandArgs) (*model.CommandResponse, error) {
	if h == nil || h.cfg == nil || !h.cfg.EnableAgentRuntimeControlPlane() {
		return ephemeral("Agent runtime control plane is disabled."), nil
	}
	if h.store == nil {
		return nil, fmt.Errorf("slash command store is not configured")
	}
	if args == nil {
		return nil, fmt.Errorf("command args are required")
	}

	fields := strings.Fields(args.Command)
	if len(fields) == 0 {
		return ephemeral(h.help()), nil
	}

	trigger := strings.TrimPrefix(fields[0], "/")
	rest := fields[1:]

	switch trigger {
	case TriggerRuntime:
		return h.handleRuntime(args, rest)
	case TriggerModel:
		return h.handleModel(args, rest)
	case TriggerNew:
		return h.handleNew(args, rest)
	case TriggerStop:
		return h.handleStop(args, rest)
	case TriggerApprove:
		return h.handleApprovalDecision(args, rest, agentruntime.ApprovalDecisionAccept)
	case TriggerDeny:
		return h.handleApprovalDecision(args, rest, agentruntime.ApprovalDecisionDeny)
	case TriggerStatus:
		return h.handleStatus(args, rest)
	case TriggerResume:
		return h.handleResume(args, rest)
	case TriggerUsage:
		return h.handleUsage(args, rest)
	case TriggerApprovals:
		return h.handleApprovals(args, rest)
	case TriggerTask:
		return h.handleTask(args, rest)
	case TriggerTasks:
		return h.handleTasks(args, rest)
	case TriggerSessions:
		return h.handleSessions(args, rest)
	case TriggerRemind:
		return h.handleRemind(args, rest)
	case TriggerFollowup:
		return h.handleFollowup(args, rest)
	default:
		return ephemeral(h.help()), nil
	}
}

func (h *Handler) handleApprovalDecision(args *model.CommandArgs, fields []string, decision string) (*model.CommandResponse, error) {
	if h.runtimeControl == nil {
		return ephemeral("Runtime approval control is not initialized."), nil
	}
	if len(fields) == 0 {
		return ephemeral("Usage: `/approve <approval_id> [reason]` or `/deny <approval_id> [reason]`"), nil
	}
	approvalID := fields[0]
	reason := strings.TrimSpace(strings.Join(fields[1:], " "))
	if err := h.runtimeControl.ExpireApprovals(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to expire pending approvals: %w", err)
	}
	if !h.userCanDecideRuntimeApproval(args.UserId, approvalID) {
		return ephemeral(fmt.Sprintf("Runtime approval `%s` was not found.", approvalID)), nil
	}
	if err := h.runtimeControl.SubmitApproval(context.Background(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: approvalID,
		UserID:     args.UserId,
		Decision:   decision,
		Reason:     reason,
	}); err != nil {
		return nil, fmt.Errorf("failed to submit approval decision: %w", err)
	}
	return ephemeral(fmt.Sprintf("Runtime approval `%s` %s.", approvalID, approvalDecisionVerb(decision))), nil
}

func (h *Handler) userCanDecideRuntimeApproval(userID string, approvalID string) bool {
	if userID == "" || approvalID == "" {
		return false
	}
	approvals, err := h.store.ListRuntimeApprovals(store.RuntimeApprovalFilter{
		RequestedBy: userID,
		Status:      agentruntime.ApprovalStatusPending,
		Limit:       100,
	})
	if err != nil {
		return false
	}
	for _, approval := range approvals {
		if approval.ID == approvalID {
			return true
		}
	}
	return false
}

func (h *Handler) handleRuntime(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) == 0 || fields[0] == "status" {
		scopeType, scopeID := commandScope(args)
		policy, err := h.store.GetRuntimePolicy(scopeType, scopeID)
		if err != nil {
			return ephemeral(fmt.Sprintf("No runtime policy set for this %s; inherited default applies.", scopeType)), nil
		}
		return ephemeral(fmt.Sprintf("Runtime for this %s is `%s` using provider `%s` model `%s`.", scopeType, policy.RuntimeType, policy.ProviderID, policy.Model)), nil
	}

	scopeType, scopeID := commandScope(args)
	if scopeID == "" {
		return ephemeral("Cannot set runtime without a channel or thread scope."), nil
	}
	if !h.canManageRuntimePolicy(args) {
		return ephemeral("You do not have permission to manage the runtime policy for this channel."), nil
	}

	policy, err := runtimePolicyForCommand(fields[0])
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	policy.ScopeType = scopeType
	policy.ScopeID = scopeID
	policy.CreatedBy = args.UserId
	policy.UpdatedBy = args.UserId

	if existing, err := h.store.GetRuntimePolicy(scopeType, scopeID); err == nil {
		policy.ID = existing.ID
		policy.CreatedAt = existing.CreatedAt
		policy.CreatedBy = existing.CreatedBy
	}
	if err := h.store.UpsertRuntimePolicy(&policy); err != nil {
		return nil, fmt.Errorf("failed to save runtime policy: %w", err)
	}
	h.recordRuntimePolicyChange(args, policy, []string{"runtimeType", "providerID", "allowCloud", "allowLocal"})

	return ephemeral(fmt.Sprintf("Runtime for this %s set to `%s`.", scopeType, policy.RuntimeType)), nil
}

func (h *Handler) handleModel(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) == 0 {
		return ephemeral("Usage: `/model <model>`"), nil
	}
	scopeType, scopeID := commandScope(args)
	if scopeID == "" {
		return ephemeral("Cannot set model without a channel or thread scope."), nil
	}
	if !h.canManageRuntimePolicy(args) {
		return ephemeral("You do not have permission to manage the runtime policy for this channel."), nil
	}

	modelName := strings.Join(fields, " ")
	policy, err := h.store.GetRuntimePolicy(scopeType, scopeID)
	if err != nil {
		defaultPolicy := runtimePolicyDefault()
		policy = &defaultPolicy
		policy.ScopeType = scopeType
		policy.ScopeID = scopeID
		policy.CreatedBy = args.UserId
	}
	policy.Model = modelName
	policy.UpdatedBy = args.UserId
	if err := h.store.UpsertRuntimePolicy(policy); err != nil {
		return nil, fmt.Errorf("failed to save runtime model policy: %w", err)
	}
	h.recordRuntimePolicyChange(args, *policy, []string{"model"})
	return ephemeral(fmt.Sprintf("Model for this %s set to `%s`.", scopeType, modelName)), nil
}

func (h *Handler) handleNew(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	agentID := DefaultTaskAgentID
	if len(fields) > 0 {
		agentID = fields[0]
	}
	policy := h.policyForCurrentScope(args)
	session := &agentruntime.RuntimeSession{
		MattermostConversationID: commandConversationID(args),
		ChannelID:                args.ChannelId,
		RootPostID:               args.RootId,
		UserID:                   args.UserId,
		AgentID:                  agentID,
		RuntimeType:              policy.RuntimeType,
		ProviderID:               policy.ProviderID,
		Model:                    policy.Model,
		Status:                   agentruntime.SessionStatusIdle,
	}
	if err := h.store.CreateRuntimeSession(session); err != nil {
		return nil, fmt.Errorf("failed to create runtime session: %w", err)
	}
	return ephemeral(fmt.Sprintf("Created new `%s` runtime session `%s`.", session.RuntimeType, session.ID)), nil
}

func (h *Handler) handleStop(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	session, err := h.sessionForCommand(args, fields)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	if h.runtimeControl != nil {
		if err := h.runtimeControl.StopSession(context.Background(), session.ID); err != nil {
			return nil, fmt.Errorf("failed to stop runtime session: %w", err)
		}
		return ephemeral(fmt.Sprintf("Stopped runtime session `%s`.", session.ID)), nil
	}
	if err := h.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusCancelled, ""); err != nil {
		return nil, fmt.Errorf("failed to mark runtime session cancelled: %w", err)
	}
	return ephemeral(fmt.Sprintf("Marked runtime session `%s` as cancelled.", session.ID)), nil
}

func (h *Handler) handleStatus(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	session, err := h.sessionForCommand(args, fields)
	if err != nil {
		return h.scopedStatus(args)
	}
	if h.runtimeControl != nil {
		status, err := h.runtimeControl.GetStatus(context.Background(), session.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get runtime status: %w", err)
		}
		lines := []string{formatRuntimeStatus(status)}
		lines = append(lines, h.runtimeContextLines(args, *session)...)
		return ephemeral(strings.Join(lines, "\n")), nil
	}
	lines := []string{formatSessionStatus(*session)}
	lines = append(lines, h.runtimeContextLines(args, *session)...)
	return ephemeral(strings.Join(lines, "\n")), nil
}

func (h *Handler) handleResume(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	session, err := h.sessionForCommand(args, fields)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	if h.runtimeControl != nil {
		events, err := h.runtimeControl.ResumeSession(context.Background(), session.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to resume runtime session: %w", err)
		}
		go func() {
			for range events {
			}
		}()
		return ephemeral(fmt.Sprintf("Resumed runtime session `%s`.", session.ID)), nil
	}
	if err := h.store.UpdateRuntimeSessionStatus(session.ID, agentruntime.SessionStatusIdle, ""); err != nil {
		return nil, fmt.Errorf("failed to mark runtime session resumable: %w", err)
	}
	return ephemeral(fmt.Sprintf("Runtime session `%s` is ready to resume on the next agent turn.", session.ID)), nil
}

func (h *Handler) handleUsage(args *model.CommandArgs, _ []string) (*model.CommandResponse, error) {
	sessions, err := h.store.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		UserID: args.UserId,
		Limit:  100,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime sessions for usage: %w", err)
	}
	byRuntime := map[agentruntime.RuntimeType]int{}
	usageByRuntime := map[agentruntime.RuntimeType]agentruntime.RuntimeUsage{}
	var totalUsage agentruntime.RuntimeUsage
	for _, session := range sessions {
		byRuntime[session.RuntimeType]++
		usage := usageFromSessionMetadata(session.Metadata)
		usageByRuntime[session.RuntimeType] = addUsage(usageByRuntime[session.RuntimeType], usage)
		totalUsage = addUsage(totalUsage, usage)
	}
	lines := []string{fmt.Sprintf("Runtime usage summary: `%d` sessions, `%d` input tokens, `%d` output tokens, `%s` duration, `$%.4f` estimated cost", len(sessions), totalUsage.InputTokens, totalUsage.OutputTokens, formatDurationMS(totalUsage.DurationMS), totalUsage.Cost)}
	for _, runtimeType := range []agentruntime.RuntimeType{agentruntime.RuntimeTypeCodex, agentruntime.RuntimeTypeOpenAI, agentruntime.RuntimeTypeLocal} {
		if count := byRuntime[runtimeType]; count > 0 {
			usage := usageByRuntime[runtimeType]
			lines = append(lines, fmt.Sprintf("- `%s`: %d sessions, %d in, %d out, %s, $%.4f", runtimeType, count, usage.InputTokens, usage.OutputTokens, formatDurationMS(usage.DurationMS), usage.Cost))
		}
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

func usageFromSessionMetadata(raw json.RawMessage) agentruntime.RuntimeUsage {
	if len(raw) == 0 {
		return agentruntime.RuntimeUsage{}
	}
	var metadata struct {
		Usage agentruntime.RuntimeUsage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &metadata)
	return metadata.Usage
}

func addUsage(a, b agentruntime.RuntimeUsage) agentruntime.RuntimeUsage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CachedReadTokens += b.CachedReadTokens
	a.CachedWriteTokens += b.CachedWriteTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.DurationMS += b.DurationMS
	a.Cost += b.Cost
	return a
}

func formatDurationMS(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Second).String()
}

func (h *Handler) handleApprovals(args *model.CommandArgs, _ []string) (*model.CommandResponse, error) {
	if h.runtimeControl != nil {
		if err := h.runtimeControl.ExpireApprovals(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to expire pending approvals: %w", err)
		}
	}
	approvals, err := h.store.ListRuntimeApprovals(store.RuntimeApprovalFilter{
		RequestedBy: args.UserId,
		Status:      agentruntime.ApprovalStatusPending,
		Limit:       20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pending approvals: %w", err)
	}
	if len(approvals) == 0 {
		return ephemeral("No pending runtime approvals found."), nil
	}
	lines := []string{"Pending runtime approvals:"}
	for _, approval := range approvals {
		lines = append(lines, fmt.Sprintf("- `%s` session `%s` external `%s`", approval.ID, approval.RuntimeSessionID, approval.ExternalApprovalID))
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

func (h *Handler) handleTask(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) == 0 {
		return ephemeral(taskHelp()), nil
	}

	switch fields[0] {
	case "create":
		agentID, promptFields := extractAgentID(fields[1:])
		prompt := strings.TrimSpace(strings.Join(promptFields, " "))
		if prompt == "" {
			return ephemeral("Usage: `/task create [--agent <agent_id>] <prompt>`"), nil
		}
		return h.createTask(args, agentID, agentruntime.TaskTypeOneShot, 0, "", prompt)
	case "remind", "reminder":
		if len(fields) < 3 {
			return ephemeral("Usage: `/task reminder <unix_ms> [--agent <agent_id>] <prompt>`"), nil
		}
		nextRunAt, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || nextRunAt <= h.now().UnixMilli() {
			return ephemeral("Reminder time must be a future Unix millisecond timestamp."), nil
		}
		agentID, promptFields := extractAgentID(fields[2:])
		prompt := strings.TrimSpace(strings.Join(promptFields, " "))
		if prompt == "" {
			return ephemeral("Usage: `/task reminder <unix_ms> [--agent <agent_id>] <prompt>`"), nil
		}
		return h.createTask(args, agentID, agentruntime.TaskTypeReminder, nextRunAt, "", prompt)
	case "every":
		if len(fields) < 3 {
			return ephemeral("Usage: `/task every <duration> [--agent <agent_id>] <prompt>`"), nil
		}
		scheduleSpec := fields[1]
		agentID, promptFields := extractAgentID(fields[2:])
		prompt := strings.TrimSpace(strings.Join(promptFields, " "))
		if prompt == "" {
			return ephemeral("Usage: `/task every <duration> [--agent <agent_id>] <prompt>`"), nil
		}
		return h.createTask(args, agentID, agentruntime.TaskTypeRecurring, h.now().UnixMilli(), scheduleSpec, prompt)
	case "pause":
		return h.updateTaskCommand(args, fields[1:], agentruntime.TaskStatusPaused, -1)
	case "resume":
		return h.updateTaskCommand(args, fields[1:], agentruntime.TaskStatusQueued, h.now().UnixMilli())
	case "run":
		return h.runTaskCommand(args, fields[1:])
	case "cancel":
		return h.updateTaskCommand(args, fields[1:], agentruntime.TaskStatusCancelled, 0)
	case "stop":
		return h.updateTaskCommand(args, fields[1:], agentruntime.TaskStatusCancelled, 0)
	case "delete":
		return h.deleteTaskCommand(args, fields[1:])
	case "snooze":
		return h.snoozeTaskCommand(args, fields[1:])
	default:
		return ephemeral(taskHelp()), nil
	}
}

func (h *Handler) handleTasks(args *model.CommandArgs, _ []string) (*model.CommandResponse, error) {
	tasks, err := h.store.ListTasks(store.TaskFilter{
		UserID: args.UserId,
		Limit:  20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	if len(tasks) == 0 {
		return ephemeral("No tasks found."), nil
	}

	lines := make([]string, 0, len(tasks)+1)
	lines = append(lines, "Tasks:")
	for _, task := range tasks {
		lines = append(lines, fmt.Sprintf("- `%s` `%s` %s", task.ID, task.Status, task.Title))
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

func (h *Handler) handleRemind(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	scope, fields := extractReminderScope(fields)
	if len(fields) == 0 {
		return ephemeral(remindHelp()), nil
	}

	agentID, fields := extractAgentID(fields)
	var taskType agentruntime.TaskType
	var nextRunAt int64
	var scheduleSpec string
	var promptFields []string
	now := h.now()

	switch fields[0] {
	case "in":
		if len(fields) < 3 {
			return ephemeral(remindHelp()), nil
		}
		duration, consumed, ok := parseReminderDuration(fields[1:])
		if !ok {
			return ephemeral("Reminder duration must be a positive value like `15m`, `2h`, `2 days`, or `1 week`."), nil
		}
		taskType = agentruntime.TaskTypeReminder
		nextRunAt = now.Add(duration).UnixMilli()
		promptFields = fields[1+consumed:]
	case "tomorrow":
		if len(fields) < 3 {
			return ephemeral(remindHelp()), nil
		}
		next, ok := parseTomorrowAt(now, fields[1])
		if !ok {
			return ephemeral("Tomorrow reminder time must use `HH:MM`, for example `/remind me tomorrow 09:00 backup pruefen`."), nil
		}
		taskType = agentruntime.TaskTypeReminder
		nextRunAt = next.UnixMilli()
		promptFields = fields[2:]
	case "every":
		if len(fields) < 3 {
			return ephemeral(remindHelp()), nil
		}
		taskType = agentruntime.TaskTypeRecurring
		if next, spec, consumed, ok := parseRecurringReminder(now, fields[1:]); ok {
			nextRunAt = next.UnixMilli()
			scheduleSpec = spec
			promptFields = fields[1+consumed:]
		} else {
			return ephemeral("Recurring reminders must use `every <duration>` or `every <weekday> HH:MM`."), nil
		}
	default:
		return ephemeral(remindHelp()), nil
	}

	prompt := strings.TrimSpace(strings.Join(promptFields, " "))
	if prompt == "" {
		return ephemeral(remindHelp()), nil
	}
	return h.createTaskWithOptions(args, createTaskOptions{
		AgentID:      agentID,
		TaskType:     taskType,
		NextRunAt:    nextRunAt,
		ScheduleSpec: scheduleSpec,
		Prompt:       prompt,
		RootPostID:   reminderRootPostID(args, scope),
		Metadata:     reminderMetadata(scope),
	})
}

func (h *Handler) handleFollowup(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	agentID, fields := extractAgentID(fields)
	if len(fields) < 5 || fields[0] != "in" {
		return ephemeral(followupHelp()), nil
	}
	duration, consumed, ok := parseReminderDuration(fields[1:])
	if !ok {
		return ephemeral("Follow-up duration must be a positive value like `15m`, `2h`, `2 days`, or `1 week`."), nil
	}
	conditionFields := fields[1+consumed:]
	if len(conditionFields) < 3 || conditionFields[0] != "if" || conditionFields[1] != "no" || conditionFields[2] != "reply" {
		return ephemeral(followupHelp()), nil
	}
	prompt := strings.TrimSpace(strings.Join(conditionFields[3:], " "))
	if prompt == "" {
		prompt = "Follow up in this thread because nobody replied."
	}
	metadata, err := json.Marshal(map[string]any{
		"kind":       "followup",
		"condition":  "no_reply",
		"rootPostID": args.RootId,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode follow-up metadata: %w", err)
	}
	return h.createTaskWithOptions(args, createTaskOptions{
		AgentID:    agentID,
		TaskType:   agentruntime.TaskTypeWatcher,
		NextRunAt:  h.now().Add(duration).UnixMilli(),
		Prompt:     prompt,
		RootPostID: args.RootId,
		Metadata:   metadata,
	})
}

func (h *Handler) handleSessions(args *model.CommandArgs, _ []string) (*model.CommandResponse, error) {
	sessions, err := h.store.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		UserID: args.UserId,
		Limit:  20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime sessions: %w", err)
	}
	if len(sessions) == 0 {
		return ephemeral("No runtime sessions found."), nil
	}

	lines := make([]string, 0, len(sessions)+1)
	lines = append(lines, "Runtime sessions:")
	for _, session := range sessions {
		lines = append(lines, fmt.Sprintf("- `%s` `%s` `%s` %s", session.ID, session.Status, session.RuntimeType, session.Model))
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

func (h *Handler) scopedStatus(args *model.CommandArgs) (*model.CommandResponse, error) {
	policy := h.policyForCurrentScope(args)
	sessions, err := h.store.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		UserID:     args.UserId,
		ChannelID:  args.ChannelId,
		RootPostID: args.RootId,
		Limit:      5,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list scoped runtime sessions: %w", err)
	}

	lines := []string{fmt.Sprintf("Current runtime policy: `%s` provider `%s` model `%s`.", policy.RuntimeType, policy.ProviderID, policy.Model)}
	if len(sessions) == 0 {
		lines = append(lines, "No runtime session found for this scope.")
		return ephemeral(strings.Join(lines, "\n")), nil
	}
	lines = append(lines, "Recent sessions:")
	for _, session := range sessions {
		lines = append(lines, fmt.Sprintf("- %s", formatSessionStatus(session)))
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

func (h *Handler) runtimeContextLines(args *model.CommandArgs, session agentruntime.RuntimeSession) []string {
	var lines []string

	approvals, err := h.store.ListRuntimeApprovals(store.RuntimeApprovalFilter{
		RuntimeSessionID: session.ID,
		RequestedBy:      args.UserId,
		Status:           agentruntime.ApprovalStatusPending,
		Limit:            5,
	})
	if err == nil && len(approvals) > 0 {
		lines = append(lines, fmt.Sprintf("Pending approvals: %d", len(approvals)))
	}

	tasks, err := h.store.ListTasks(store.TaskFilter{
		UserID:    args.UserId,
		ChannelID: session.ChannelID,
		Limit:     5,
	})
	if err == nil {
		taskLines := []string{}
		for _, task := range tasks {
			if session.RootPostID != "" && task.RootPostID != "" && task.RootPostID != session.RootPostID {
				continue
			}
			taskLines = append(taskLines, fmt.Sprintf("%s:%s", task.ID, task.Status))
			if len(taskLines) >= 3 {
				break
			}
		}
		if len(taskLines) > 0 {
			lines = append(lines, "Tasks: "+strings.Join(taskLines, ", "))
		}
	}

	supervisors, err := h.store.ListSupervisorRuns(store.SupervisorRunFilter{
		RuntimeSessionID: session.ID,
		CreatedBy:        args.UserId,
		Limit:            3,
	})
	if err == nil && len(supervisors) > 0 {
		for _, supervisor := range supervisors {
			subagents, subErr := h.store.ListSubagentRuns(supervisor.ID)
			if subErr != nil {
				lines = append(lines, fmt.Sprintf("Supervisor `%s`: %s", supervisor.ID, supervisor.Status))
				continue
			}
			lines = append(lines, fmt.Sprintf("Supervisor `%s`: %s, %d subagents%s", supervisor.ID, supervisor.Status, len(subagents), formatSubagentStatuses(subagents)))
		}
	}

	return lines
}

func (h *Handler) sessionForCommand(args *model.CommandArgs, fields []string) (*agentruntime.RuntimeSession, error) {
	if len(fields) > 0 {
		session, err := h.store.GetRuntimeSession(fields[0])
		if err != nil {
			return nil, fmt.Errorf("runtime session `%s` was not found", fields[0])
		}
		if session.UserID != args.UserId {
			return nil, fmt.Errorf("runtime session `%s` was not found", fields[0])
		}
		return session, nil
	}

	sessions, err := h.store.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{
		UserID:     args.UserId,
		ChannelID:  args.ChannelId,
		RootPostID: args.RootId,
		Limit:      1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find runtime session: %w", err)
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no runtime session found in this scope")
	}
	return &sessions[0], nil
}

func (h *Handler) policyForCurrentScope(args *model.CommandArgs) agentruntime.RuntimePolicy {
	scopeType, scopeID := commandScope(args)
	if policy, err := h.store.GetRuntimePolicy(scopeType, scopeID); err == nil && policy.RuntimeType != agentruntime.RuntimeTypeInherit {
		return *policy
	}
	return runtimePolicyDefault()
}

func (h *Handler) createTask(args *model.CommandArgs, agentID string, taskType agentruntime.TaskType, nextRunAt int64, scheduleSpec, prompt string) (*model.CommandResponse, error) {
	return h.createTaskWithOptions(args, createTaskOptions{
		AgentID:      agentID,
		TaskType:     taskType,
		NextRunAt:    nextRunAt,
		ScheduleSpec: scheduleSpec,
		Prompt:       prompt,
		RootPostID:   args.RootId,
	})
}

type createTaskOptions struct {
	AgentID      string
	TaskType     agentruntime.TaskType
	NextRunAt    int64
	ScheduleSpec string
	Prompt       string
	RootPostID   string
	Metadata     json.RawMessage
}

func (h *Handler) createTaskWithOptions(args *model.CommandArgs, opts createTaskOptions) (*model.CommandResponse, error) {
	agentID := opts.AgentID
	if agentID == "" {
		agentID = DefaultTaskAgentID
	}
	policy := h.policyForCurrentScope(args)
	policySnapshot, err := json.Marshal(map[string]any{
		"runtimeType":       policy.RuntimeType,
		"providerID":        policy.ProviderID,
		"model":             policy.Model,
		"workspacePolicyID": policy.WorkspacePolicyID,
		"approvalPolicyID":  policy.ApprovalPolicyID,
		"allowCloud":        policy.AllowCloud,
		"allowLocal":        policy.AllowLocal,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode runtime policy snapshot: %w", err)
	}
	title := opts.Prompt
	if len(title) > 80 {
		title = title[:80]
	}
	metadata := opts.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	task := &agentruntime.Task{
		Title:                 title,
		Prompt:                opts.Prompt,
		TaskType:              opts.TaskType,
		Status:                agentruntime.TaskStatusQueued,
		ChannelID:             args.ChannelId,
		RootPostID:            opts.RootPostID,
		UserID:                args.UserId,
		AgentID:               agentID,
		RuntimePolicySnapshot: policySnapshot,
		ScheduleSpec:          opts.ScheduleSpec,
		NextRunAt:             opts.NextRunAt,
		CreatedBy:             args.UserId,
		Metadata:              metadata,
	}
	if err := h.store.CreateTask(task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}
	h.recordTaskChange(args, *task, "create", []string{"taskType", "status", "agentID", "nextRunAt", "scheduleSpec", "runtimePolicySnapshot"})
	return ephemeral(fmt.Sprintf("Created `%s` task `%s`.", task.TaskType, task.ID)), nil
}

func (h *Handler) updateTaskCommand(args *model.CommandArgs, fields []string, status agentruntime.TaskStatus, nextRunAt int64) (*model.CommandResponse, error) {
	if len(fields) == 0 {
		return ephemeral("Usage: `/task run|pause|resume|stop|cancel <task_id>`"), nil
	}
	task, err := h.getTaskForUser(fields[0], args.UserId)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	if status == agentruntime.TaskStatusQueued && nextRunAt <= h.now().UnixMilli() {
		nextRunAt = h.now().Add(time.Minute).UnixMilli()
	}
	if err := h.store.UpdateTaskStatus(task.ID, status, -1, nextRunAt); err != nil {
		return nil, fmt.Errorf("failed to update task status: %w", err)
	}
	task.Status = status
	if nextRunAt >= 0 {
		task.NextRunAt = nextRunAt
	}
	h.recordTaskChange(args, *task, string(status), []string{"status", "nextRunAt"})
	return ephemeral(fmt.Sprintf("Task `%s` set to `%s`.", task.ID, status)), nil
}

func (h *Handler) runTaskCommand(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) == 0 {
		return ephemeral("Usage: `/task run <task_id>`"), nil
	}
	task, err := h.getTaskForUser(fields[0], args.UserId)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	if err := h.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusQueued, -1, h.now().UnixMilli()); err != nil {
		return nil, fmt.Errorf("failed to queue task: %w", err)
	}
	task.Status = agentruntime.TaskStatusQueued
	task.NextRunAt = h.now().UnixMilli()
	h.recordTaskChange(args, *task, "run", []string{"status", "nextRunAt"})
	return ephemeral(fmt.Sprintf("Task `%s` queued to run now.", task.ID)), nil
}

func (h *Handler) snoozeTaskCommand(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) < 2 {
		return ephemeral("Usage: `/task snooze <task_id> <duration>`"), nil
	}
	task, err := h.getTaskForUser(fields[0], args.UserId)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	duration, err := time.ParseDuration(fields[1])
	if err != nil || duration <= 0 {
		return ephemeral("Snooze duration must be a positive value like `15m`, `1h`, or `24h`."), nil
	}
	nextRunAt := h.now().Add(duration).UnixMilli()
	if err := h.store.UpdateTaskStatus(task.ID, agentruntime.TaskStatusQueued, -1, nextRunAt); err != nil {
		return nil, fmt.Errorf("failed to snooze task: %w", err)
	}
	task.Status = agentruntime.TaskStatusQueued
	task.NextRunAt = nextRunAt
	h.recordTaskChange(args, *task, "snooze", []string{"status", "nextRunAt"})
	return ephemeral(fmt.Sprintf("Task `%s` snoozed until `%s`.", task.ID, time.UnixMilli(nextRunAt).Format(time.RFC3339))), nil
}

func (h *Handler) deleteTaskCommand(args *model.CommandArgs, fields []string) (*model.CommandResponse, error) {
	if len(fields) == 0 {
		return ephemeral("Usage: `/task delete <task_id>`"), nil
	}
	task, err := h.getTaskForUser(fields[0], args.UserId)
	if err != nil {
		return ephemeral(err.Error()), nil
	}
	if err := h.store.DeleteTask(task.ID); err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}
	h.recordTaskChange(args, *task, "delete", []string{"deleted"})
	return ephemeral(fmt.Sprintf("Task `%s` deleted.", task.ID)), nil
}

func (h *Handler) getTaskForUser(taskID, userID string) (*agentruntime.Task, error) {
	task, err := h.store.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("task `%s` was not found", taskID)
	}
	if task.UserID != userID {
		return nil, fmt.Errorf("task `%s` was not found", taskID)
	}
	return task, nil
}

func (h *Handler) canManageRuntimePolicy(args *model.CommandArgs) bool {
	if h.permissions == nil || args == nil || args.ChannelId == "" {
		return false
	}
	return h.permissions.CanManageRuntimePolicy(args.UserId, args.ChannelId)
}

func (h *Handler) recordRuntimePolicyChange(args *model.CommandArgs, policy agentruntime.RuntimePolicy, changedFields []string) {
	if h.audit == nil || args == nil {
		return
	}
	h.audit.RecordRuntimePolicyChange(RuntimePolicyAuditEvent{
		UserID:        args.UserId,
		ChannelID:     args.ChannelId,
		RootPostID:    args.RootId,
		ScopeType:     policy.ScopeType,
		ScopeID:       policy.ScopeID,
		RuntimeType:   policy.RuntimeType,
		ProviderID:    policy.ProviderID,
		ChangedFields: changedFields,
	})
}

func (h *Handler) recordTaskChange(args *model.CommandArgs, task agentruntime.Task, action string, changedFields []string) {
	if h.audit == nil || args == nil {
		return
	}
	h.audit.RecordTaskChange(TaskAuditEvent{
		UserID:        args.UserId,
		ChannelID:     task.ChannelID,
		RootPostID:    task.RootPostID,
		TaskID:        task.ID,
		TaskType:      task.TaskType,
		TaskStatus:    task.Status,
		AgentID:       task.AgentID,
		Action:        action,
		NextRunAt:     task.NextRunAt,
		ChangedFields: changedFields,
	})
}

func runtimePolicyForCommand(value string) (agentruntime.RuntimePolicy, error) {
	switch value {
	case "local":
		return agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeLocal,
			ProviderID:  "local",
			AllowLocal:  true,
		}, nil
	case "cloud", "codex":
		return agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeCodex,
			ProviderID:  "codex",
			AllowCloud:  true,
			AllowLocal:  true,
		}, nil
	case "openai":
		return agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeOpenAI,
			ProviderID:  "openai",
			AllowCloud:  true,
			AllowLocal:  true,
		}, nil
	case "inherit":
		return agentruntime.RuntimePolicy{
			RuntimeType: agentruntime.RuntimeTypeInherit,
			AllowCloud:  true,
			AllowLocal:  true,
		}, nil
	default:
		return agentruntime.RuntimePolicy{}, fmt.Errorf("Usage: `/runtime local|cloud|openai|inherit|status`")
	}
}

func runtimePolicyDefault() agentruntime.RuntimePolicy {
	return agentruntime.RuntimePolicy{
		RuntimeType: agentruntime.RuntimeTypeCodex,
		ProviderID:  "codex",
		AllowCloud:  true,
		AllowLocal:  true,
	}
}

func commandScope(args *model.CommandArgs) (agentruntime.PolicyScopeType, string) {
	if args.RootId != "" {
		return agentruntime.PolicyScopeThread, args.RootId
	}
	return agentruntime.PolicyScopeChannel, args.ChannelId
}

func commandConversationID(args *model.CommandArgs) string {
	if args.RootId != "" {
		return args.RootId
	}
	return args.ChannelId
}

func formatSessionStatus(session agentruntime.RuntimeSession) string {
	parts := []string{fmt.Sprintf("`%s` `%s` `%s` provider `%s` model `%s`", session.ID, session.Status, session.RuntimeType, session.ProviderID, session.Model)}
	if session.ExternalSessionID != "" {
		parts = append(parts, fmt.Sprintf("external `%s`", session.ExternalSessionID))
	}
	if session.WorkspacePath != "" {
		parts = append(parts, fmt.Sprintf("workspace `%s`", session.WorkspacePath))
	}
	if session.LastError != "" {
		parts = append(parts, "last_error `"+session.LastError+"`")
	}
	return strings.Join(parts, " ")
}

func formatRuntimeStatus(status agentruntime.RuntimeStatus) string {
	text := fmt.Sprintf("Session `%s` is `%s` using `%s` provider `%s` model `%s`.", status.SessionID, status.Status, status.RuntimeType, status.ProviderID, status.Model)
	if status.WorkspacePath != "" {
		text += fmt.Sprintf(" Workspace: `%s`.", status.WorkspacePath)
	}
	if status.LastError != "" {
		text += fmt.Sprintf(" Last error: %s", status.LastError)
	}
	return text
}

func formatSubagentStatuses(subagents []agentruntime.SubagentRun) string {
	if len(subagents) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(subagents), 4))
	for _, subagent := range subagents {
		label := subagent.Role
		if label == "" {
			label = subagent.Title
		}
		if label == "" {
			label = subagent.ID
		}
		parts = append(parts, fmt.Sprintf(" %s:%s", label, subagent.Status))
		if len(parts) >= 4 {
			break
		}
	}
	return " (" + strings.TrimSpace(strings.Join(parts, ",")) + ")"
}

func approvalDecisionVerb(decision string) string {
	switch decision {
	case agentruntime.ApprovalDecisionAccept:
		return "approved"
	case agentruntime.ApprovalDecisionDeny:
		return "denied"
	default:
		return "decided"
	}
}

func (h *Handler) help() string {
	return strings.Join([]string{
		"Available commands:",
		"- `/runtime local|cloud|openai|inherit|status`",
		"- `/model <model>`",
		"- `/new [agent_id]`",
		"- `/stop [session_id]`",
		"- `/approve <approval_id> [reason]`",
		"- `/deny <approval_id> [reason]`",
		"- `/status [session_id]`",
		"- `/resume [session_id]`",
		"- `/usage`",
		"- `/approvals`",
		"- `/task create [--agent <agent_id>] <prompt>`",
		"- `/task reminder <unix_ms> [--agent <agent_id>] <prompt>`",
		"- `/task every <duration> [--agent <agent_id>] <prompt>`",
		"- `/task run|pause|resume|stop|cancel|delete <task_id>`",
		"- `/task snooze <task_id> <duration>`",
		"- `/remind [me|channel|thread] in <duration> <prompt>`",
		"- `/remind [me|channel|thread] tomorrow HH:MM <prompt>`",
		"- `/remind channel every friday 15:00 <prompt>`",
		"- `/followup in <duration> if no reply [prompt]`",
		"",
		"Durations use values like `15m`, `1h`, or `24h`.",
		"- `/tasks`",
		"- `/sessions`",
	}, "\n")
}

func taskHelp() string {
	return "Usage: `/task create [--agent <agent_id>] <prompt>`, `/task reminder <unix_ms> [--agent <agent_id>] <prompt>`, `/task every <duration> [--agent <agent_id>] <prompt>`, `/task run|pause|resume|stop|cancel|delete <task_id>`, or `/task snooze <task_id> <duration>`"
}

func remindHelp() string {
	return "Usage: `/remind [me|channel|thread] in <duration> <prompt>`, `/remind [me|channel|thread] tomorrow HH:MM <prompt>`, or `/remind channel every friday 15:00 <prompt>`"
}

func followupHelp() string {
	return "Usage: `/followup in <duration> if no reply [prompt]`"
}

func extractReminderScope(fields []string) (string, []string) {
	if len(fields) == 0 {
		return "me", fields
	}
	switch fields[0] {
	case "me", "channel", "thread":
		return fields[0], fields[1:]
	default:
		return "me", fields
	}
}

func reminderRootPostID(args *model.CommandArgs, scope string) string {
	switch scope {
	case "channel":
		return ""
	case "thread":
		return args.RootId
	default:
		return args.RootId
	}
}

func reminderMetadata(scope string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{
		"kind":  "reminder",
		"scope": scope,
	})
	if err != nil {
		return json.RawMessage(`{"kind":"reminder"}`)
	}
	return raw
}

func parseReminderDuration(fields []string) (time.Duration, int, bool) {
	if len(fields) == 0 {
		return 0, 0, false
	}
	if duration, err := time.ParseDuration(fields[0]); err == nil && duration > 0 {
		return duration, 1, true
	}
	if len(fields) < 2 {
		return 0, 0, false
	}
	amount, err := strconv.Atoi(fields[0])
	if err != nil || amount <= 0 {
		return 0, 0, false
	}
	switch strings.ToLower(fields[1]) {
	case "day", "days", "tag", "tage", "tagen":
		return time.Duration(amount) * 24 * time.Hour, 2, true
	case "week", "weeks", "woche", "wochen":
		return time.Duration(amount) * 7 * 24 * time.Hour, 2, true
	default:
		return 0, 0, false
	}
}

func parseTomorrowAt(now time.Time, value string) (time.Time, bool) {
	hour, minute, ok := parseHourMinute(value)
	if !ok {
		return time.Time{}, false
	}
	next := time.Date(now.Year(), now.Month(), now.Day()+1, hour, minute, 0, 0, now.Location())
	return next, true
}

func parseRecurringReminder(now time.Time, fields []string) (time.Time, string, int, bool) {
	if len(fields) == 0 {
		return time.Time{}, "", 0, false
	}
	if duration, consumed, ok := parseReminderDuration(fields); ok {
		spec := "every " + strings.Join(fields[:consumed], " ")
		return now.Add(duration), spec, consumed, true
	}
	if len(fields) < 2 {
		return time.Time{}, "", 0, false
	}
	weekday, ok := parseWeekday(fields[0])
	if !ok {
		return time.Time{}, "", 0, false
	}
	hour, minute, ok := parseHourMinute(fields[1])
	if !ok {
		return time.Time{}, "", 0, false
	}
	next := nextWeekdayAt(now, weekday, hour, minute)
	return next, fmt.Sprintf("every %s %02d:%02d", strings.ToLower(fields[0]), hour, minute), 2, true
}

func parseHourMinute(value string) (int, int, bool) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, false
	}
	return parsed.Hour(), parsed.Minute(), true
}

func parseWeekday(value string) (time.Weekday, bool) {
	switch strings.ToLower(value) {
	case "monday", "mon", "montag", "mo":
		return time.Monday, true
	case "tuesday", "tue", "dienstag", "di":
		return time.Tuesday, true
	case "wednesday", "wed", "mittwoch", "mi":
		return time.Wednesday, true
	case "thursday", "thu", "donnerstag", "do":
		return time.Thursday, true
	case "friday", "fri", "freitag", "fr":
		return time.Friday, true
	case "saturday", "sat", "samstag", "sa":
		return time.Saturday, true
	case "sunday", "sun", "sonntag", "so":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func nextWeekdayAt(now time.Time, weekday time.Weekday, hour, minute int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	days := (int(weekday) - int(now.Weekday()) + 7) % 7
	if days == 0 && !next.After(now) {
		days = 7
	}
	return next.AddDate(0, 0, days)
}

func extractAgentID(fields []string) (string, []string) {
	out := make([]string, 0, len(fields))
	agentID := ""
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--agent" && i+1 < len(fields) {
			agentID = fields[i+1]
			i++
			continue
		}
		out = append(out, fields[i])
	}
	return agentID, out
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}
