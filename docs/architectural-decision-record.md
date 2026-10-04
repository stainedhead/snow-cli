# Architectural decision record

One entry per decision: context, decision, consequences. Entries are drafted
from the approved spec (`D-*` references are spec decision numbers) and are
revised at the integration review. Status values: Accepted (decided in the spec),
Proposed (drafted here, needs review), Open (undecided, tracked elsewhere).

## ADR-001 Depend on agent-cli-core at a released tag

- Status: Accepted
- Context: auth, policy, output envelope, audit, httpx, selftest and docgen are shared by the CLI set and live in their own repository, `agent-cli-core`.
- Decision: `go.mod` requires `github.com/stainedhead/agent-cli-core v0.1.0`. No `replace` directive and no pseudo-version on `main`; a test greps `go.mod`. Core changes are requested, never copied (see `docs/core-change-requests.md`).
- Consequences: gaps in the core are worked around in `snow` and recorded as change requests; a core bump is an ordinary PR (DEP-6).

## ADR-002 Agent-mode daemon client is a fail-closed stub

- Status: Superseded by ADR-017
- Context: agent mode takes its Okta token from the `agent-okta-d` daemon through the core's `auth.DaemonClient` / `auth.TokenSource` interfaces. The core defines the interfaces, but the real adapter (and the `agent-okta-d` `pkg/client` release it needs) does not exist yet.
- Decision: the composition root has a `newDaemonClient()` that returns a client reporting the daemon unavailable. It surfaces the core's unreachable error (exit 3, category `auth`) and the message names the configured `daemon.socket` path. `agent-okta-d` is NOT added to `go.mod`. `app.Options.DaemonClient` is the test seam; tests use the core's `authtest.Fake`. Nothing in `snow` fabricates, caches or reads a token on its own, so there is no "pretend it works" path.
- Consequences: in this release an agent-mode command that reaches ServiceNow exits 3 against a real instance until the adapter lands. Human mode and all fake-server tests are unaffected. Replacing the stub is a one-function change in the composition root plus the `go.mod` require; it is listed in `docs/deferred.md`. The agent skill must say the agent mode is not usable end to end until then (`docs/root-skill-update-needed.md`).

## ADR-003 Shipped policies use the core's strict schema, not the PRD sketch

- Status: Accepted (D-f), implemented as `policies/agent.policy.yaml` and `policies/human.policy.yaml`
- Context: PRD section 9 shows a policy sketch that is not loadable by the core (`policy.Parse` rejects unknown keys and duplicate keys).
- Decision: both files use the core schema (`version: 1`, `limits`, ordered `rules` with `id`, `effect`, `verbs`, `resources`, `mode`, `fields`, `constraints`, `rate_limit`). A verb/resource vocabulary (`internal/policymap`) is the stable contract between use cases, policy files and the generated skill. Files are embedded (`policies.Named`) so `--policy agent|human` works without a file on disk, and every file is parsed in unit tests.
- Consequences: constructs the core cannot express are handled outside policy (`incident.create.require` is validated by `snow` before the policy check, exit 9) or approximated (`max_writes_per_run` becomes `rate_limit.per_run` on each write rule because the core has no shared write counter; see core change requests). The mapping table lives in the spec (D-f).

## ADR-004 Impact and urgency use the ServiceNow scale; the PRD inversion is corrected

- Status: Accepted (D-d)
- Context: ServiceNow scale is 1 = High, 2 = Medium, 3 = Low. The PRD sketch `max_impact_urgency: 2  # nothing above High` maps to a core `Max: 2`, which allows 1 (the highest) and forbids 3, the reverse of the intent.
- Decision: the agent policy constrains `impact` and `urgency` with `min: 2, max: 3`; the human policy allows 1..3 so a human escalates. `priority` is never written (and not in any create/update allowlist). A test named for assumption A-01 asserts 1 is denied and 2 and 3 are allowed for both fields on create and update.
- Consequences: instances with a different scale need a different policy file; the config key `incident.scale` records the instance's values. The P1 block also belongs server-side in the record producer, which is out of scope here.

## ADR-005 Client policy is a guardrail; ServiceNow roles and ACLs are the boundary

- Status: Accepted
- Context: loosening ServiceNow roles because the CLI has guardrails would invert the security model (PRD section 13.1).
- Decision: the policy engine narrows what the CLI attempts and explains denials early (exit 6). A server denial is exit 4 and final. Deny rules (`sys_*` tables, `sysapproval_approver`, agent `resolve`) win over any allow. No command prints a token, there is no raw REST passthrough, and the audit log is block-on-failure for writes.
- Consequences: `snow selftest` is the acceptance test the ServiceNow platform team runs; it needs a live instance and never runs in PR CI.

## ADR-006 The agent skill is generated from the command tree; the root copy is updated by hand

