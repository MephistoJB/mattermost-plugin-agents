CREATE TABLE IF NOT EXISTS Agents_RuntimeSessions (
    ID TEXT PRIMARY KEY,
    MattermostConversationID TEXT NOT NULL DEFAULT '',
    ServerID TEXT NOT NULL DEFAULT '',
    TeamID TEXT NOT NULL DEFAULT '',
    ChannelID TEXT NOT NULL DEFAULT '',
    RootPostID TEXT NOT NULL DEFAULT '',
    UserID TEXT NOT NULL,
    AgentID TEXT NOT NULL,
    RuntimeType TEXT NOT NULL,
    ProviderID TEXT NOT NULL DEFAULT '',
    Model TEXT NOT NULL DEFAULT '',
    WorkspacePath TEXT NOT NULL DEFAULT '',
    ExternalSessionID TEXT NOT NULL DEFAULT '',
    Status TEXT NOT NULL,
    LastError TEXT NOT NULL DEFAULT '',
    LastEventAt BIGINT NOT NULL DEFAULT 0,
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_runtime_sessions_conversation_agent
    ON Agents_RuntimeSessions(MattermostConversationID, AgentID)
    WHERE MattermostConversationID <> '';

CREATE INDEX IF NOT EXISTS idx_agents_runtime_sessions_user_updated
    ON Agents_RuntimeSessions(UserID, UpdatedAt DESC);

CREATE INDEX IF NOT EXISTS idx_agents_runtime_sessions_server_updated
    ON Agents_RuntimeSessions(ServerID, UpdatedAt DESC)
    WHERE ServerID <> '';

CREATE INDEX IF NOT EXISTS idx_agents_runtime_sessions_team_updated
    ON Agents_RuntimeSessions(TeamID, UpdatedAt DESC)
    WHERE TeamID <> '';

CREATE INDEX IF NOT EXISTS idx_agents_runtime_sessions_thread
    ON Agents_RuntimeSessions(ChannelID, RootPostID);

CREATE TABLE IF NOT EXISTS Agents_RuntimePolicies (
    ID TEXT PRIMARY KEY,
    ScopeType TEXT NOT NULL,
    ScopeID TEXT NOT NULL,
    RuntimeType TEXT NOT NULL,
    ProviderID TEXT NOT NULL DEFAULT '',
    Model TEXT NOT NULL DEFAULT '',
    WorkspacePolicyID TEXT NOT NULL DEFAULT '',
    ApprovalPolicyID TEXT NOT NULL DEFAULT '',
    AllowCloud BOOLEAN NOT NULL DEFAULT FALSE,
    AllowLocal BOOLEAN NOT NULL DEFAULT TRUE,
    CloudBudgetCents BIGINT NOT NULL DEFAULT 0,
    CloudBudgetWindow TEXT NOT NULL DEFAULT '',
    CreatedBy TEXT NOT NULL DEFAULT '',
    UpdatedBy TEXT NOT NULL DEFAULT '',
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_runtime_policies_scope
    ON Agents_RuntimePolicies(ScopeType, ScopeID);

CREATE TABLE IF NOT EXISTS Agents_SupervisorRuns (
    ID TEXT PRIMARY KEY,
    RuntimeSessionID TEXT NOT NULL,
    MattermostConversationID TEXT NOT NULL DEFAULT '',
    RootTaskID TEXT NOT NULL DEFAULT '',
    SupervisorAgentID TEXT NOT NULL DEFAULT '',
    Status TEXT NOT NULL,
    Objective TEXT NOT NULL DEFAULT '',
    Plan JSONB NOT NULL DEFAULT '{}'::jsonb,
    FinalResultPostID TEXT NOT NULL DEFAULT '',
    CreatedBy TEXT NOT NULL DEFAULT '',
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agents_supervisor_runs_session
    ON Agents_SupervisorRuns(RuntimeSessionID);

CREATE INDEX IF NOT EXISTS idx_agents_supervisor_runs_task
    ON Agents_SupervisorRuns(RootTaskID);

CREATE TABLE IF NOT EXISTS Agents_SubagentRuns (
    ID TEXT PRIMARY KEY,
    SupervisorRunID TEXT NOT NULL,
    ParentSubagentRunID TEXT NOT NULL DEFAULT '',
    Role TEXT NOT NULL DEFAULT '',
    Title TEXT NOT NULL DEFAULT '',
    Prompt TEXT NOT NULL DEFAULT '',
    RuntimeType TEXT NOT NULL,
    ProviderID TEXT NOT NULL DEFAULT '',
    Model TEXT NOT NULL DEFAULT '',
    WorkspacePath TEXT NOT NULL DEFAULT '',
    ExternalSessionID TEXT NOT NULL DEFAULT '',
    Status TEXT NOT NULL,
    Result JSONB NOT NULL DEFAULT '{}'::jsonb,
    Summary TEXT NOT NULL DEFAULT '',
    StartedAt BIGINT NOT NULL DEFAULT 0,
    FinishedAt BIGINT NOT NULL DEFAULT 0,
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agents_subagent_runs_supervisor
    ON Agents_SubagentRuns(SupervisorRunID);

CREATE INDEX IF NOT EXISTS idx_agents_subagent_runs_parent
    ON Agents_SubagentRuns(ParentSubagentRunID)
    WHERE ParentSubagentRunID <> '';
