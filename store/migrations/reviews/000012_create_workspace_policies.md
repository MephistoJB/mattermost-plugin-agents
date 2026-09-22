# Migration 000012: Workspace policies

Adds `Agents_WorkspacePolicies` for the Hermes replacement runtime control plane.

- `AllowedRoots` is JSONB so callers can store multiple permitted workspace roots without schema churn.
- `Mode`, `NetworkMode`, and `ShellMode` are text enums enforced in application code to keep future values migratable.
- `Name` is unique because policies are selected and displayed by a stable operator-facing label.
