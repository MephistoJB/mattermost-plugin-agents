// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

export type RuntimeType = 'inherit' | 'codex' | 'openai' | 'local';

export type RuntimeSessionStatus = 'idle' | 'running' | 'waiting_approval' | 'completed' | 'failed' | 'cancelled';

export type TaskStatus = 'queued' | 'running' | 'waiting_approval' | 'paused' | 'completed' | 'failed' | 'cancelled';

export type RuntimeTaskAction = 'run' | 'pause' | 'resume' | 'stop' | 'cancel' | 'snooze' | 'delete';

export type RuntimeSessionAction = 'resume' | 'stop';

export type RuntimeApprovalStatus = 'pending' | 'accepted' | 'denied' | 'expired' | 'cancelled';

export type RuntimePolicy = {
    id: string;
    scopeType: string;
    scopeID: string;
    runtimeType: RuntimeType;
    providerID: string;
    model: string;
    workspacePolicyID: string;
    approvalPolicyID: string;
    allowCloud: boolean;
    allowLocal: boolean;
    cloudBudgetCents: number;
    cloudBudgetWindow: string;
    createdBy: string;
    updatedBy: string;
    createdAt: number;
    updatedAt: number;
};

export type RuntimePolicyRequest = {
    runtimeType: RuntimeType;
    providerID?: string;
    model?: string;
    workspacePolicyID?: string;
    approvalPolicyID?: string;
    allowCloud: boolean;
    allowLocal: boolean;
    cloudBudgetCents?: number;
    cloudBudgetWindow?: string;
    cloudEscalationConfirmed?: boolean;
};

export type WorkspacePolicy = {
    id: string;
    name: string;
    allowedRoots: string[];
    defaultWorkspacePath: string;
    mode: string;
    networkMode: string;
    shellMode: string;
    createdAt: number;
    updatedAt: number;
    metadata?: Record<string, unknown>;
};

export type WorkspacePolicyRequest = {
    name: string;
    allowedRoots: string[];
    defaultWorkspacePath: string;
    mode: string;
    networkMode: string;
    shellMode: string;
    metadata?: Record<string, unknown>;
};

export type RuntimeSession = {
    id: string;
    mattermostConversationID: string;
    serverID: string;
    teamID: string;
    channelID: string;
    rootPostID: string;
    userID: string;
    agentID: string;
    runtimeType: RuntimeType;
    providerID: string;
    model: string;
    workspacePath: string;
    externalSessionID: string;
    status: RuntimeSessionStatus;
    lastError: string;
    lastEventAt: number;
    createdAt: number;
    updatedAt: number;
};

export type RuntimeApproval = {
    id: string;
    runtimeSessionID: string;
    externalApprovalID: string;
    subagentRunID: string;
    status: RuntimeApprovalStatus;
    requestPayload: Record<string, unknown>;
    decision: string;
    decisionScope: string;
    decisionReason: string;
    requestedBy: string;
    decidedBy: string;
    createdAt: number;
    updatedAt: number;
    decidedAt: number;
    expiresAt: number;
};

export type RuntimeTaskRun = {
    id: string;
    taskID: string;
    runtimeSessionID: string;
    status: TaskStatus;
    startedAt: number;
    finishedAt: number;
    resultPostID: string;
    error: string;
    usage: Record<string, unknown>;
};

export type RuntimeTask = {
    id: string;
    title: string;
    prompt: string;
    taskType: string;
    status: TaskStatus;
    channelID: string;
    rootPostID: string;
    userID: string;
    agentID: string;
    workspacePath: string;
    scheduleSpec: string;
    nextRunAt: number;
    lastRunAt: number;
    createdBy: string;
    createdAt: number;
    updatedAt: number;
    lastRun?: RuntimeTaskRun;
};

export type RuntimeTaskActionRequest = {
    action: RuntimeTaskAction;
    nextRunAt?: number;
    snoozeMs?: number;
};

export type RuntimeSessionActionRequest = {
    action: RuntimeSessionAction;
};

export type SubagentRun = {
    id: string;
    supervisorRunID: string;
    parentSubagentRunID: string;
    role: string;
    title: string;
    prompt: string;
    runtimeType: RuntimeType;
    providerID: string;
    model: string;
    workspacePath: string;
    externalSessionID: string;
    status: string;
    summary: string;
    startedAt: number;
    finishedAt: number;
    createdAt: number;
    updatedAt: number;
};

export type SupervisorRun = {
    id: string;
    runtimeSessionID: string;
    mattermostConversationID: string;
    rootTaskID: string;
    supervisorAgentID: string;
    status: string;
    objective: string;
    finalResultPostID: string;
    createdBy: string;
    createdAt: number;
    updatedAt: number;
    subagents: SubagentRun[];
};

export type RuntimeReadinessCheck = {
    key: string;
    label: string;
    status: 'ok' | 'warning' | 'missing';
    required: boolean;
    detail: string;
};

export type RuntimeHealth = {
    enabled: boolean;
    healthy: boolean;
    hermesOffReady?: boolean;
    readiness?: RuntimeReadinessCheck[];
    activeSessions: number;
    sessionsByStatus: Record<string, number>;
    sessionsByRuntimeType: Partial<Record<RuntimeType, number>>;
    usage?: {
        input_tokens: number;
        output_tokens: number;
        cached_read_tokens?: number;
        cached_write_tokens?: number;
        reasoning_tokens?: number;
        duration_ms: number;
        cost?: number;
    };
    activeTasks: number;
    tasksByStatus: Partial<Record<TaskStatus, number>>;
    pendingApprovals: number;
    activeSupervisorRuns: number;
    supervisorsByStatus: Record<string, number>;
    voice?: {
        transcriptionConfigured: boolean;
        localTranscriptionConfigured: boolean;
        textToSpeechConfigured: boolean;
        localTextToSpeechConfigured: boolean;
        localVoiceReady: boolean;
    };
    recovery?: {
        startedAt: number;
        completedAt: number;
        runtimeRecoveryError?: string;
        taskRecoveryError?: string;
        ready: boolean;
    };
};

export type HermesOffChecklistItem = {
    key: string;
    label: string;
    status: 'ok' | 'warning' | 'missing' | 'manual';
    required: boolean;
    manual?: boolean;
    detail: string;
    evidence?: string;
    updatedBy?: string;
    updatedAt?: number;
};

export type HermesOffChecklistItemState = {
    key: string;
    status: 'ok' | 'manual';
    detail: string;
    updatedBy: string;
    updatedAt: number;
};

export type HermesOffChecklistUpdateRequest = {
    status: 'ok' | 'manual';
    detail?: string;
};

export type HermesOffChecklistGroup = {
    key: string;
    label: string;
    items: HermesOffChecklistItem[];
};

export type HermesOffChecklist = {
    ready: boolean;
    generatedAt: number;
    groups: HermesOffChecklistGroup[];
    health: RuntimeHealth;
};
