# WS-G requests and doc hand-offs (for WS-H T-H8 to fold into the ADR / docs, then delete)

## user-docs/configuration.md (WS-H owns)
- FR-R06: new profile key `policy.allow_override` (default false). In agent mode `--policy` is refused with exit 6 and no HTTP unless it is true. In human mode a named policy (`agent`/`human`) must match the mode unless it is true. Passing the same value as `policy.path` is not an override.
- FR-R02: `rate_limit.per_hour` and `per_run` are now enforced across invocations from a locked state file `ratelimit.json` next to the audit log (flock, 0600). `per_hour` is a sliding hour window keyed by agent id and rule; `per_run` is keyed by agent id, `SNOW_RUN_ID` and rule. Without `SNOW_RUN_ID` every invocation gets a new run id, so `per_run` then bounds one process only: set `SNOW_RUN_ID` per agent run. A denial is exit 6 with a "retry in ..." reason (hourly). An unreadable or corrupt state file refuses the limited action (exit 1, names the path). Replace the sentence "per_hour is counted by the policy engine ... per process". The shared-write-counter limitation (CR-01) remains.
- FR-R10: audit `resource` may carry the target as `<base>:<ref>` (for example `incident:INC0010001`); outcomes `dry_run` and `applied_conflict` exist; selftest probes appear as verbs `selftest:probe-list|probe-resolve|probe-update` with policy_decision `probe_bypass`.

## ADR / technical-details (WS-H)
- FR-R04: selftest server-ACL and write probes deliberately skip the client policy (they ask the server ACL) but are audited in Block mode through `auditx.Guard.RunProbe` (`selftest.ProbeGuard`): pending record first, no request when it cannot be written, remaining probes abort after an audit failure. Distinct verbs, `policy_decision: probe_bypass`. Document the deliberate bypass.
- FR-R02 decision S0.1 option (a): state file design above; `auditx.StateLimiter` (flock; unix only, fails closed elsewhere). Key points: only policy-allowed requests consume budget (as in core); a refusal consumes none; run counters idle for 7 days are pruned.
- FR-R06: policy pinned like mode (D-i extended).
- FR-R14: `cli.Env` no longer has `Keychain any` / `Extra map`; typed `Keychain humanauth.Store`, `HumanAuth *HumanAuthDeps`, `TaskFetcher write.TaskFetcher`, `Selftest *selftest.Service`, `PolicyErrors usecase.PolicyErrorAdapter` (cli no longer imports internal/sn outside tests). If `PolicyErrors` is nil a denial is still refused but exits 1.

## core-change-requests.md (WS-H appends)
- CR: first-class audit fields for the target reference and for outcome `dry_run`/`applied_conflict` (today: `<base>:<ref>` resource suffix and outcome label strings, which the core record accepts as free text).
- CR: cross-process rate-limit state in the core policy engine (or an injectable counter store), replacing `auditx.StateLimiter`.

## Requests for WS-H code
- H5 must set `usecase.Action.Ref` (number or sys_id) on write/read actions and call `usecase.SetOutcome(ctx, usecase.OutcomeDryRun)` for `--dry-run` previews and `OutcomeAppliedConflict` for the FR-R08 post-write conflict. The guard already: logs `base:ref`, uses the sink outcome, and labels a policy dry_run_only preview `dry_run` when no outcome was set.
- `sn.DeniedError.Hint()` could include the retry wait when `Err.Decision.RetryAfter > 0` (today the wait is in the message text only).
- docs/assumptions.md: no new ASSUMPTION from WS-G.

## Root skill (docs/root-skill-update-needed.md)
- Limits now hold across invocations when `SNOW_RUN_ID` is set; agents should export a stable `SNOW_RUN_ID` per run. `--policy` is refused on agent profiles.
