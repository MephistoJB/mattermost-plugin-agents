// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattermost/mattermost-plugin-agents/v2/agentruntime"
	"github.com/mattermost/mattermost-plugin-agents/v2/audit"
	"github.com/mattermost/mattermost-plugin-agents/v2/store"
	"github.com/mattermost/mattermost/server/public/model"
)

const maxRuntimeSessionsPageSize = 100

type runtimePolicyRequest struct {
	RuntimeType              agentruntime.RuntimeType `json:"runtimeType" binding:"required"`
	ProviderID               string                   `json:"providerID"`
	Model                    string                   `json:"model"`
	WorkspacePolicyID        string                   `json:"workspacePolicyID"`
	ApprovalPolicyID         string                   `json:"approvalPolicyID"`
	AllowCloud               bool                     `json:"allowCloud"`
	AllowLocal               bool                     `json:"allowLocal"`
	CloudBudgetCents         int64                    `json:"cloudBudgetCents"`
	CloudBudgetWindow        string                   `json:"cloudBudgetWindow"`
	CloudEscalationConfirmed bool                     `json:"cloudEscalationConfirmed"`
}

type workspacePolicyRequest struct {
	Name                 string                            `json:"name" binding:"required"`
	AllowedRoots         []string                          `json:"allowedRoots"`
	DefaultWorkspacePath string                            `json:"defaultWorkspacePath"`
	Mode                 agentruntime.WorkspaceMode        `json:"mode"`
	NetworkMode          agentruntime.WorkspaceNetworkMode `json:"networkMode"`
	ShellMode            agentruntime.WorkspaceShellMode   `json:"shellMode"`
	Metadata             map[string]any                    `json:"metadata"`
}

type runtimeApprovalDecisionRequest struct {
	Decision string `json:"decision" binding:"required"`
	Scope    string `json:"scope"`
	Reason   string `json:"reason"`
}

type runtimeSessionActionRequest struct {
	Action string `json:"action" binding:"required"`
}

type runtimeTaskActionRequest struct {
	Action    string `json:"action" binding:"required"`
	NextRunAt int64  `json:"nextRunAt"`
	SnoozeMs  int64  `json:"snoozeMs"`
}

type hermesOffChecklistUpdateRequest struct {
	Status agentruntime.ChecklistStatus `json:"status" binding:"required"`
	Detail string                       `json:"detail"`
}

type supervisorRunResponse struct {
	agentruntime.SupervisorRun
	Subagents []agentruntime.SubagentRun `json:"subagents"`
}

type runtimeHealthResponse struct {
	Enabled               bool                             `json:"enabled"`
	Healthy               bool                             `json:"healthy"`
	HermesOffReady        bool                             `json:"hermesOffReady"`
	Readiness             []runtimeReadinessCheckResponse  `json:"readiness"`
	ActiveSessions        int                              `json:"activeSessions"`
	SessionsByStatus      map[string]int                   `json:"sessionsByStatus"`
	SessionsByRuntimeType map[agentruntime.RuntimeType]int `json:"sessionsByRuntimeType"`
	Usage                 agentruntime.RuntimeUsage        `json:"usage"`
	ActiveTasks           int                              `json:"activeTasks"`
	TasksByStatus         map[agentruntime.TaskStatus]int  `json:"tasksByStatus"`
	PendingApprovals      int                              `json:"pendingApprovals"`
	ActiveSupervisorRuns  int                              `json:"activeSupervisorRuns"`
	SupervisorsByStatus   map[string]int                   `json:"supervisorsByStatus"`
	Voice                 runtimeVoiceHealthResponse       `json:"voice"`
	Recovery              runtimeRecoveryHealthResponse    `json:"recovery"`
}

type hermesOffChecklistResponse struct {
	Ready       bool                      `json:"ready"`
	GeneratedAt int64                     `json:"generatedAt"`
	Groups      []hermesOffChecklistGroup `json:"groups"`
	Health      runtimeHealthResponse     `json:"health"`
}

type hermesOffChecklistGroup struct {
	Key   string                   `json:"key"`
	Label string                   `json:"label"`
	Items []hermesOffChecklistItem `json:"items"`
}

type hermesOffChecklistItem struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	Manual    bool   `json:"manual"`
	Detail    string `json:"detail"`
	Evidence  string `json:"evidence"`
	UpdatedBy string `json:"updatedBy"`
	UpdatedAt int64  `json:"updatedAt"`
}

type runtimeReadinessCheckResponse struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type runtimeVoiceHealthResponse struct {
	TranscriptionConfigured      bool `json:"transcriptionConfigured"`
	LocalTranscriptionConfigured bool `json:"localTranscriptionConfigured"`
	TextToSpeechConfigured       bool `json:"textToSpeechConfigured"`
	LocalTextToSpeechConfigured  bool `json:"localTextToSpeechConfigured"`
	LocalVoiceReady              bool `json:"localVoiceReady"`
}

type RuntimeRecoveryStatus struct {
	StartedAt            int64
	CompletedAt          int64
	RuntimeRecoveryError string
	TaskRecoveryError    string
}

type runtimeRecoveryHealthResponse struct {
	StartedAt            int64  `json:"startedAt"`
	CompletedAt          int64  `json:"completedAt"`
	RuntimeRecoveryError string `json:"runtimeRecoveryError"`
	TaskRecoveryError    string `json:"taskRecoveryError"`
	Ready                bool   `json:"ready"`
}

type runtimeTaskResponse struct {
	agentruntime.Task
	LastRun *agentruntime.TaskRun `json:"lastRun,omitempty"`
}

func (a *API) handleListRuntimeSessions(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	limit := parseRuntimeLimit(c.Query("limit"))
	filter := agentruntime.RuntimeSessionFilter{
		UserID: userID,
		Limit:  limit,
	}
	if agentID := c.Query("agent_id"); agentID != "" {
		filter.AgentID = agentID
	}
	if conversationID := c.Query("conversation_id"); conversationID != "" {
		filter.ConversationID = conversationID
	}
	if status := c.Query("status"); status != "" {
		filter.Status = agentruntime.RuntimeSessionStatus(status)
	}

	sessions, err := a.runtimeStore.ListRuntimeSessions(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime sessions: %w", err))
		return
	}
	c.JSON(http.StatusOK, sessions)
}

func (a *API) handleAdminListRuntimeSessions(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime sessions") {
		return
	}

	filter := agentruntime.RuntimeSessionFilter{Limit: parseRuntimeLimit(c.Query("limit"))}
	if userID := c.Query("user_id"); userID != "" {
		filter.UserID = userID
	}
	if agentID := c.Query("agent_id"); agentID != "" {
		filter.AgentID = agentID
	}
	if channelID := c.Query("channel_id"); channelID != "" {
		filter.ChannelID = channelID
	}
	if rootPostID := c.Query("root_post_id"); rootPostID != "" {
		filter.RootPostID = rootPostID
	}
	if conversationID := c.Query("conversation_id"); conversationID != "" {
		filter.ConversationID = conversationID
	}
	if status := c.Query("status"); status != "" {
		filter.Status = agentruntime.RuntimeSessionStatus(status)
	}

	sessions, err := a.runtimeStore.ListRuntimeSessions(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime sessions: %w", err))
		return
	}
	c.JSON(http.StatusOK, sessions)
}

