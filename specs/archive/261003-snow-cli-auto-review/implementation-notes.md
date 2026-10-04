# Implementation Notes: snow-cli Auto Review Fixes
Date: 2026-10-03

Purpose: record decisions, edge cases and deviations as work proceeds. Update after each task.

## Technical Decisions
### S0.1 Open question decisions (2026-10-03)
1. FR-R02: option (a). Enforce cross-process write limits from a locked state file (flock, keyed by agent id and run id, hour window for per_hour) owned by WS-G; the audit log is not the source of truth. Denial is exit 6 with a retry hint. If WS-G cannot meet PRD R02 it falls back to option (b) docs-only and records it in docs/ws-g-requests.md.
2. FR-R06: explicit `policy.allow_override` config key (default false). In agent mode `--policy` is refused (exit 6, no HTTP) unless the key is true, and the named policy must match the profile mode by default. Documented in user-docs/configuration.md.
3. FR-R10: audit resource carries a `:<ref>` suffix (for example incident:INC0010001) while policy matches on the base resource; previews use outcome label `dry_run`. Core is not changed: a core change request for a first-class label goes to docs/core-change-requests.md and the suffix/label convention stays until core adds it.

### S0.2 Frozen contract (internal/usecase/ports.go)
- `Guard.AllowedFields(verb, resource string) []string` is part of the Guard port (read.FieldAllowlister removed).
- `Action.Ref string`: audit-only target reference; guard logs `usecase.ResourceRef(Request.Resource, Ref)`; policy matches Request.Resource only.
- `usecase.Outcome` with `OutcomeDryRun` ("dry_run") and `OutcomeAppliedConflict` ("applied_conflict"); a use case calls `usecase.SetOutcome(ctx, o)` inside the ActionFunc; the guard creates the context with `usecase.WithOutcomeSink` and reads `sink.Outcome()` after fn (WS-G wires this in auditx; not yet consumed).
- `usecase.PolicyErrorAdapter` (+ `PolicyErrorFunc`): policy-error adaptation port; CLI should depend on it instead of internal/sn (WS-G wires).
- `usecase.GuardedCatalog` (CatalogGet, CatalogVars) and `usecase.CatalogVars`; read.Service satisfies it (`read.VarsData` is an alias). `write.OrderService.Catalog` is a `usecase.GuardedCatalog`; it resolves item and variables with one guarded CatalogVars call. cli passes `readService(c)`.
## Edge Cases & Solutions
## Deviations from Plan
## Lessons Learned
