// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mattermost/mattermost-plugin-agents/v2/llm"
)

const (
	RuntimeTypeInherit RuntimeType = "inherit"
	RuntimeTypeCodex   RuntimeType = "codex"
	RuntimeTypeOpenAI  RuntimeType = "openai"
	RuntimeTypeLocal   RuntimeType = "local"

	SessionStatusIdle            RuntimeSessionStatus = "idle"
	SessionStatusRunning         RuntimeSessionStatus = "running"
	SessionStatusWaitingApproval RuntimeSessionStatus = "waiting_approval"
	SessionStatusCompleted       RuntimeSessionStatus = "completed"
	SessionStatusFailed          RuntimeSessionStatus = "failed"
	SessionStatusCancelled       RuntimeSessionStatus = "cancelled"

	RunStatusPending         = "pending"
	RunStatusRunning         = "running"
	RunStatusWaitingApproval = "waiting_approval"
	RunStatusCompleted       = "completed"
	RunStatusFailed          = "failed"
	RunStatusCancelled       = "cancelled"

	TaskTypeOneShot   TaskType = "one_shot"
	TaskTypeReminder  TaskType = "reminder"
	TaskTypeRecurring TaskType = "recurring"
	TaskTypeWatcher   TaskType = "watcher"

	TaskStatusQueued          TaskStatus = "queued"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusPaused          TaskStatus = "paused"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCancelled       TaskStatus = "cancelled"

	ApprovalStatusPending  RuntimeApprovalStatus = "pending"
	ApprovalStatusAccepted RuntimeApprovalStatus = "accepted"
	ApprovalStatusDenied   RuntimeApprovalStatus = "denied"
	ApprovalStatusExpired  RuntimeApprovalStatus = "expired"

	ApprovalDecisionAccept = "accept"
	ApprovalDecisionDeny   = "deny"

	ChecklistStatusManual ChecklistStatus = "manual"
	ChecklistStatusOK     ChecklistStatus = "ok"

	EventTypeTextDelta         RuntimeEventType = "text_delta"
	EventTypeReasoningDelta    RuntimeEventType = "reasoning_delta"
	EventTypeToolCallRequested RuntimeEventType = "tool_call_requested"
	EventTypeToolCallStarted   RuntimeEventType = "tool_call_started"
	EventTypeToolCallCompleted RuntimeEventType = "tool_call_completed"
	EventTypeApprovalRequested RuntimeEventType = "approval_requested"
	EventTypeApprovalResolved  RuntimeEventType = "approval_resolved"
	EventTypeFileCreated       RuntimeEventType = "file_created"
	EventTypeUsage             RuntimeEventType = "usage"
	EventTypeStatus            RuntimeEventType = "status"
	EventTypeError             RuntimeEventType = "error"
	EventTypeCompleted         RuntimeEventType = "completed"
	EventTypeCancelled         RuntimeEventType = "cancelled"
	EventTypeSubagentStarted   RuntimeEventType = "subagent_started"
	EventTypeSubagentEvent     RuntimeEventType = "subagent_event"
	EventTypeSubagentCompleted RuntimeEventType = "subagent_completed"
	EventTypeSubagentFailed    RuntimeEventType = "subagent_failed"

	PolicyScopeServer  PolicyScopeType = "server"
	PolicyScopeTeam    PolicyScopeType = "team"
	PolicyScopeChannel PolicyScopeType = "channel"
	PolicyScopeThread  PolicyScopeType = "thread"
	PolicyScopeUser    PolicyScopeType = "user"
	PolicyScopeAgent   PolicyScopeType = "agent"

	WorkspaceModeReadOnly   WorkspaceMode = "read_only"
	WorkspaceModeAskWrite   WorkspaceMode = "ask_write"
	WorkspaceModeFullAccess WorkspaceMode = "full_access"

	WorkspaceNetworkModeNone    WorkspaceNetworkMode = "none"
	WorkspaceNetworkModeAsk     WorkspaceNetworkMode = "ask"
	WorkspaceNetworkModeAllowed WorkspaceNetworkMode = "allowed"

	WorkspaceShellModeDisabled WorkspaceShellMode = "disabled"
	WorkspaceShellModeAsk      WorkspaceShellMode = "ask"
	WorkspaceShellModeAllowed  WorkspaceShellMode = "allowed"
)

