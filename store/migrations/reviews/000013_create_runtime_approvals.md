# Migration 000013: Runtime approvals

Adds `Agents_RuntimeApprovals` for the Hermes replacement approval broker.

- Approvals are linked to runtime sessions and optionally subagent runs.
- `ExternalApprovalID` stores provider/runtime-specific IDs when available.
- `RequestPayload` preserves the sanitized runtime request payload for later UI rendering and audit.
- A partial unique index prevents duplicate provider approval rows for the same session.
- Pending lookup index supports `/approvals`, runtime status, and later UI polling.