func (a *API) handleListRuntimeApprovals(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	if err := a.expireRuntimeApprovals(c); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	approvals, err := a.runtimeStore.ListRuntimeApprovals(store.RuntimeApprovalFilter{
		RequestedBy: c.GetHeader("Mattermost-User-Id"),
		Status:      agentruntime.ApprovalStatusPending,
		Limit:       parseRuntimeLimit(c.Query("limit")),
	})
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime approvals: %w", err))
		return
	}
	c.JSON(http.StatusOK, approvals)
}

func (a *API) handleAdminListRuntimeApprovals(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime approvals") {
		return
	}
	if err := a.expireRuntimeApprovals(c); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	filter := store.RuntimeApprovalFilter{Limit: parseRuntimeLimit(c.Query("limit"))}
	if runtimeSessionID := c.Query("runtime_session_id"); runtimeSessionID != "" {
		filter.RuntimeSessionID = runtimeSessionID
	}
	if requestedBy := c.Query("requested_by"); requestedBy != "" {
		filter.RequestedBy = requestedBy
	}
	if status := c.Query("status"); status != "" {
		filter.Status = agentruntime.RuntimeApprovalStatus(status)
	}

	approvals, err := a.runtimeStore.ListRuntimeApprovals(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime approvals: %w", err))
		return
	}
	c.JSON(http.StatusOK, approvals)
}

func (a *API) handleListRuntimeTasks(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	filter := store.TaskFilter{
		UserID: c.GetHeader("Mattermost-User-Id"),
		Limit:  parseRuntimeLimit(c.Query("limit")),
	}
	if channelID := c.Query("channel_id"); channelID != "" {
		filter.ChannelID = channelID
	}
	if status := c.Query("status"); status != "" {
		filter.Status = agentruntime.TaskStatus(status)
	}

	tasks, err := a.runtimeStore.ListTasks(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime tasks: %w", err))
		return
	}
	if rootPostID := c.Query("root_post_id"); rootPostID != "" {
		tasks = filterTasksByRootPostID(tasks, rootPostID)
	}
	c.JSON(http.StatusOK, tasks)
}

func (a *API) handleAdminListRuntimeTasks(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime tasks") {
		return
	}

	filter := store.TaskFilter{
		Limit: parseRuntimeLimit(c.Query("limit")),
	}
	if channelID := c.Query("channel_id"); channelID != "" {
		filter.ChannelID = channelID
	}
	if status := c.Query("status"); status != "" {
		filter.Status = agentruntime.TaskStatus(status)
	}

	tasks, err := a.runtimeStore.ListTasks(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime tasks: %w", err))
		return
	}
	if rootPostID := c.Query("root_post_id"); rootPostID != "" {
		tasks = filterTasksByRootPostID(tasks, rootPostID)
	}
	responses, err := a.runtimeTaskResponses(tasks)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime task runs: %w", err))
		return
	}
	c.JSON(http.StatusOK, responses)
}

func (a *API) handleAdminListRuntimeTaskRuns(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime task runs") {
		return
	}

	taskID := c.Param("taskID")
	if taskID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("task id is required"))
		return
	}
	if _, err := a.runtimeStore.GetTask(taskID); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime task: %w", err))
		return
	}

	runs, err := a.runtimeStore.ListTaskRuns(taskID)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime task runs: %w", err))
		return
	}
	c.JSON(http.StatusOK, runs)
}

func (a *API) handleAdminRuntimeTaskAction(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime tasks") {
		return
	}

	taskID := c.Param("taskID")
	if taskID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("task id is required"))
		return
	}

	var req runtimeTaskActionRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}

	task, err := a.runtimeStore.GetTask(taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime task: %w", err))
		return
	}

	now := model.GetMillis()
	status, nextRunAt, deleteTask, err := runtimeTaskActionTarget(req, task, now)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	audit.AddParam(auditRec(c), "task_id", audit.TruncateID(task.ID))
	audit.AddParam(auditRec(c), audit.KeyAgentID, audit.TruncateID(task.AgentID))
	audit.AddParam(auditRec(c), audit.KeyChannelID, audit.TruncateID(task.ChannelID))
	if task.RootPostID != "" {
		audit.AddParam(auditRec(c), audit.KeyThreadRootPostID, audit.TruncateID(task.RootPostID))
	}
	audit.AddParam(auditRec(c), "task_action", req.Action)
	audit.AddParam(auditRec(c), "task_type", string(task.TaskType))

	if deleteTask {
		if err := a.runtimeStore.DeleteTask(task.ID); err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to delete runtime task: %w", err))
			return
		}
		c.Status(http.StatusNoContent)
		return
	}

	if err := a.runtimeStore.UpdateTaskStatus(task.ID, status, -1, nextRunAt); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to update runtime task: %w", err))
		return
	}
	task.Status = status
	if nextRunAt >= 0 {
		task.NextRunAt = nextRunAt
	}
	c.JSON(http.StatusOK, task)
}

func (a *API) handleListSupervisorRuns(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	filter := store.SupervisorRunFilter{
		CreatedBy: c.GetHeader("Mattermost-User-Id"),
		Limit:     parseRuntimeLimit(c.Query("limit")),
	}
	if conversationID := c.Query("conversation_id"); conversationID != "" {
		filter.MattermostConversationID = conversationID
	}
	if runtimeSessionID := c.Query("runtime_session_id"); runtimeSessionID != "" {
		filter.RuntimeSessionID = runtimeSessionID
	}
	if rootTaskID := c.Query("root_task_id"); rootTaskID != "" {
		filter.RootTaskID = rootTaskID
	}
	if status := c.Query("status"); status != "" {
		filter.Status = status
	}

	runs, err := a.runtimeStore.ListSupervisorRuns(filter)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list supervisor runs: %w", err))
		return
	}
	responses := make([]supervisorRunResponse, 0, len(runs))
	for _, run := range runs {
		subagents, err := a.runtimeStore.ListSubagentRuns(run.ID)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list subagent runs: %w", err))
			return
		}
		responses = append(responses, supervisorRunResponse{
			SupervisorRun: run,
			Subagents:     subagents,
		})
	}
	c.JSON(http.StatusOK, responses)
}

func (a *API) handleGetScopedRuntimePolicy(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	scopeType := agentruntime.PolicyScopeType(c.Param("scopeType"))
	scopeID := c.Param("scopeID")
	if !validRuntimePolicyScope(scopeType) || !isUserScopedRuntimePolicyScope(scopeType) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid scoped runtime policy scope %q", scopeType))
		return
	}
	if err := a.requireRuntimePolicyScopePermission(c, scopeType, scopeID, false); err != nil {
		c.AbortWithError(http.StatusForbidden, err)
		return
	}

	policy, err := a.runtimeStore.GetRuntimePolicy(scopeType, scopeID)
	if err != nil {
		if errors.Is(err, store.ErrRuntimePolicyNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime policy: %w", err))
		return
	}
	c.JSON(http.StatusOK, policy)
}