- Status: Accepted (FR-051, SKILL-3)
- Decision: `snow skill generate` renders the skill through the core's `docgen` from the router's command tree (commands, usage, examples and forbidden actions per command). `make skill` writes `dist/snow-cli.md` (git-ignored). A golden file (`internal/cli/testdata/skill.golden.md`) pins the output; the unit test and a CI step fail on drift. The root `skills/snow-cli.md` in the `agentic-teams` repository is not edited from here; needed changes are recorded in `docs/root-skill-update-needed.md` and applied by a manual pull request at release time (PRD 16.1).
- Consequences: adding or changing a command changes the golden; the golden is regenerated with `go test ./internal/cli -run TestSkillGolden -update` and the diff reviewed. Hand-written guidance that `docgen` cannot derive lives only in the root copy.

## ADR-007 Output extras live in `data`; list pagination is snow-trimmed

- Status: Accepted (D-e, D-h)
- Context: core `output.Meta` is fixed; core bounds arrays, strings, but not an `items` array nested in an object (verified in the core `output` package).
- Decision: `acl_filtered_possible`, `deduplicated`, `page` detail and `dry_run` go in `data`. Lists return `{items, page, acl_filtered_possible}` and `snow` trims `items` to `--max-bytes` itself. `data.page.next_offset` is absolute; core `meta.next_offset` (when set) is relative to the page returned.
- Consequences: documented in the skill and user docs; a core request asks for an offset base option.

## ADR-008 Human token type sent to ServiceNow defaults to the access token

- Status: Open (decided by M0 spike, A-05)
- Decision for now: config `okta.token_type: access|id`, default `access`. Changing the default after the spike is a one-line change.

## ADR-009 Stdlib `flag` with a small command router

- Status: Accepted (revisitable)
- Context: the command set is a fixed tree of noun-verb commands; the skill generator needs usage, examples and forbidden actions per command.
- Decision: `internal/cli` has its own router over the standard library `flag` package. Each command is registered with path, usage, examples, forbidden actions and a flag function; the same registry feeds `help` and `skill generate`. No third-party CLI framework.
- Consequences: no new dependency and no drift between help, skill and code. Global flags go after the command. Shell completion and nested flag inheritance are not provided.

## ADR-010 Write path: pending audit record, dedupe before retry, order never retried

- Status: Accepted (D-c)
- Context: a retried POST can create duplicates, and an audit gap on a write is worse than a refused write.
- Decision: writes run under the audit logger in block mode: a pending record is written before the request and a failure aborts with no request sent (exit 1). The outcome record follows; if it fails the error says the write may have happened. Incident create POSTs are marked safe to retry only after the `correlation_id` dedupe query found nothing. Catalog order POSTs are never marked safe because no retrievable dedupe key is confirmed (A-07). Updates use the `sys_mod_count` guard (A-09).
- Consequences: repeated creates within the hour return the existing record; a transient order failure exits 8 with a hint to check `snow request list`. Correctness depends on the unverified `correlation_id` and `sys_mod_count` assumptions.

## ADR-011 Human credential store behind an interface; real backends not built

- Status: Accepted (revisit when backends land)
- Context: tokens must not sit in plaintext by default, and keychain behaviour on macOS, Linux and WSL2 is unverified (A-13).
- Decision: human tokens go through a `humanauth.Store` interface. Memory and failing fakes serve tests. The macOS, Linux Secret Service and WSL2 stores are stubs that fail closed (exit 3) naming the missing backend. `--insecure-store` or `SNOW_INSECURE_STORE=1` selects a 0600 JSON file at `~/.config/snow/credentials.json`. Credential values print as redacted in every format.
- Consequences: human mode is usable only with the explicit insecure opt-in until real backends exist (`deferred.md`).

## ADR-012 Okta traffic is restricted to the issuer host; login is hardened (FR-R01, FR-R09)

- Status: Accepted
- Context: login, refresh, device flow and revoke send `code`, `code_verifier` and refresh tokens; a redirect must not carry them elsewhere.
- Decision: the Okta client allows requests only to the issuer host (host:port, same scheme). A redirect to another host or an https to http downgrade fails with `*httpx.ForbiddenHostError` (exit 4) and the body is never re-sent, including for an injected `Config.HTTP` client (its `CheckRedirect` is replaced). A refresh that hits a refused redirect returns that error (exit 4), not a login-required error. PKCE callbacks with a wrong or missing `state`, or a Host header other than the listener address, are answered 400 and ignored (login keeps waiting until the timeout); a returned id_token must carry the matching nonce. The id_token signature is not verified (it arrives directly from the token endpoint over TLS); the displayed subject is an unverified claim and never used for authorization.
- Consequences: a state mismatch no longer aborts login; a hostile local request cannot cancel it.

## ADR-013 Policy selection is pinned like mode; write limits persist across invocations (FR-R06, FR-R02)

