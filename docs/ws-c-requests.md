# WS-C (write path) notes and requests

## Assumptions introduced (for the assumptions register, E8/E6)
Each has a code comment `ASSUMPTION(unverified against a real instance)` and a test whose name contains `Assumption`.
- A-01: priority is derived by instance rules; never written (create payload, update refuses `priority`); impact/urgency 1 denied by the agent constraint (test `TestAssumptionA01ImpactUrgencyConstraint`).
- A-04: `correlation_id` / `correlation_display` exist on incident (and task) in the target release (`TestAssumptionA04CorrelationDisplay`).
- A-07: no dedupe key retrievable after `order_now`; order POST is never marked safe to retry (`TestAssumptionA07OrderPostNotRetried`).
- A-08: record producer and `order_now` response shapes (`TestAssumptionA08CreateViaProducer`, `TestAssumptionA08OrderResponse`); producer stores the `correlation_id` variable.
- A-09: `sys_mod_count` exists and increments by one per update; when absent the guard degrades to no detection (`TestAssumptionA09*`).
- Task: dot-walked `assigned_to.user_name` is returned by the Table API; OOB sc_task states 2 and 3 are the permitted agent states (`TestAssumptionTaskStateSet`).

## Decisions
- PATCH is never marked safe to retry (a re-sent applied PATCH would duplicate work_notes). D-j says "PATCH retried only with the guard"; the stricter reading was taken. A 503 on PATCH exits 8.
- Policy requests carry only caller-chosen fields; system-added `correlation_id`, `correlation_display` and the provenance work note are not subject to the caller's field allowlist. Policy files (WS-E) therefore need `fields` allowing only: short_description, description, cmdb_ci, impact, urgency, assignment_group, work_notes (create); any updatable fields (update); close_code, close_notes (resolve); work_notes, comments, state, assigned_to (task update).
- `--ci` and `--app` both map to `cmdb_ci`. Dry-run of `incident create` sends zero requests (no dedupe lookup); catalog order dry-run still performs read-only catalog lookups.
- Confirmation (FR-047) runs inside the guarded action, after policy allow and the pending audit record; a declined prompt is recorded as an error outcome and exits 1. Non-interactive human mode without `--yes` exits 2. Agent mode never prompts.
- Unassigned tasks may be claimed (assigned_to = caller); any other task not assigned to the caller is refused (exit 6).

## Requests to the integration owner
- `internal/cli/cmd_write.go` uses `Env.Extra["write.task_fetcher"]` (set by `wire_write.go`) for the task assignee check; `Env.Catalog` (WS-B `wireRead`) is required by `catalog order`.
- Root skill / docs: new commands `incident create|update|resolve`, `task update`, `catalog order` with global flags `--dry-run --idempotency-key --yes`.

## Integration disposition
- `Env.Extra["write.task_fetcher"]` and `Env.Catalog` are wired by `wire_write.go` / `wire_read.go`; no change needed.
- The shipped policies (`policies/*.policy.yaml`) carry the field allowlists listed under Decisions; `TestIntegrationM2*` run create, update and order through them.
- Root skill / docs request recorded in docs/root-skill-update-needed.md; the skill golden was regenerated at integration.