func (a *API) handleUpsertScopedRuntimePolicy(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	scopeType := agentruntime.PolicyScopeType(c.Param("scopeType"))
	scopeID := c.Param("scopeID")
	if !validRuntimePolicyScope(scopeType) || !isUserScopedRuntimePolicyScope(scopeType) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid scoped runtime policy scope %q", scopeType))
		return
	}
	if err := a.requireRuntimePolicyScopePermission(c, scopeType, scopeID, true); err != nil {
		c.AbortWithError(http.StatusForbidden, err)
		return
	}

	var req runtimePolicyRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if !validRuntimeType(req.RuntimeType) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid runtime type %q", req.RuntimeType))
		return
	}
	if err := validateCloudEscalationConfirmation(req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	policy := runtimePolicyFromRequest(req, scopeType, scopeID, userID)
	if existing, err := a.runtimeStore.GetRuntimePolicy(scopeType, scopeID); err == nil {
		policy.ID = existing.ID
		policy.CreatedAt = existing.CreatedAt
		policy.CreatedBy = existing.CreatedBy
	} else if !errors.Is(err, store.ErrRuntimePolicyNotFound) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime policy: %w", err))
		return
	}

	audit.AddParam(auditRec(c), "scope_type", string(scopeType))
	audit.AddParam(auditRec(c), "scope_id", scopeID)
	audit.AddParam(auditRec(c), "runtime_type", string(req.RuntimeType))
	audit.AddParam(auditRec(c), "cloud_escalation_confirmed", strconv.FormatBool(req.CloudEscalationConfirmed))
	if err := a.runtimeStore.UpsertRuntimePolicy(policy); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to save runtime policy: %w", err))
		return
	}
	c.JSON(http.StatusOK, policy)
}

func (a *API) handleSubmitRuntimeApproval(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	if a.runtimeApprovalControl == nil {
		c.AbortWithError(http.StatusInternalServerError, errors.New("runtime approval control is not configured"))
		return
	}

	approvalID := c.Param("approvalID")
	if approvalID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime approval id is required"))
		return
	}

	var req runtimeApprovalDecisionRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if !validRuntimeApprovalDecision(req.Decision) {
		c.AbortWithError(http.StatusBadRequest, errors.New("approval decision must be accept or deny"))
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	if err := a.expireRuntimeApprovals(c); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	approval, err := a.runtimeStore.GetRuntimeApproval(approvalID)
	if err != nil {
		if errors.Is(err, store.ErrRuntimeApprovalNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime approval: %w", err))
		return
	}
	if userID == "" || approval.RequestedBy != userID || approval.Status != agentruntime.ApprovalStatusPending {
		c.AbortWithError(http.StatusForbidden, errors.New("user cannot decide this runtime approval"))
		return
	}
	audit.AddParam(auditRec(c), "runtime_approval_id", approvalID)
	audit.AddParam(auditRec(c), "runtime_approval_decision", req.Decision)
	if err := a.runtimeApprovalControl.SubmitApproval(c.Request.Context(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: approvalID,
		UserID:     userID,
		Decision:   req.Decision,
		Scope:      req.Scope,
		Reason:     req.Reason,
	}); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to submit runtime approval: %w", err))
		return
	}
	a.postRuntimeApprovalDecisionNotice(approval, req.Decision, userID)
	c.Status(http.StatusNoContent)
}

func (a *API) handleAdminSubmitRuntimeApproval(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime approvals") {
		return
	}
	if a.runtimeApprovalControl == nil {
		c.AbortWithError(http.StatusInternalServerError, errors.New("runtime approval control is not configured"))
		return
	}

	approvalID := c.Param("approvalID")
	if approvalID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime approval id is required"))
		return
	}

	var req runtimeApprovalDecisionRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if !validRuntimeApprovalDecision(req.Decision) {
		c.AbortWithError(http.StatusBadRequest, errors.New("approval decision must be accept or deny"))
		return
	}
	if err := a.expireRuntimeApprovals(c); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	approval, err := a.runtimeStore.GetRuntimeApproval(approvalID)
	if err != nil {
		if errors.Is(err, store.ErrRuntimeApprovalNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime approval: %w", err))
		return
	}

	audit.AddParam(auditRec(c), "runtime_approval_id", approvalID)
	audit.AddParam(auditRec(c), "runtime_approval_decision", req.Decision)
	audit.AddParam(auditRec(c), "runtime_approval_admin_decision", "true")
	if err := a.runtimeApprovalControl.SubmitApproval(c.Request.Context(), agentruntime.RuntimeApprovalDecision{
		ApprovalID: approvalID,
		UserID:     userID,
		Decision:   req.Decision,
		Scope:      req.Scope,
		Reason:     req.Reason,
	}); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to submit runtime approval: %w", err))
		return
	}
	a.postRuntimeApprovalDecisionNotice(approval, req.Decision, userID)
	c.Status(http.StatusNoContent)
}

func (a *API) expireRuntimeApprovals(c *gin.Context) error {
	if a.runtimeApprovalControl == nil {
		return errors.New("runtime approval control is not configured")
	}
	if err := a.runtimeApprovalControl.ExpireApprovals(c.Request.Context()); err != nil {
		return fmt.Errorf("failed to expire runtime approvals: %w", err)
	}
	return nil
}

func (a *API) handleRuntimeSessionAction(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	if a.runtimeApprovalControl == nil {
		c.AbortWithError(http.StatusInternalServerError, errors.New("runtime control is not configured"))
		return
	}

	sessionID := c.Param("sessionID")
	if sessionID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime session id is required"))
		return
	}

	var req runtimeSessionActionRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action != "resume" && req.Action != "stop" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime session action must be resume or stop"))
		return
	}

	session, err := a.runtimeStore.GetRuntimeSession(sessionID)
	if err != nil {
		if errors.Is(err, store.ErrRuntimeSessionNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime session: %w", err))
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if userID == "" || session.UserID != userID {
		c.AbortWithError(http.StatusForbidden, errors.New("user cannot manage this runtime session"))
		return
	}

	audit.AddParam(auditRec(c), "runtime_session_id", audit.TruncateID(session.ID))
	audit.AddParam(auditRec(c), "runtime_session_action", req.Action)
	audit.AddParam(auditRec(c), "runtime_type", string(session.RuntimeType))

	switch req.Action {
	case "stop":
		if err := a.runtimeApprovalControl.StopSession(c.Request.Context(), session.ID); err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to stop runtime session: %w", err))
			return
		}
	case "resume":
		events, err := a.runtimeApprovalControl.ResumeSession(c.Request.Context(), session.ID)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to resume runtime session: %w", err))
			return
		}
		a.postRuntimeSessionNotice(session, "Runtime session resumed.")
		go a.postRuntimeResumeEvents(session, events)
	}

	c.Status(http.StatusNoContent)
}

func (a *API) handleAdminRuntimeSessionAction(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime sessions") {
		return
	}
	if a.runtimeApprovalControl == nil {
		c.AbortWithError(http.StatusInternalServerError, errors.New("runtime control is not configured"))
		return
	}

	sessionID := c.Param("sessionID")
	if sessionID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime session id is required"))
		return
	}

	var req runtimeSessionActionRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action != "resume" && req.Action != "stop" {
		c.AbortWithError(http.StatusBadRequest, errors.New("runtime session action must be resume or stop"))
		return
	}

	session, err := a.runtimeStore.GetRuntimeSession(sessionID)
	if err != nil {
		if errors.Is(err, store.ErrRuntimeSessionNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime session: %w", err))
		return
	}

	audit.AddParam(auditRec(c), "runtime_session_id", audit.TruncateID(session.ID))
	audit.AddParam(auditRec(c), "runtime_session_action", req.Action)
	audit.AddParam(auditRec(c), "runtime_type", string(session.RuntimeType))
	audit.AddParam(auditRec(c), "runtime_session_owner", audit.TruncateID(session.UserID))

	switch req.Action {
	case "stop":
		if err := a.runtimeApprovalControl.StopSession(c.Request.Context(), session.ID); err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to stop runtime session: %w", err))
			return
		}
	case "resume":
		events, err := a.runtimeApprovalControl.ResumeSession(c.Request.Context(), session.ID)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to resume runtime session: %w", err))
			return
		}
		a.postRuntimeSessionNotice(session, "Runtime session resumed by an administrator.")
		go a.postRuntimeResumeEvents(session, events)
	}

	c.Status(http.StatusNoContent)
}

