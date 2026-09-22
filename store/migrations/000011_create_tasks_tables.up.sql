CREATE TABLE IF NOT EXISTS Agents_Tasks (
    ID TEXT PRIMARY KEY,
    Title TEXT NOT NULL DEFAULT '',
    Prompt TEXT NOT NULL,
    TaskType TEXT NOT NULL,
    Status TEXT NOT NULL,
    ChannelID TEXT NOT NULL DEFAULT '',
    RootPostID TEXT NOT NULL DEFAULT '',
    UserID TEXT NOT NULL,
    AgentID TEXT NOT NULL,
    RuntimePolicySnapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    WorkspacePath TEXT NOT NULL DEFAULT '',
    ScheduleSpec TEXT NOT NULL DEFAULT '',
    NextRunAt BIGINT NOT NULL DEFAULT 0,
    LastRunAt BIGINT NOT NULL DEFAULT 0,
    CreatedBy TEXT NOT NULL DEFAULT '',
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agents_tasks_status_next_run
    ON Agents_Tasks(Status, NextRunAt);

CREATE INDEX IF NOT EXISTS idx_agents_tasks_user_updated
    ON Agents_Tasks(UserID, UpdatedAt DESC);

CREATE INDEX IF NOT EXISTS idx_agents_tasks_thread
    ON Agents_Tasks(ChannelID, RootPostID);

CREATE TABLE IF NOT EXISTS Agents_TaskRuns (
    ID TEXT PRIMARY KEY,
    TaskID TEXT NOT NULL,
    RuntimeSessionID TEXT NOT NULL DEFAULT '',
    Status TEXT NOT NULL,
    StartedAt BIGINT NOT NULL DEFAULT 0,
    FinishedAt BIGINT NOT NULL DEFAULT 0,
    ResultPostID TEXT NOT NULL DEFAULT '',
    Error TEXT NOT NULL DEFAULT '',
    Usage JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agents_taskruns_task
    ON Agents_TaskRuns(TaskID, StartedAt DESC);

CREATE INDEX IF NOT EXISTS idx_agents_taskruns_session
    ON Agents_TaskRuns(RuntimeSessionID)
    WHERE RuntimeSessionID <> '';