var (
	ErrRuntimeUnavailable  = errors.New("agent runtime unavailable")
	ErrSessionNotFound     = errors.New("runtime session not found")
	ErrPolicyNotFound      = errors.New("runtime policy not found")
	ErrCloudNotAllowed     = errors.New("cloud runtime is not allowed in this scope")
	ErrLocalNotAllowed     = errors.New("local runtime is not allowed in this scope")
	ErrWorkspaceNotAllowed = errors.New("workspace is not allowed by policy")
	ErrBudgetExceeded      = errors.New("runtime budget exceeded")
	ErrApprovalUnsupported = errors.New("runtime approval submission is not supported")
)

type RuntimeType string
type RuntimeSessionStatus string
type RuntimeEventType string
type PolicyScopeType string
type TaskType string
type TaskStatus string
type RuntimeApprovalStatus string
type ChecklistStatus string
type WorkspaceMode string
type WorkspaceNetworkMode string
type WorkspaceShellMode string

type RuntimeTurnRequest struct {
	Session           RuntimeSession
	Prompt            string
	Context           *llm.Context
	ShouldExecuteTool func(llm.ToolCall) bool
	Attachments       []RuntimeAttachment
	Metadata          json.RawMessage
	SupervisorRun     *SupervisorRun
}

type SubagentRequest struct {
	SupervisorRunID string
	SubagentRunID   string
	ParentRunID     string
	Role            string
	Title           string
	Prompt          string
	Session         RuntimeSession
	Metadata        json.RawMessage
}

type RuntimeAttachment struct {
	FileID      string
	Name        string
	MimeType    string
	LocalPath   string
	DownloadURL string
}

type RuntimeEvent struct {
	Type              RuntimeEventType
	SessionID         string
	ExternalSessionID string
	SubagentRunID     string
	Text              string
	Payload           json.RawMessage
	Err               error
}

type RuntimeUsage struct {
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CachedReadTokens  int64   `json:"cached_read_tokens,omitempty"`
	CachedWriteTokens int64   `json:"cached_write_tokens,omitempty"`
	ReasoningTokens   int64   `json:"reasoning_tokens,omitempty"`
	DurationMS        int64   `json:"duration_ms"`
	Cost              float64 `json:"cost,omitempty"`
}

type RuntimeCostRate struct {
	RuntimeType           RuntimeType `json:"runtimeType"`
	ProviderID            string      `json:"providerID"`
	Model                 string      `json:"model"`
	InputPerMillion       float64     `json:"inputPerMillion"`
	CachedReadPerMillion  float64     `json:"cachedReadPerMillion"`
	CachedWritePerMillion float64     `json:"cachedWritePerMillion"`
	OutputPerMillion      float64     `json:"outputPerMillion"`
}

type RuntimeFileCreatedPayload struct {
	Path          string `json:"path,omitempty"`
	FileName      string `json:"fileName,omitempty"`
	MimeType      string `json:"mimeType,omitempty"`
	Content       string `json:"content,omitempty"`
	ContentBase64 string `json:"contentBase64,omitempty"`
}

type RuntimeStatus struct {
	SessionID     string
	Status        RuntimeSessionStatus
	RuntimeType   RuntimeType
	ProviderID    string
	Model         string
	WorkspacePath string
	LastError     string
}

type RuntimeSessionFilter struct {
	ServerID       string
	TeamID         string
	UserID         string
	AgentID        string
	ChannelID      string
	RootPostID     string
	ConversationID string
	Status         RuntimeSessionStatus
	CreatedAtFrom  int64
	Limit          int
}

type RuntimeApprovalDecision struct {
	SessionID     string `json:"sessionID"`
	ApprovalID    string `json:"approvalID"`
	SubagentRunID string `json:"subagentRunID,omitempty"`
	UserID        string `json:"userID"`
	Decision      string `json:"decision"`
	Scope         string `json:"scope"`
	Reason        string `json:"reason"`
}

type AgentRuntime interface {
	StartTurn(ctx context.Context, req RuntimeTurnRequest) (<-chan RuntimeEvent, error)
	StartSubagent(ctx context.Context, req SubagentRequest) (<-chan RuntimeEvent, error)
	StopTurn(ctx context.Context, sessionID string) error
	ResumeSession(ctx context.Context, sessionID string) (<-chan RuntimeEvent, error)
	GetStatus(ctx context.Context, sessionID string) (RuntimeStatus, error)
	ListSessions(ctx context.Context, filter RuntimeSessionFilter) ([]RuntimeSession, error)
	SubmitApproval(ctx context.Context, approval RuntimeApprovalDecision) error
}

