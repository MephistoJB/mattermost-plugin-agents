# 000010 Create Runtime Control Tables

Adds the persistence layer required for replacing Hermes orchestration inside
the Mattermost agents plugin.

- `Agents_RuntimeSessions` tracks active and resumable runtime sessions.
- `Agents_RuntimePolicies` stores per-server/team/channel/thread/user/agent
  runtime selection and local/cloud allow flags.
- `Agents_SupervisorRuns` and `Agents_SubagentRuns` reserve durable state for
  supervisor-mode task decomposition and subagent execution.

The migration is additive and does not change existing conversation or agent
rows.