func (a *API) postRuntimeApprovalDecisionNotice(approval *agentruntime.RuntimeApproval, decision string, userID string) {
	if approval == nil {
		return
	}
	session, err := a.runtimeStore.GetRuntimeSession(approval.RuntimeSessionID)
	if err != nil {
		if a.mmClient != nil {
			a.mmClient.LogWarn("Failed to load runtime session for approval decision notice", "error", err, "runtime_session_id", approval.RuntimeSessionID)
		}
		return
	}
	verb := "denied"
	if decision == agentruntime.ApprovalDecisionAccept {
		verb = "accepted"
	}
	suffix := ""
	if userID != "" {
		suffix = fmt.Sprintf(" by %s", userID)
	}
	summary := runtimeApprovalNoticeSummary(approval)
	if summary != "" {
		a.postRuntimeSessionNotice(session, fmt.Sprintf("Runtime approval %s%s: %s", verb, suffix, summary))
		return
	}
	a.postRuntimeSessionNotice(session, fmt.Sprintf("Runtime approval %s%s.", verb, suffix))
}

func (a *API) postRuntimeSessionNotice(session *agentruntime.RuntimeSession, message string) {
	if a == nil || a.mmClient == nil || session == nil || session.ChannelID == "" || strings.TrimSpace(message) == "" {
		return
	}
	post := &model.Post{
		ChannelId: session.ChannelID,
		RootId:    session.RootPostID,
		Message:   message,
	}
	if err := a.mmClient.CreatePost(post); err != nil {
		a.mmClient.LogWarn("Failed to post runtime session notice", "error", err, "runtime_session_id", session.ID)
	}
}

func (a *API) postRuntimeResumeEvents(session *agentruntime.RuntimeSession, events <-chan agentruntime.RuntimeEvent) {
	if events == nil {
		return
	}
	var text strings.Builder
	waitingPosted := false
	for event := range events {
		switch event.Type {
		case agentruntime.EventTypeTextDelta:
			appendRuntimeNoticeText(&text, event.Text)
		case agentruntime.EventTypeReasoningDelta:
			continue
		case agentruntime.EventTypeApprovalRequested:
			if !waitingPosted {
				waitingPosted = true
				a.postRuntimeSessionNotice(session, "Runtime session is waiting for approval.")
			}
		case agentruntime.EventTypeStatus:
			if event.Text == string(agentruntime.SessionStatusWaitingApproval) && !waitingPosted {
				waitingPosted = true
				a.postRuntimeSessionNotice(session, "Runtime session is waiting for approval.")
			}
		case agentruntime.EventTypeError, agentruntime.EventTypeSubagentFailed:
			message := event.Text
			if event.Err != nil {
				message = event.Err.Error()
			}
			a.postRuntimeSessionNotice(session, fmt.Sprintf("Runtime session failed: %s", truncateRuntimeNotice(message)))
			return
		case agentruntime.EventTypeCompleted, agentruntime.EventTypeSubagentCompleted:
			message := "Runtime session completed."
			if summary := strings.TrimSpace(text.String()); summary != "" {
				message = fmt.Sprintf("Runtime session completed.\n\n%s", truncateRuntimeNotice(summary))
			}
			a.postRuntimeSessionNotice(session, message)
			return
		case agentruntime.EventTypeCancelled:
			a.postRuntimeSessionNotice(session, "Runtime session cancelled.")
			return
		}
	}
}

func appendRuntimeNoticeText(builder *strings.Builder, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString("\n")
	}
	builder.WriteString(value)
}

func truncateRuntimeNotice(value string) string {
	const maxRuntimeNoticeLength = 3500
	value = strings.TrimSpace(value)
	if len(value) <= maxRuntimeNoticeLength {
		return value
	}
	return value[:maxRuntimeNoticeLength-3] + "..."
}

func runtimeApprovalNoticeSummary(approval *agentruntime.RuntimeApproval) string {
	if approval == nil {
		return ""
	}
	payload := map[string]any{}
	if len(approval.RequestPayload) > 0 {
		_ = json.Unmarshal(approval.RequestPayload, &payload)
	}
	params, _ := payload["params"].(map[string]any)
	command := firstRuntimeString(params["command"], payload["command"], params["cmd"], payload["cmd"])
	if command != "" {
		return truncateRuntimeNotice("Command: " + command)
	}
	path := firstRuntimeString(params["path"], payload["path"], params["filePath"], payload["filePath"], params["cwd"], payload["cwd"])
	action := firstRuntimeString(params["action"], payload["action"], params["operation"], payload["operation"])
	if path != "" && action != "" {
		return truncateRuntimeNotice(action + ": " + path)
	}
	if path != "" {
		return truncateRuntimeNotice("File: " + path)
	}
	tool := firstRuntimeString(params["tool"], payload["tool"], params["toolName"], payload["toolName"])
	if tool != "" {
		return truncateRuntimeNotice("Tool: " + tool)
	}
	method := firstRuntimeString(payload["method"], payload["type"])
	if method != "" {
		return truncateRuntimeNotice("Request: " + method)
	}
	return ""
}

func firstRuntimeString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func (a *API) handleRuntimeHealth(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime health") {
		return
	}

	health, err := a.runtimeHealth()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, health)
}

func (a *API) handleHermesOffChecklist(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "Hermes-off checklist") {
		return
	}

	health, err := a.runtimeHealth()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	states, err := a.runtimeStore.ListHermesOffChecklistStates()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list Hermes-off checklist states: %w", err))
		return
	}

	checklist := runtimeHermesOffChecklist(*health, store.HermesOffChecklistStatesByKey(states), model.GetMillis())
	c.JSON(http.StatusOK, checklist)
}

func (a *API) handleUpdateHermesOffChecklistItem(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "Hermes-off checklist") {
		return
	}

	itemKey := c.Param("itemKey")
	if itemKey == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("checklist item key is required"))
		return
	}

	var req hermesOffChecklistUpdateRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if req.Status != agentruntime.ChecklistStatusOK && req.Status != agentruntime.ChecklistStatusManual {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid checklist status %q", req.Status))
		return
	}
	if req.Status == agentruntime.ChecklistStatusOK && strings.TrimSpace(req.Detail) == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("checklist evidence is required when marking a gate done"))
		return
	}
	if !manualHermesOffChecklistItem(itemKey) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("checklist item %q is not a manual gate", itemKey))
		return
	}

	state := &agentruntime.HermesOffChecklistItemState{
		Key:       itemKey,
		Status:    req.Status,
		Detail:    strings.TrimSpace(req.Detail),
		UpdatedBy: userID,
	}
	audit.AddParam(auditRec(c), "checklist_item", itemKey)
	audit.AddParam(auditRec(c), "checklist_status", string(req.Status))
	if err := a.runtimeStore.UpsertHermesOffChecklistState(state); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to update Hermes-off checklist item: %w", err))
		return
	}

	c.JSON(http.StatusOK, state)
}