type RuntimeSession struct {
	ID                       string               `db:"id" json:"id"`
	MattermostConversationID string               `db:"mattermostconversationid" json:"mattermostConversationID"`
	ServerID                 string               `db:"serverid" json:"serverID"`
	TeamID                   string               `db:"teamid" json:"teamID"`
	ChannelID                string               `db:"channelid" json:"channelID"`
	RootPostID               string               `db:"rootpostid" json:"rootPostID"`
	UserID                   string               `db:"userid" json:"userID"`
	AgentID                  string               `db:"agentid" json:"agentID"`
	RuntimeType              RuntimeType          `db:"runtimetype" json:"runtimeType"`
	ProviderID               string               `db:"providerid" json:"providerID"`
	Model                    string               `db:"model" json:"model"`
	WorkspacePath            string               `db:"workspacepath" json:"workspacePath"`
	ExternalSessionID        string               `db:"externalsessionid" json:"externalSessionID"`
	Status                   RuntimeSessionStatus `db:"status" json:"status"`
	LastError                string               `db:"lasterror" json:"lastError"`
	LastEventAt              int64                `db:"lasteventat" json:"lastEventAt"`
	CreatedAt                int64                `db:"createdat" json:"createdAt"`
	UpdatedAt                int64                `db:"updatedat" json:"updatedAt"`
	Metadata                 json.RawMessage      `db:"metadata" json:"metadata"`
}

type RuntimePolicy struct {
	ID                string          `db:"id" json:"id"`
	ScopeType         PolicyScopeType `db:"scopetype" json:"scopeType"`
	ScopeID           string          `db:"scopeid" json:"scopeID"`
	RuntimeType       RuntimeType     `db:"runtimetype" json:"runtimeType"`
	ProviderID        string          `db:"providerid" json:"providerID"`
	Model             string          `db:"model" json:"model"`
	WorkspacePolicyID string          `db:"workspacepolicyid" json:"workspacePolicyID"`
	ApprovalPolicyID  string          `db:"approvalpolicyid" json:"approvalPolicyID"`
	AllowCloud        bool            `db:"allowcloud" json:"allowCloud"`
	AllowLocal        bool            `db:"allowlocal" json:"allowLocal"`
	CloudBudgetCents  int64           `db:"cloudbudgetcents" json:"cloudBudgetCents"`
	CloudBudgetWindow string          `db:"cloudbudgetwindow" json:"cloudBudgetWindow"`
	CreatedBy         string          `db:"createdby" json:"createdBy"`
	UpdatedBy         string          `db:"updatedby" json:"updatedBy"`
	CreatedAt         int64           `db:"createdat" json:"createdAt"`
	UpdatedAt         int64           `db:"updatedat" json:"updatedAt"`
}

type HermesOffChecklistItemState struct {
	Key       string          `db:"key" json:"key"`
	Status    ChecklistStatus `db:"status" json:"status"`
	Detail    string          `db:"detail" json:"detail"`
	UpdatedBy string          `db:"updatedby" json:"updatedBy"`
	UpdatedAt int64           `db:"updatedat" json:"updatedAt"`
}

type WorkspacePolicy struct {
	ID                   string               `db:"id" json:"id"`
	Name                 string               `db:"name" json:"name"`
	AllowedRoots         json.RawMessage      `db:"allowedroots" json:"allowedRoots"`
	DefaultWorkspacePath string               `db:"defaultworkspacepath" json:"defaultWorkspacePath"`
	Mode                 WorkspaceMode        `db:"mode" json:"mode"`
	NetworkMode          WorkspaceNetworkMode `db:"networkmode" json:"networkMode"`
	ShellMode            WorkspaceShellMode   `db:"shellmode" json:"shellMode"`
	CreatedAt            int64                `db:"createdat" json:"createdAt"`
	UpdatedAt            int64                `db:"updatedat" json:"updatedAt"`
	Metadata             json.RawMessage      `db:"metadata" json:"metadata"`
}

type RuntimeApproval struct {
	ID                 string                `db:"id" json:"id"`
	RuntimeSessionID   string                `db:"runtimesessionid" json:"runtimeSessionID"`
	ExternalApprovalID string                `db:"externalapprovalid" json:"externalApprovalID"`
	SubagentRunID      string                `db:"subagentrunid" json:"subagentRunID"`
	Status             RuntimeApprovalStatus `db:"status" json:"status"`
	RequestPayload     json.RawMessage       `db:"requestpayload" json:"requestPayload"`
	Decision           string                `db:"decision" json:"decision"`
	DecisionScope      string                `db:"decisionscope" json:"decisionScope"`
	DecisionReason     string                `db:"decisionreason" json:"decisionReason"`
	RequestedBy        string                `db:"requestedby" json:"requestedBy"`
	DecidedBy          string                `db:"decidedby" json:"decidedBy"`
	ExpiresAt          int64                 `db:"expiresat" json:"expiresAt"`
	CreatedAt          int64                 `db:"createdat" json:"createdAt"`
	UpdatedAt          int64                 `db:"updatedat" json:"updatedAt"`
	Metadata           json.RawMessage       `db:"metadata" json:"metadata"`
}

