# Spec: snow-cli Auto Review Fixes

Created: 2026-10-03 | Branch: feat/snow-cli | Source PRD: specs/261003-snow-cli-auto-review/snow-cli-auto-review-PRD.md

## Executive Summary
An automated review of snow-cli (feat/snow-cli) found no P0 but 8 P1 and 7 P2 guardrail, integrity and hygiene gaps. This spec turns each finding into a fix requirement (FR-R01..FR-R15) delivered via three parallel workstreams plus a serial prelude and closing pass.

## Problem Statement
Documented controls read stronger than they are: Okta calls lack host/redirect restriction, write rate limits are per-process, catalog order and selftest probes bypass policy/audit, encoded-query pass-through is a blacklist, an agent can pick `--policy human`, create retries can duplicate, and update conflicts are reported after the write applied.

## Goals / Non-Goals
Goals: close all 15 findings with TDD; keep build/vet/gofmt/golangci-lint/`go test -race ./...` green; usecase coverage >= 90%; humanauth and sn >= 90%.
Non-Goals: changes to agent-cli-core (needs go to docs/core-change-requests.md), root skills/snow-cli.md, ServiceNow scoped app, native Windows, release workflows, live ServiceNow/Okta testing.

## User Requirements: Functional Requirements
Requirement IDs are kept as in the PRD (FR-R01..FR-R15); the PRD holds the full evidence text and is authoritative for acceptance criteria.

P1
- FR-R01 Okta transport: host allowlist = issuer host only; cross-host or https->http redirects fail with ForbiddenHostError (exit 4), no body sent; 307/308 test proves second server gets zero requests.
- FR-R02 Write rate limits (per_hour, per_run) enforced across processes (preferred: audit-log-derived or locked state file keyed by agent and run id; exit 6 with retry hint), or deferred with docs stating per-process only.
- FR-R03 `catalog order` item/variable reads run through guarded CatalogGet/CatalogVars (or after policy allow); denied order sends zero HTTP; reads audited.
- FR-R04 Selftest probes (resolve, update, server list) wrapped in audited guard actions with distinct verbs (e.g. selftest:probe-resolve); write probes write pending record first and abort on audit failure; deliberate policy bypass documented in ADR.
- FR-R05 Encoded-query parser: only `field OP value` clauses joined by `^` (and `^OR`); reject NQ, DYNAMIC, javascript/gs. (any case/encoding), control chars; field names in clauses and --order-by submitted to policy; ORDERBY detection by clause position; `table count --query` field-checked.
- FR-R06 `--policy` cannot select a broader policy in agent mode: refused (exit 6) unless config sets `policy.allow_override: true`; policy must match profile mode; documented.
- FR-R07 Create POST retry: re-run FindByCorrelation before every re-send (own bounded loop); hit returns `deduplicated: true`; POST-count test.
- FR-R08 Update conflict: pre-write mod-count check (ExpectedModCount) sends no PATCH on mismatch; post-write message states change was applied and names the record; audit outcome distinguishes applied-with-conflict.

P2
- FR-R09 Human login: bad-state callback ignored (400) not aborting; Host header check on loopback listener; missing nonce fails when id_token present; subject documented as unverified.
- FR-R10 Audit resource includes target reference (e.g. incident:INC0010001) while policy matches on base resource; `dry_run` outcome label (resource-suffix workaround if core cannot).
- FR-R11 Idempotency: validate explicit key charset (exit 9, before guard); normalise CI to sys_id or document; widen dedupe window / drop active=true; document concurrency limit.
- FR-R12 Untrusted marking inverted: all string fields untrusted except documented structured set; catalog variable text marked; golden injection tests.
- FR-R13 Policy requests list every fetched field (dot-walked) and check cmdb_rel_ci reads; non-rate-limit 5xx -> exit 8 accurate message; oversize body -> "response too large" (exit 1).
- FR-R14 Type safety/test gaps: replace Keychain any / Extra map[string]any with typed Env fields; AllowedFields on Guard port; policy-error adaptation behind a port; humanauth and sn >= 90%.
- FR-R15 Docs: every statement touched by R01..R14 matches code; docs/ws-*-requests.md folded or deleted; README spec links resolve; docs/assumptions.md lists new ASSUMPTIONs; user-docs have no links into specs/.

## Non-Functional Requirements
Security (R01, R03-R06, R09, R12: no new secret in logs/audit); Reliability (R07, R08, R11: no increased duplicate-write risk); Performance (at most one pre-write read for R08 and one dedupe lookup per create retry for R07); Observability (R04, R10); Quality gates as in Goals.

## System Architecture
Clean Architecture, deps inward. Affected: internal/humanauth, internal/auditx, internal/app, internal/cli, internal/config, internal/usecase/{read,write,selftest}, internal/sn, internal/idempotency, internal/domain, internal/policymap, testsupport fakes, docs, user-docs. Workstream ownership is defined in tasks.md.

## Scope of Changes
Modify only the packages above. No new external dependencies; agent-cli-core stays v0.1.0.

## Breaking Changes
Config: new optional `policy.allow_override`; `--policy` in agent mode refused by default. CLI: invalid `--idempotency-key` now exit 9; rejected encoded-query forms now exit 9/6. Audit: resource now carries a reference suffix; new dry_run outcome (or suffix). Possible rate-limit state file/log (R02).

## Success and Acceptance Criteria
Each FR's acceptance list in the PRD is met with a red-then-green test; one reviewer pass per fix branch (security read for R01, R03, R04, R05, R06); gates green.

## Risks and Mitigation
- Shared files conflict across branches: exclusive ownership + serial handling (tasks.md).
- Core lacks dry_run outcome / cross-process counters: use workarounds, log in docs/core-change-requests.md.
- Stricter query parser rejects previously accepted queries: documented, tests per form.

## Timeline and Milestones
M0 serial prelude; M1 P1 fixes in parallel; M2 P2 fixes in parallel; M3 serial merge, docs/R15, final review.

## Open Questions (carried from PRD; decide before the owning stream starts)
1. FR-R02 enforcement approach (a) vs defer (b). 2. FR-R06 allow_override key vs removing --policy in agent mode. 3. FR-R10 core dry_run label vs suffix.

## References
specs/261003-snow-cli-auto-review/snow-cli-auto-review-PRD.md; specs/archive/261003-snow-cli/
