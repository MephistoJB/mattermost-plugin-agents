DROP INDEX IF EXISTS idx_agents_subagent_runs_parent;
DROP INDEX IF EXISTS idx_agents_subagent_runs_supervisor;
DROP TABLE IF EXISTS Agents_SubagentRuns;

DROP INDEX IF EXISTS idx_agents_supervisor_runs_task;
DROP INDEX IF EXISTS idx_agents_supervisor_runs_session;
DROP TABLE IF EXISTS Agents_SupervisorRuns;

DROP INDEX IF EXISTS idx_agents_runtime_policies_scope;
DROP TABLE IF EXISTS Agents_RuntimePolicies;

DROP INDEX IF EXISTS idx_agents_runtime_sessions_thread;
DROP INDEX IF EXISTS idx_agents_runtime_sessions_user_updated;
DROP INDEX IF EXISTS idx_agents_runtime_sessions_conversation_agent;
DROP TABLE IF EXISTS Agents_RuntimeSessions;
