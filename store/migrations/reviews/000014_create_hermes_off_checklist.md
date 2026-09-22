# Migration 000014: Hermes-Off Checklist

Creates `Agents_HermesOffChecklist` for persistent manual migration gate status.

- `Key` is the stable checklist item identifier and primary key.
- `Status` stores `manual` or `ok`.
- `Detail`, `UpdatedBy`, and `UpdatedAt` keep an auditable admin note and actor metadata.

The table contains no secrets or prompt content.