func (a *API) runtimeHealth() (*runtimeHealthResponse, error) {
	sessions, err := a.runtimeStore.ListRuntimeSessions(agentruntime.RuntimeSessionFilter{Limit: maxRuntimeSessionsPageSize})
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime sessions for health: %w", err)
	}
	sessionStatusCounts := map[string]int{}
	sessionRuntimeCounts := map[agentruntime.RuntimeType]int{}
	var usage agentruntime.RuntimeUsage
	activeSessions := 0
	for _, session := range sessions {
		sessionStatusCounts[string(session.Status)]++
		sessionRuntimeCounts[session.RuntimeType]++
		usage = addRuntimeUsage(usage, runtimeUsageFromMetadata(session.Metadata))
		if activeRuntimeSessionStatus(session.Status) {
			activeSessions++
		}
	}

	tasksByStatus := map[agentruntime.TaskStatus]int{}
	activeTasks := 0
	for _, status := range []agentruntime.TaskStatus{agentruntime.TaskStatusQueued, agentruntime.TaskStatusRunning, agentruntime.TaskStatusWaitingApproval} {
		tasks, err := a.runtimeStore.ListTasks(store.TaskFilter{Status: status, Limit: maxRuntimeSessionsPageSize})
		if err != nil {
			return nil, fmt.Errorf("failed to list runtime tasks for health: %w", err)
		}
		tasksByStatus[status] = len(tasks)
		activeTasks += len(tasks)
	}

	if a.runtimeApprovalControl != nil {
		if err := a.runtimeApprovalControl.ExpireApprovals(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to expire runtime approvals for health: %w", err)
		}
	}
	approvals, err := a.runtimeStore.ListRuntimeApprovals(store.RuntimeApprovalFilter{
		Status: agentruntime.ApprovalStatusPending,
		Limit:  maxRuntimeSessionsPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime approvals for health: %w", err)
	}

	supervisorsByStatus := map[string]int{}
	activeSupervisorRuns := 0
	for _, status := range []string{agentruntime.RunStatusPending, agentruntime.RunStatusRunning, agentruntime.RunStatusWaitingApproval} {
		runs, err := a.runtimeStore.ListSupervisorRuns(store.SupervisorRunFilter{
			Status: status,
			Limit:  maxRuntimeSessionsPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list supervisor runs for health: %w", err)
		}
		supervisorsByStatus[status] = len(runs)
		activeSupervisorRuns += len(runs)
	}

	runtimePolicies, err := a.runtimeStore.ListRuntimePolicies()
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime policies for health: %w", err)
	}
	workspacePolicies, err := a.runtimeStore.ListWorkspacePolicies()
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace policies for health: %w", err)
	}

	voice := a.runtimeVoiceHealth()
	recovery := a.runtimeRecoveryHealth()
	readiness := runtimeReadinessChecks(runtimePolicies, workspacePolicies, voice, recovery)

	return &runtimeHealthResponse{
		Enabled:               true,
		Healthy:               true,
		HermesOffReady:        runtimeHermesOffReady(readiness),
		Readiness:             readiness,
		ActiveSessions:        activeSessions,
		SessionsByStatus:      sessionStatusCounts,
		SessionsByRuntimeType: sessionRuntimeCounts,
		Usage:                 usage,
		ActiveTasks:           activeTasks,
		TasksByStatus:         tasksByStatus,
		PendingApprovals:      len(approvals),
		ActiveSupervisorRuns:  activeSupervisorRuns,
		SupervisorsByStatus:   supervisorsByStatus,
		Voice:                 voice,
		Recovery:              recovery,
	}, nil
}

func runtimeHermesOffChecklist(health runtimeHealthResponse, states map[string]agentruntime.HermesOffChecklistItemState, generatedAt int64) hermesOffChecklistResponse {
	readinessItems := make([]hermesOffChecklistItem, 0, len(health.Readiness))
	for _, check := range health.Readiness {
		readinessItems = append(readinessItems, hermesOffChecklistItem{
			Key:      check.Key,
			Label:    check.Label,
			Status:   check.Status,
			Required: check.Required,
			Manual:   false,
			Detail:   check.Detail,
		})
	}

	groups := []hermesOffChecklistGroup{
		{
			Key:   "runtime_readiness",
			Label: "Runtime readiness",
			Items: readinessItems,
		},
		{
			Key:   "parallel_operation",
			Label: "Parallel operation",
			Items: []hermesOffChecklistItem{
				manualChecklistItem(states, "hermes_feature_mapping", "Hermes feature mapping", "map actively used Hermes workflows to Mattermost Agents capabilities"),
				manualChecklistItem(states, "test_channel_soak", "Test channel soak", "run a representative test channel without Hermes for 7 days"),
				manualChecklistItem(states, "codex_runtime_e2e", "CodexRuntime E2E", "complete a real Mattermost task through the target Codex instance"),
				manualChecklistItem(states, "local_runtime_e2e", "LocalRuntime E2E", "complete a real Mattermost task through the local LLM provider"),
			},
		},
		{
			Key:   "hermes_capability_migration",
			Label: "Hermes capability migration",
			Items: []hermesOffChecklistItem{
				manualChecklistItem(states, "migration_agent_chat", "Agent chat", "verify normal channel and thread conversations run through Mattermost Agents without Hermes"),
				manualChecklistItem(states, "migration_workspace_files", "Workspace and files", "verify attachment import, workspace access, generated file attachments, and generated file links"),
				manualChecklistItem(states, "migration_reminders_tasks", "Reminders and tasks", "verify autonomous tasks, one-shot reminders, recurring reminders, and follow-up watchers"),
				manualChecklistItem(states, "migration_voice_discussion", "Voice discussion", "verify audio upload, transcription, assistant answer, and local TTS where required"),
				manualChecklistItem(states, "migration_approval_resume", "Approval and resume flow", "verify pending approvals, accept/deny decisions, provider continuation, resumed output, and thread-visible decision/resume notices"),
				manualChecklistItem(states, "migration_supervisor_subagents", "Supervisor and subagents", "verify supervisor planning, subagent execution, aggregation, stop, and restart recovery behavior"),
				manualChecklistItem(states, "migration_policy_privacy", "Policy and privacy controls", "verify channel/thread cloud-local routing, approvals, budgets, audit entries, and local-only enforcement"),
				manualChecklistItem(states, "migration_usage_admin_ops", "Usage and admin operations", "verify usage reporting, runtime health, session controls, task controls, and Hermes-off readiness reporting"),
			},
		},
		{
			Key:   "feature_validation",
			Label: "Feature validation",
			Items: []hermesOffChecklistItem{
				manualChecklistItem(states, "reminder_delivery", "Reminder delivery", "verify one-shot and recurring reminders without Hermes"),
				manualChecklistItem(states, "voice_flow", "Voice flow", "verify audio upload, transcription, answer, and local TTS where required"),
				manualChecklistItem(states, "policy_switching", "Policy switching", "verify local/cloud policy switching per channel and thread"),
				manualChecklistItem(states, "supervisor_subagents", "Supervisor subagents", "verify a supervisor run with at least two subagents"),
			},
		},
		{
			Key:   "cutover",
			Label: "Cutover",
			Items: []hermesOffChecklistItem{
				manualChecklistItem(states, "fallback_documented", "Fallback documented", "document restart/rollback steps before disabling Hermes"),
				manualChecklistItem(states, "production_channels_switched", "Production channels switched", "switch productive channels after successful parallel operation"),
				manualChecklistItem(states, "hermes_adapter_disabled", "Hermes adapter disabled", "disable the Hermes Mattermost adapter and leave Mattermost workflows running"),
			},
		},
	}

	return hermesOffChecklistResponse{
		Ready:       checklistReady(groups),
		GeneratedAt: generatedAt,
		Groups:      groups,
		Health:      health,
	}
}

