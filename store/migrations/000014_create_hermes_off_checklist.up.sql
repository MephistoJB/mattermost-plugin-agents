CREATE TABLE IF NOT EXISTS Agents_HermesOffChecklist (
    Key TEXT PRIMARY KEY,
    Status TEXT NOT NULL DEFAULT 'manual',
    Detail TEXT NOT NULL DEFAULT '',
    UpdatedBy TEXT NOT NULL DEFAULT '',
    UpdatedAt BIGINT NOT NULL DEFAULT 0
);
