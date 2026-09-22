DROP INDEX IF EXISTS idx_agents_taskruns_session;
DROP INDEX IF EXISTS idx_agents_taskruns_task;
DROP TABLE IF EXISTS Agents_TaskRuns;

DROP INDEX IF EXISTS idx_agents_tasks_thread;
DROP INDEX IF EXISTS idx_agents_tasks_user_updated;
DROP INDEX IF EXISTS idx_agents_tasks_status_next_run;
DROP TABLE IF EXISTS Agents_Tasks;