func manualChecklistItem(states map[string]agentruntime.HermesOffChecklistItemState, key, label, detail string) hermesOffChecklistItem {
	if state, ok := states[key]; ok && state.Status == agentruntime.ChecklistStatusOK {
		return hermesOffChecklistItem{
			Key:       key,
			Label:     label,
			Status:    string(agentruntime.ChecklistStatusOK),
			Required:  true,
			Manual:    true,
			Detail:    detail,
			Evidence:  state.Detail,
			UpdatedBy: state.UpdatedBy,
			UpdatedAt: state.UpdatedAt,
		}
	}
	if state, ok := states[key]; ok {
		return hermesOffChecklistItem{
			Key:       key,
			Label:     label,
			Status:    string(agentruntime.ChecklistStatusManual),
			Required:  true,
			Manual:    true,
			Detail:    detail,
			Evidence:  state.Detail,
			UpdatedBy: state.UpdatedBy,
			UpdatedAt: state.UpdatedAt,
		}
	}
	return hermesOffChecklistItem{
		Key:      key,
		Label:    label,
		Status:   string(agentruntime.ChecklistStatusManual),
		Required: true,
		Manual:   true,
		Detail:   detail,
	}
}

func manualHermesOffChecklistItem(key string) bool {
	for _, group := range runtimeHermesOffChecklist(runtimeHealthResponse{}, nil, 0).Groups {
		if group.Key == "runtime_readiness" {
			continue
		}
		for _, item := range group.Items {
			if item.Key == key {
				return true
			}
		}
	}
	return false
}

func checklistReady(groups []hermesOffChecklistGroup) bool {
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Required && item.Status != "ok" {
				return false
			}
		}
	}
	return true
}

func runtimeReadinessChecks(runtimePolicies []agentruntime.RuntimePolicy, workspacePolicies []agentruntime.WorkspacePolicy, voice runtimeVoiceHealthResponse, recovery runtimeRecoveryHealthResponse) []runtimeReadinessCheckResponse {
	hasCloudRuntime := false
	hasLocalRuntime := false
	hasScopedRouting := false
	for _, policy := range runtimePolicies {
		if policy.AllowCloud && (policy.RuntimeType == agentruntime.RuntimeTypeCodex || policy.RuntimeType == agentruntime.RuntimeTypeOpenAI) {
			hasCloudRuntime = true
		}
		if policy.AllowLocal && policy.RuntimeType == agentruntime.RuntimeTypeLocal {
			hasLocalRuntime = true
		}
		if policy.ScopeType == agentruntime.PolicyScopeChannel || policy.ScopeType == agentruntime.PolicyScopeThread {
			hasScopedRouting = true
		}
	}

	return []runtimeReadinessCheckResponse{
		readinessCheck("control_plane", "Runtime control plane", true, true, "enabled"),
		readinessCheck("cloud_runtime_policy", "Codex/OpenAI routing", true, hasCloudRuntime, readinessConfiguredDetail(hasCloudRuntime, "cloud runtime policy configured", "no Codex/OpenAI runtime policy configured")),
		readinessCheck("local_runtime_policy", "Local LLM routing", true, hasLocalRuntime, readinessConfiguredDetail(hasLocalRuntime, "local runtime policy configured", "no local runtime policy configured")),
		readinessCheck("channel_thread_routing", "Channel/thread routing", true, hasScopedRouting, readinessConfiguredDetail(hasScopedRouting, "channel or thread policy configured", "no channel/thread policy configured")),
		readinessCheck("workspace_policy", "Workspace access", true, len(workspacePolicies) > 0, readinessConfiguredDetail(len(workspacePolicies) > 0, "workspace policy configured", "no workspace policy configured")),
		readinessCheck("task_scheduler", "Autonomous tasks", true, true, "task store reachable"),
		readinessCheck("supervisor_orchestration", "Supervisor orchestration", true, true, "supervisor store reachable"),
		readinessCheck("approvals", "Approval flow", true, true, "approval store reachable"),
		readinessCheck("voice_discussion", "Voice discussion", true, voice.TranscriptionConfigured, readinessConfiguredDetail(voice.TranscriptionConfigured, "transcription configured", "transcription missing")),
		readinessCheck("local_voice", "Local voice path", true, voice.LocalVoiceReady, readinessConfiguredDetail(voice.LocalVoiceReady, "local STT/TTS ready", "local STT/TTS not ready")),
		readinessCheck("restart_recovery", "Restart recovery", true, recovery.Ready, recoveryReadinessDetail(recovery)),
	}
}

func readinessCheck(key, label string, required, ok bool, detail string) runtimeReadinessCheckResponse {
	status := "missing"
	if ok {
		status = "ok"
	} else if !required {
		status = "warning"
	}
	return runtimeReadinessCheckResponse{
		Key:      key,
		Label:    label,
		Status:   status,
		Required: required,
		Detail:   detail,
	}
}

func readinessConfiguredDetail(ok bool, configured, missing string) string {
	if ok {
		return configured
	}
	return missing
}

func (a *API) runtimeRecoveryHealth() runtimeRecoveryHealthResponse {
	if a == nil {
		return runtimeRecoveryHealthResponse{}
	}
	a.runtimeRecoveryMu.RLock()
	defer a.runtimeRecoveryMu.RUnlock()
	if a.runtimeRecoveryStatus == nil {
		return runtimeRecoveryHealthResponse{}
	}
	status := *a.runtimeRecoveryStatus
	return runtimeRecoveryHealthResponse{
		StartedAt:            status.StartedAt,
		CompletedAt:          status.CompletedAt,
		RuntimeRecoveryError: status.RuntimeRecoveryError,
		TaskRecoveryError:    status.TaskRecoveryError,
		Ready:                status.StartedAt > 0 && status.CompletedAt >= status.StartedAt && status.RuntimeRecoveryError == "" && status.TaskRecoveryError == "",
	}
}

func recoveryReadinessDetail(recovery runtimeRecoveryHealthResponse) string {
	if recovery.StartedAt == 0 {
		return "restart recovery has not run since plugin start"
	}
	if recovery.RuntimeRecoveryError != "" && recovery.TaskRecoveryError != "" {
		return "runtime and task recovery failed"
	}
	if recovery.RuntimeRecoveryError != "" {
		return "runtime recovery failed"
	}
	if recovery.TaskRecoveryError != "" {
		return "task recovery failed"
	}
	if recovery.CompletedAt == 0 {
		return "restart recovery still running"
	}
	return "last restart recovery completed successfully"
}

func runtimeHermesOffReady(readiness []runtimeReadinessCheckResponse) bool {
	for _, check := range readiness {
		if check.Required && check.Status != "ok" {
			return false
		}
	}
	return true
}

func runtimeUsageFromMetadata(raw json.RawMessage) agentruntime.RuntimeUsage {
	if len(raw) == 0 {
		return agentruntime.RuntimeUsage{}
	}
	var metadata struct {
		Usage agentruntime.RuntimeUsage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &metadata)
	return metadata.Usage
}

func addRuntimeUsage(a, b agentruntime.RuntimeUsage) agentruntime.RuntimeUsage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CachedReadTokens += b.CachedReadTokens
	a.CachedWriteTokens += b.CachedWriteTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.DurationMS += b.DurationMS
	a.Cost += b.Cost
	return a
}

func (a *API) runtimeVoiceHealth() runtimeVoiceHealthResponse {
	transcriptionConfigured := a.bots != nil && a.bots.HasTranscribe()
	localTranscriptionConfigured := a.bots != nil && a.bots.HasLocalTranscribe()
	textToSpeechConfigured := a.conversationsService != nil && a.conversationsService.TextToSpeechEnabled()
	localTextToSpeechConfigured := a.conversationsService != nil && a.conversationsService.TextToSpeechLocal()
	return runtimeVoiceHealthResponse{
		TranscriptionConfigured:      transcriptionConfigured,
		LocalTranscriptionConfigured: localTranscriptionConfigured,
		TextToSpeechConfigured:       textToSpeechConfigured,
		LocalTextToSpeechConfigured:  localTextToSpeechConfigured,
		LocalVoiceReady:              localTranscriptionConfigured && (!textToSpeechConfigured || localTextToSpeechConfigured),
	}
}

