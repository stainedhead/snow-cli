# Status: snow-cli Auto Review Fixes
Created: 2026-10-03

| Phase | Status |
|---|---|
| Phase 0: Spec and serial prelude | Complete |
| Phase 1: P1 fixes (3 parallel streams) | Complete (merged) |
| Phase 2: P2 fixes (3 parallel streams) | Complete (merged) |
| Phase 3: Serial merge, docs, final review | Complete (integration pass; independent reviewer pass M1 not run) |

## Phase 0 checklist
- [x] Spec created from PRD
- [x] Research questions identified (research.md)
- [x] Phase files initialized
- [x] Open questions 1-3 decided (R02 state file, R06 allow_override, R10 suffix + dry_run label)
- [x] Serial prelude S0 merged

## Blockers
(none)

## Recent Activity
- 2026-10-03: spec directory created.

## FR acceptance (2026-10-03)

| FR | Status | Notes |
|---|---|---|
| FR-R01 | Met | Okta host/redirect restriction |
| FR-R02 | Met | cross-invocation write limits (unix only; no cross-rule counter, CR-01/CR-11) |
| FR-R03 | Met | catalog order via guarded read service |
| FR-R04 | Met | audited selftest probes (deliberate policy bypass in ADR-014) |
| FR-R05 | Met | encoded-query parser and policy-checked fields |
| FR-R06 | Met | --policy pin + policy.allow_override |
| FR-R07 | Met | create re-dedupe before each re-send (not atomic across concurrent runs) |
| FR-R08 | Met | ExpectedModCount precondition, --expected-mod-count flag, applied_conflict audit outcome |
| FR-R09 | Met | login hardening (id_token signature unverified, documented) |
| FR-R10 | Met | audit <base>:<ref> suffix + dry_run/applied_conflict outcomes (core first-class fields deferred, CR-10) |
| FR-R11 | Met | key validation, previous-bucket dedupe (concurrency limit documented) |
| FR-R12 | Met | inverted untrusted marking |
| FR-R13 | Met | dot-walked fields in policy, 5xx mapping, oversize body |
| FR-R14 | Met | typed Env/Store/AllowedFields/PolicyErrors; humanauth 92.6%, sn 94.8% |
| FR-R15 | Met | docs pass; ws-*-requests.md folded and deleted |

No FR is deferred outright. Limits that remain, all listed in docs/deferred.md: R02 is unix-only and has no shared write counter; R07/R11 create is not atomic across simultaneous runs; R09 id_token signature unverified; R10 audit target/outcome are a resource suffix and label strings, not first-class core fields. Assumptions stay unverified against a real instance. Reviewer pass M1 (independent security read) was not performed in this pass.

## Recent Activity
- 2026-10-03: Phase 3 integration: --expected-mod-count wired; e2e audit tests (applied_conflict, dry_run, ref suffix, mod-count mismatch); docs folded, ws-*-requests.md deleted, skill golden regenerated.
