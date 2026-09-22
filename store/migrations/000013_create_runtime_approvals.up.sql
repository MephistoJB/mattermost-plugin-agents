CREATE TABLE IF NOT EXISTS Agents_RuntimeApprovals (
    ID TEXT PRIMARY KEY,
    RuntimeSessionID TEXT NOT NULL,
    ExternalApprovalID TEXT NOT NULL DEFAULT '',
    SubagentRunID TEXT NOT NULL DEFAULT '',
    Status TEXT NOT NULL DEFAULT 'pending',
    RequestPayload JSONB NOT NULL DEFAULT '{}'::jsonb,
    Decision TEXT NOT NULL DEFAULT '',
    DecisionScope TEXT NOT NULL DEFAULT '',
    DecisionReason TEXT NOT NULL DEFAULT '',
    RequestedBy TEXT NOT NULL DEFAULT '',
    DecidedBy TEXT NOT NULL DEFAULT '',
    ExpiresAt BIGINT NOT NULL DEFAULT 0,
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_agents_runtimeapprovals_session
    ON Agents_RuntimeApprovals(RuntimeSessionID, CreatedAt DESC);

CREATE INDEX IF NOT EXISTS idx_agents_runtimeapprovals_pending
    ON Agents_RuntimeApprovals(Status, ExpiresAt, CreatedAt DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_runtimeapprovals_external
    ON Agents_RuntimeApprovals(RuntimeSessionID, ExternalApprovalID)
    WHERE ExternalApprovalID <> '';