- Status: Accepted (decisions S0.1 of the review spec)
- Decision, policy: `--policy` is refused in agent mode (exit 6, no HTTP) unless the profile sets `policy.allow_override: true`; in human mode a named policy (`agent`/`human`) must match the mode unless the override is set. Passing the same value as `policy.path` is not an override.
- Decision, limits: `rate_limit.per_hour` and `per_run` are enforced across invocations from a flock-protected state file `ratelimit.json` (0600) next to the audit log (`auditx.StateLimiter`; unix only, fails closed elsewhere). `per_hour` is a sliding hour window keyed by agent id and rule; `per_run` is keyed by agent id, `SNOW_RUN_ID` and rule. Only policy-allowed requests consume budget; a refusal consumes none; run counters idle for 7 days are pruned. An unreadable or corrupt state file refuses the limited action (exit 1, names the path). A hourly denial is exit 6 with a "retry in ..." reason.
- Consequences: without `SNOW_RUN_ID` every invocation is its own run, so `per_run` then bounds one process only; agents must export a stable `SNOW_RUN_ID`. No counter is shared across rules (CR-01).

## ADR-014 Selftest probes and catalog order go through audited guard actions (FR-R03, FR-R04)

- Status: Accepted
- Decision, order: `catalog order` resolves the item and validates variables through the guarded read service (`CatalogGet`/`CatalogVars`), so a policy that denies `vars` makes the order exit 6 with no HTTP, and the reads appear in the audit log.
- Decision, probes: selftest server-ACL and write probes deliberately skip the client policy (they ask the server ACL what it would allow) but are audited in block mode through `auditx.Guard.RunProbe` (`selftest.ProbeGuard`): pending record first, no request when it cannot be written, remaining probes abort after an audit failure. Verbs are `selftest:probe-list|probe-resolve|probe-update` with `policy_decision: probe_bypass`.
- Consequences: the policy bypass is intentional and visible in the audit trail.

## ADR-015 Audit identifies the target record and distinguishes previews and applied conflicts (FR-R10, FR-R08)

- Status: Accepted (core workaround, see CR-10)
- Decision: the audit `resource` carries the target as `<base>:<ref>` (for example `incident:INC0010001`; create uses the idempotency key, because the number is unknown before the POST) while policy matching stays on the base resource. Use cases set `Action.Ref` and call `usecase.SetOutcome` with `dry_run` (for `--dry-run` and `dry_run_only` previews) or `applied_conflict` (write applied, then `sys_mod_count` advanced by more than one); `auditx.Guard` reads the outcome sink and writes that label as the record outcome. The `--expected-mod-count N` flag on `incident update|resolve` and `task update` makes the pre-write check a hard precondition: a mismatch sends no PATCH (exit 7, "not applied"). Task update otherwise uses the sys_mod_count it fetched for the assignment check.
- Consequences: end-to-end tests assert the audit lines (`TestE2EAppliedConflictAuditOutcomeAndRefSuffix`, `TestE2EDryRunAuditOutcomeAndRefSuffix`, `TestE2EExpectedModCountFlagMismatchSendsNoPatch`).

## ADR-016 Typed composition seams (FR-R14)

- Status: Accepted
- Decision: `cli.Env` has typed fields (`Keychain humanauth.Store`, `HumanAuth *HumanAuthDeps`, `TaskFetcher write.TaskFetcher`, `Selftest *selftest.Service`, `PolicyErrors usecase.PolicyErrorAdapter`) instead of `Keychain any` and an `Extra` map; `cli` no longer imports `internal/sn` outside tests. If `PolicyErrors` is nil a denial is still refused but exits 1. `Guard.AllowedFields(verb, resource)` returns the allowlist of the first matching allow rule (a matching deny returns none); reads without `--fields` request exactly that allowlist. `app/human.go` keeps its own `SNOW_INSECURE_STORE` and home lookup behind a `storeFactory` seam.

## ADR-017 Agent-mode daemon client is the core's oktad adapter

- Status: Accepted (supersedes ADR-002; the core bump also supersedes the `v0.1.0` pin in ADR-001)
- Context: `agent-cli-core v0.2.1` ships `auth/oktad`, an `auth.DaemonClient` over the `agent-okta-d` Go client.
- Decision: `go.mod` requires `agent-cli-core v0.2.1` (and, through it, `agent-okta-d v0.1.0`, which tests also import for the `clienttest` fake daemon). `newDaemonClient()` returns `oktad.New` with a 5 s request timeout. A profile `daemon.socket`, when set, is passed as the socket; when unset `snow` no longer fills in a default and the adapter uses `AGENT_OKTA_D_SOCKET`, then the platform default. The provider (`daemon.provider`, default `snow`) and the `auth.DaemonTokenSource` with its re-enrollment remediation text are unchanged. Exit codes follow the adapter: unreachable, reauth_required, revoked and access errors exit 3; degraded or retry-hinted answers exit 8. The `auth.DaemonTokenSource` result is used directly: core v0.2.1 passes categorized errors (`TransientError`, `AccessError`) through, so exit 8 and the adapter hints survive (CR-13, resolved). A cancelled caller context stays a general error (exit 1). The `internal/agentauth` stub is deleted.
- Consequences: agent mode can obtain a token from a running daemon; it is verified against the fake daemon only. Other v0.2 features are not adopted (see `docs/deferred.md`).