func filterTasksByRootPostID(tasks []agentruntime.Task, rootPostID string) []agentruntime.Task {
	if rootPostID == "" {
		return tasks
	}
	out := make([]agentruntime.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.RootPostID == rootPostID {
			out = append(out, task)
		}
	}
	return out
}

func (a *API) runtimeTaskResponses(tasks []agentruntime.Task) ([]runtimeTaskResponse, error) {
	responses := make([]runtimeTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		response := runtimeTaskResponse{Task: task}
		runs, err := a.runtimeStore.ListTaskRuns(task.ID)
		if err != nil {
			return nil, err
		}
		if len(runs) > 0 {
			response.LastRun = &runs[0]
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func runtimeTaskActionTarget(req runtimeTaskActionRequest, task *agentruntime.Task, now int64) (agentruntime.TaskStatus, int64, bool, error) {
	switch req.Action {
	case "run":
		return agentruntime.TaskStatusQueued, now, false, nil
	case "pause":
		return agentruntime.TaskStatusPaused, -1, false, nil
	case "resume":
		nextRunAt := task.NextRunAt
		if nextRunAt <= now {
			nextRunAt = now + time.Minute.Milliseconds()
		}
		return agentruntime.TaskStatusQueued, nextRunAt, false, nil
	case "stop", "cancel":
		return agentruntime.TaskStatusCancelled, 0, false, nil
	case "snooze":
		if req.SnoozeMs <= 0 && req.NextRunAt <= now {
			return "", 0, false, errors.New("snooze requires a future nextRunAt or positive snoozeMs")
		}
		nextRunAt := req.NextRunAt
		if req.SnoozeMs > 0 {
			nextRunAt = now + req.SnoozeMs
		}
		return agentruntime.TaskStatusQueued, nextRunAt, false, nil
	case "delete":
		return task.Status, -1, true, nil
	default:
		return "", 0, false, errors.New("task action must be run, pause, resume, stop, cancel, snooze, or delete")
	}
}

func (a *API) handleListRuntimePolicies(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime policies") {
		return
	}

	policies, err := a.runtimeStore.ListRuntimePolicies()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list runtime policies: %w", err))
		return
	}
	c.JSON(http.StatusOK, policies)
}

func (a *API) handleUpsertRuntimePolicy(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "runtime policies") {
		return
	}

	scopeType := agentruntime.PolicyScopeType(c.Param("scopeType"))
	scopeID := c.Param("scopeID")
	if scopeID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("scope id is required"))
		return
	}
	if !validRuntimePolicyScope(scopeType) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid runtime policy scope %q", scopeType))
		return
	}

	var req runtimePolicyRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	if !validRuntimeType(req.RuntimeType) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid runtime type %q", req.RuntimeType))
		return
	}
	if err := validateCloudEscalationConfirmation(req); err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	policy := runtimePolicyFromRequest(req, scopeType, scopeID, userID)
	if existing, err := a.runtimeStore.GetRuntimePolicy(scopeType, scopeID); err == nil {
		policy.ID = existing.ID
		policy.CreatedAt = existing.CreatedAt
		policy.CreatedBy = existing.CreatedBy
	} else if !errors.Is(err, store.ErrRuntimePolicyNotFound) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to get runtime policy: %w", err))
		return
	}

	audit.AddParam(auditRec(c), "scope_type", string(scopeType))
	audit.AddParam(auditRec(c), "scope_id", scopeID)
	audit.AddParam(auditRec(c), "runtime_type", string(req.RuntimeType))
	audit.AddParam(auditRec(c), "cloud_escalation_confirmed", strconv.FormatBool(req.CloudEscalationConfirmed))

	if err := a.runtimeStore.UpsertRuntimePolicy(policy); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to save runtime policy: %w", err))
		return
	}

	c.JSON(http.StatusOK, policy)
}

func validateCloudEscalationConfirmation(req runtimePolicyRequest) error {
	if !runtimePolicyCanUseCloud(req) || req.CloudEscalationConfirmed {
		return nil
	}
	return errors.New("cloud runtime escalation must be explicitly confirmed")
}

func runtimePolicyCanUseCloud(req runtimePolicyRequest) bool {
	switch req.RuntimeType {
	case agentruntime.RuntimeTypeCodex, agentruntime.RuntimeTypeOpenAI:
		return true
	default:
		return req.AllowCloud
	}
}

func runtimePolicyFromRequest(req runtimePolicyRequest, scopeType agentruntime.PolicyScopeType, scopeID, userID string) *agentruntime.RuntimePolicy {
	return &agentruntime.RuntimePolicy{
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		RuntimeType:       req.RuntimeType,
		ProviderID:        req.ProviderID,
		Model:             req.Model,
		WorkspacePolicyID: req.WorkspacePolicyID,
		ApprovalPolicyID:  req.ApprovalPolicyID,
		AllowCloud:        req.AllowCloud,
		AllowLocal:        req.AllowLocal,
		CloudBudgetCents:  req.CloudBudgetCents,
		CloudBudgetWindow: req.CloudBudgetWindow,
		CreatedBy:         userID,
		UpdatedBy:         userID,
	}
}

func isUserScopedRuntimePolicyScope(scopeType agentruntime.PolicyScopeType) bool {
	return scopeType == agentruntime.PolicyScopeChannel || scopeType == agentruntime.PolicyScopeThread
}

func (a *API) requireRuntimePolicyScopePermission(c *gin.Context, scopeType agentruntime.PolicyScopeType, scopeID string, write bool) error {
	if scopeID == "" {
		return errors.New("scope id is required")
	}

	userID := c.GetHeader("Mattermost-User-Id")
	channelID := scopeID
	if scopeType == agentruntime.PolicyScopeThread {
		post, err := a.mmClient.GetPost(scopeID)
		if err != nil {
			return fmt.Errorf("failed to get thread root post: %w", err)
		}
		channelID = post.ChannelId
	}

	channel, err := a.mmClient.GetChannel(channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	if !a.mmClient.HasPermissionToChannel(userID, channel.Id, model.PermissionReadChannel) {
		return errors.New("user does not have channel access")
	}
	if !write {
		return nil
	}
	permission := model.PermissionManagePublicChannelProperties
	if channel.Type == model.ChannelTypePrivate || channel.Type == model.ChannelTypeGroup || channel.Type == model.ChannelTypeDirect {
		permission = model.PermissionManagePrivateChannelProperties
	}
	if !a.mmClient.HasPermissionToChannel(userID, channel.Id, permission) {
		return errors.New("user cannot manage runtime policy for this channel")
	}
	return nil
}

func (a *API) handleListWorkspacePolicies(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}
	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "workspace policies") {
		return
	}

	policies, err := a.runtimeStore.ListWorkspacePolicies()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to list workspace policies: %w", err))
		return
	}
	c.JSON(http.StatusOK, policies)
}

func (a *API) handleCreateWorkspacePolicy(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "workspace policies") {
		return
	}

	var req workspacePolicyRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	policy, err := workspacePolicyFromRequest(req)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	audit.AddParam(auditRec(c), "workspace_policy_name", policy.Name)
	audit.AddParam(auditRec(c), "workspace_mode", string(policy.Mode))
	if err := a.runtimeStore.CreateWorkspacePolicy(policy); err != nil {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to create workspace policy: %w", err))
		return
	}
	c.JSON(http.StatusCreated, policy)
}