type SupervisorRun struct {
	ID                       string          `db:"id" json:"id"`
	RuntimeSessionID         string          `db:"runtimesessionid" json:"runtimeSessionID"`
	MattermostConversationID string          `db:"mattermostconversationid" json:"mattermostConversationID"`
	RootTaskID               string          `db:"roottaskid" json:"rootTaskID"`
	SupervisorAgentID        string          `db:"supervisoragentid" json:"supervisorAgentID"`
	Status                   string          `db:"status" json:"status"`
	Objective                string          `db:"objective" json:"objective"`
	Plan                     json.RawMessage `db:"plan" json:"plan"`
	FinalResultPostID        string          `db:"finalresultpostid" json:"finalResultPostID"`
	CreatedBy                string          `db:"createdby" json:"createdBy"`
	CreatedAt                int64           `db:"createdat" json:"createdAt"`
	UpdatedAt                int64           `db:"updatedat" json:"updatedAt"`
	Metadata                 json.RawMessage `db:"metadata" json:"metadata"`
}

type SubagentRun struct {
	ID                  string          `db:"id" json:"id"`
	SupervisorRunID     string          `db:"supervisorrunid" json:"supervisorRunID"`
	ParentSubagentRunID string          `db:"parentsubagentrunid" json:"parentSubagentRunID"`
	Role                string          `db:"role" json:"role"`
	Title               string          `db:"title" json:"title"`
	Prompt              string          `db:"prompt" json:"prompt"`
	RuntimeType         RuntimeType     `db:"runtimetype" json:"runtimeType"`
	ProviderID          string          `db:"providerid" json:"providerID"`
	Model               string          `db:"model" json:"model"`
	WorkspacePath       string          `db:"workspacepath" json:"workspacePath"`
	ExternalSessionID   string          `db:"externalsessionid" json:"externalSessionID"`
	Status              string          `db:"status" json:"status"`
	Result              json.RawMessage `db:"result" json:"result"`
	Summary             string          `db:"summary" json:"summary"`
	StartedAt           int64           `db:"startedat" json:"startedAt"`
	FinishedAt          int64           `db:"finishedat" json:"finishedAt"`
	CreatedAt           int64           `db:"createdat" json:"createdAt"`
	UpdatedAt           int64           `db:"updatedat" json:"updatedAt"`
	Metadata            json.RawMessage `db:"metadata" json:"metadata"`
}

type Task struct {
	ID                    string          `db:"id" json:"id"`
	Title                 string          `db:"title" json:"title"`
	Prompt                string          `db:"prompt" json:"prompt"`
	TaskType              TaskType        `db:"tasktype" json:"taskType"`
	Status                TaskStatus      `db:"status" json:"status"`
	ChannelID             string          `db:"channelid" json:"channelID"`
	RootPostID            string          `db:"rootpostid" json:"rootPostID"`
	UserID                string          `db:"userid" json:"userID"`
	AgentID               string          `db:"agentid" json:"agentID"`
	RuntimePolicySnapshot json.RawMessage `db:"runtimepolicysnapshot" json:"runtimePolicySnapshot"`
	WorkspacePath         string          `db:"workspacepath" json:"workspacePath"`
	ScheduleSpec          string          `db:"schedulespec" json:"scheduleSpec"`
	NextRunAt             int64           `db:"nextrunat" json:"nextRunAt"`
	LastRunAt             int64           `db:"lastrunat" json:"lastRunAt"`
	CreatedBy             string          `db:"createdby" json:"createdBy"`
	CreatedAt             int64           `db:"createdat" json:"createdAt"`
	UpdatedAt             int64           `db:"updatedat" json:"updatedAt"`
	Metadata              json.RawMessage `db:"metadata" json:"metadata"`
}

type TaskRun struct {
	ID               string          `db:"id" json:"id"`
	TaskID           string          `db:"taskid" json:"taskID"`
	RuntimeSessionID string          `db:"runtimesessionid" json:"runtimeSessionID"`
	Status           TaskStatus      `db:"status" json:"status"`
	StartedAt        int64           `db:"startedat" json:"startedAt"`
	FinishedAt       int64           `db:"finishedat" json:"finishedAt"`
	ResultPostID     string          `db:"resultpostid" json:"resultPostID"`
	Error            string          `db:"error" json:"error"`
	Usage            json.RawMessage `db:"usage" json:"usage"`
}
