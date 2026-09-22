# 000011 Create Tasks Tables

Adds durable storage for autonomous tasks and reminders.

- `Agents_Tasks` stores one-shot tasks, reminders, recurring jobs, and watchers.
- `Agents_TaskRuns` records individual executions and links them to runtime
  sessions when a run is started.

The migration is additive and does not change existing runtime-session or
conversation rows.