func (a *API) handleUpdateWorkspacePolicy(c *gin.Context) {
	if !a.runtimeControlEnabled(c) {
		return
	}

	userID := c.GetHeader("Mattermost-User-Id")
	if !a.requireRuntimeAdmin(c, userID, "workspace policies") {
		return
	}

	policyID := c.Param("policyID")
	if policyID == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("workspace policy id is required"))
		return
	}

	var req workspacePolicyRequest
	if err := c.BindJSON(&req); err != nil {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	policy, err := workspacePolicyFromRequest(req)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	policy.ID = policyID

	audit.AddParam(auditRec(c), "workspace_policy_id", policyID)
	audit.AddParam(auditRec(c), "workspace_policy_name", policy.Name)
	audit.AddParam(auditRec(c), "workspace_mode", string(policy.Mode))
	if err := a.runtimeStore.UpdateWorkspacePolicy(policy); err != nil {
		if errors.Is(err, store.ErrWorkspacePolicyNotFound) {
			c.AbortWithError(http.StatusNotFound, err)
			return
		}
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("failed to update workspace policy: %w", err))
		return
	}
	c.JSON(http.StatusOK, policy)
}

func (a *API) runtimeControlEnabled(c *gin.Context) bool {
	if a.config == nil || !a.config.EnableAgentRuntimeControlPlane() {
		c.AbortWithError(http.StatusForbidden, errors.New("agent runtime control plane is disabled"))
		return false
	}
	if a.runtimeStore == nil {
		c.AbortWithError(http.StatusInternalServerError, errors.New("runtime store is not configured"))
		return false
	}
	return true
}

func (a *API) requireRuntimeAdmin(c *gin.Context, userID, resource string) bool {
	if a.mmClient == nil || !a.mmClient.HasPermissionTo(userID, model.PermissionManageSystem) {
		c.AbortWithError(http.StatusForbidden, fmt.Errorf("user cannot manage %s", resource))
		return false
	}
	return true
}

func parseRuntimeLimit(raw string) int {
	limit := 60
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit <= 0 || limit > maxRuntimeSessionsPageSize {
		return maxRuntimeSessionsPageSize
	}
	return limit
}

func validRuntimePolicyScope(scopeType agentruntime.PolicyScopeType) bool {
	switch scopeType {
	case agentruntime.PolicyScopeServer,
		agentruntime.PolicyScopeTeam,
		agentruntime.PolicyScopeChannel,
		agentruntime.PolicyScopeThread,
		agentruntime.PolicyScopeUser,
		agentruntime.PolicyScopeAgent:
		return true
	default:
		return false
	}
}

func validRuntimeType(runtimeType agentruntime.RuntimeType) bool {
	switch runtimeType {
	case agentruntime.RuntimeTypeInherit,
		agentruntime.RuntimeTypeCodex,
		agentruntime.RuntimeTypeOpenAI,
		agentruntime.RuntimeTypeLocal:
		return true
	default:
		return false
	}
}

func validRuntimeApprovalDecision(decision string) bool {
	return decision == agentruntime.ApprovalDecisionAccept || decision == agentruntime.ApprovalDecisionDeny
}

func activeRuntimeSessionStatus(status agentruntime.RuntimeSessionStatus) bool {
	switch status {
	case agentruntime.SessionStatusIdle,
		agentruntime.SessionStatusRunning,
		agentruntime.SessionStatusWaitingApproval:
		return true
	default:
		return false
	}
}

func workspacePolicyFromRequest(req workspacePolicyRequest) (*agentruntime.WorkspacePolicy, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, errors.New("workspace policy name is required")
	}
	if req.Mode == "" {
		req.Mode = agentruntime.WorkspaceModeAskWrite
	}
	if req.NetworkMode == "" {
		req.NetworkMode = agentruntime.WorkspaceNetworkModeAsk
	}
	if req.ShellMode == "" {
		req.ShellMode = agentruntime.WorkspaceShellModeAsk
	}
	if !validWorkspaceMode(req.Mode) {
		return nil, fmt.Errorf("invalid workspace mode %q", req.Mode)
	}
	if !validWorkspaceNetworkMode(req.NetworkMode) {
		return nil, fmt.Errorf("invalid workspace network mode %q", req.NetworkMode)
	}
	if !validWorkspaceShellMode(req.ShellMode) {
		return nil, fmt.Errorf("invalid workspace shell mode %q", req.ShellMode)
	}

	allowedRootsValue, err := normalizeWorkspacePolicyRoots(req.AllowedRoots)
	if err != nil {
		return nil, err
	}
	allowedRoots, err := json.Marshal(allowedRootsValue)
	if err != nil {
		return nil, fmt.Errorf("failed to encode allowed roots: %w", err)
	}
	req.DefaultWorkspacePath = strings.TrimSpace(req.DefaultWorkspacePath)
	if req.DefaultWorkspacePath != "" {
		if !filepath.IsAbs(req.DefaultWorkspacePath) {
			return nil, errors.New("workspace policy default workspace path must be absolute")
		}
		req.DefaultWorkspacePath = filepath.Clean(req.DefaultWorkspacePath)
		if _, err := agentruntime.ResolveWorkspacePath(req.DefaultWorkspacePath, &agentruntime.WorkspacePolicy{AllowedRoots: allowedRoots}); err != nil {
			return nil, err
		}
	}
	metadataValue := req.Metadata
	if metadataValue == nil {
		metadataValue = map[string]any{}
	}
	metadata, err := json.Marshal(metadataValue)
	if err != nil {
		return nil, fmt.Errorf("failed to encode workspace metadata: %w", err)
	}

	return &agentruntime.WorkspacePolicy{
		Name:                 req.Name,
		AllowedRoots:         allowedRoots,
		DefaultWorkspacePath: req.DefaultWorkspacePath,
		Mode:                 req.Mode,
		NetworkMode:          req.NetworkMode,
		ShellMode:            req.ShellMode,
		Metadata:             metadata,
	}, nil
}

func normalizeWorkspacePolicyRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, errors.New("workspace policy requires at least one allowed root")
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return nil, errors.New("workspace policy allowed roots must be absolute")
		}
		root = filepath.Clean(root)
		if seen[root] {
			continue
		}
		seen[root] = true
		normalized = append(normalized, root)
	}
	if len(normalized) == 0 {
		return nil, errors.New("workspace policy requires at least one allowed root")
	}
	return normalized, nil
}

func validWorkspaceMode(mode agentruntime.WorkspaceMode) bool {
	switch mode {
	case agentruntime.WorkspaceModeReadOnly,
		agentruntime.WorkspaceModeAskWrite,
		agentruntime.WorkspaceModeFullAccess:
		return true
	default:
		return false
	}
}

func validWorkspaceNetworkMode(mode agentruntime.WorkspaceNetworkMode) bool {
	switch mode {
	case agentruntime.WorkspaceNetworkModeNone,
		agentruntime.WorkspaceNetworkModeAsk,
		agentruntime.WorkspaceNetworkModeAllowed:
		return true
	default:
		return false
	}
}

func validWorkspaceShellMode(mode agentruntime.WorkspaceShellMode) bool {
	switch mode {
	case agentruntime.WorkspaceShellModeDisabled,
		agentruntime.WorkspaceShellModeAsk,
		agentruntime.WorkspaceShellModeAllowed:
		return true
	default:
		return false
	}
}
