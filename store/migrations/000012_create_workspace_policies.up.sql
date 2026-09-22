CREATE TABLE IF NOT EXISTS Agents_WorkspacePolicies (
    ID TEXT PRIMARY KEY,
    Name TEXT NOT NULL,
    AllowedRoots JSONB NOT NULL DEFAULT '[]'::jsonb,
    DefaultWorkspacePath TEXT NOT NULL DEFAULT '',
    Mode TEXT NOT NULL DEFAULT 'ask_write',
    NetworkMode TEXT NOT NULL DEFAULT 'ask',
    ShellMode TEXT NOT NULL DEFAULT 'ask',
    CreatedAt BIGINT NOT NULL,
    UpdatedAt BIGINT NOT NULL,
    Metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_workspace_policies_name
    ON Agents_WorkspacePolicies(Name);
