# Tasks: snow-cli Auto Review Fixes
Date: 2026-10-03 | Status: Planning | Progress: 0/20 tasks complete

## Ownership model
Three parallel workstreams, each in its own worktree/branch from feat/snow-cli (after S0), with EXCLUSIVE file ownership by package. A stream edits only paths it owns. Anything needed in another stream's or a shared file goes in docs/ws-<x>-requests.md (owned by that stream; folded/deleted by WS-H in T-H8) and the stream keeps going with a workaround. Within each stream P1 tasks run before P2.

| Stream | Branch | Owns (exclusive) | FRs |
|---|---|---|---|
| WS-F human auth / Okta | feat/snow-cli-ws-f | internal/humanauth/**, internal/testsupport/oktafake/**, docs/ws-f-requests.md | R01, R09, R14 (humanauth part) |
| WS-G guard / policy / audit / selftest / CLI / app | feat/snow-cli-ws-g | internal/auditx/**, internal/app/**, internal/cli/**, internal/config/**, internal/policymap/**, internal/agentauth/**, internal/provenance/**, internal/usecase/selftest/**, internal/repocheck/**, cmd/snow/**, policies/**, docs/ws-g-requests.md | R02, R04, R06, R10 (audit side), R03 (CLI wiring), R14 (Env/typing) |
| WS-H sn adapters / write / idempotency / read / docs | feat/snow-cli-ws-h | internal/sn/**, internal/idempotency/**, internal/usecase/write/**, internal/usecase/read/**, internal/usecase/whoami/**, internal/domain/**, internal/testsupport/snfake/**, docs/** (except files listed serial), user-docs/**, README.md | R03 (use case), R05, R07, R08, R10 (resource refs), R11, R12, R13, R14 (sn coverage), R15 |

## Serial handling (shared files; not edited by streams in parallel)
- internal/usecase/ports.go and ports_test.go: Guard.AllowedFields, policy-error adaptation port, audit outcome/resource-ref types. Done once in S0 by the orchestrator before streams fork; later changes via requests, applied serially at merge.
- go.mod, go.sum, .golangci.yml, Makefile: frozen (no changes expected; orchestrator only).
- docs/assumptions.md, docs/core-change-requests.md, docs/deferred.md, the ADR: streams F and G append requests to their docs/ws-<x>-requests.md; WS-H applies them in T-H8; assumption test (internal/repocheck, WS-G) runs after.
- specs/261003-snow-cli-auto-review/{status,tasks}.md: orchestrator only.
- Cross-stream seams: R03 (WS-G wires cli/cmd_write.go to the guarded read service; WS-H changes usecase/write/order.go; contract frozen in S0: order takes the read service). R10 (WS-G adds the dry_run outcome/audit support in auditx; WS-H builds the resource refs in use cases).
- Merge order in Phase 3: F, then H, then G (G touches app/cli wiring that depends on H and ports), then docs pass.

## Phase 0 - Serial prelude (orchestrator)
- S0.1 Decide open questions 1-3 (recommended: R02 option a via audit-log-derived counts or locked state file; R06 allow_override key; R10 resource suffix + dry_run label with core change request). Acceptance: decisions in implementation-notes.md.
- S0.2 Ports prelude: Guard.AllowedFields, policy-error adapter port, resource-ref/outcome types in usecase/ports.go; build green. Depends: S0.1. Acceptance: go build/vet/test green; streams fork from this commit.

## Phase 1 - P1 (parallel; order within stream)
### WS-F
- F1 FR-R01 Okta restricted client (issuer-only host, redirect/downgrade refusal, exit 4). Depends: S0.2. Acceptance: PRD R01 a-d; 307/308 test gets zero second-server requests.
### WS-G
- G1 FR-R06 --policy pin + allow_override, docs request. Acceptance: PRD R06; denied with no HTTP.
- G2 FR-R04 selftest probes audited (distinct verbs, pending-first, abort on audit failure). Acceptance: PRD R04.
- G3 FR-R02 cross-process write limits (or deferral docs per decision). Acceptance: PRD R02; N+1 invocation test.
- G4 FR-R03 CLI wiring for catalog order through guarded read service (pairs with H1). Depends: H1 contract from S0.2.
### WS-H
- H1 FR-R03 order use case via guarded CatalogGet/CatalogVars. Acceptance: denied vars -> exit 6, zero HTTP.
- H2 FR-R05 encoded-query parser, field submission to policy, count, ORDERBY position. Acceptance: PRD R05 table-driven tests.
- H3 FR-R07 create retry re-dedupe loop. Acceptance: first POST commits then 503, second attempt finds record, one POST total.
- H4 FR-R08 pre-write mod-count check, applied-conflict message, audit outcome. Acceptance: PRD R08 d.

## Phase 2 - P2 (parallel)
### WS-F
- F2 FR-R09 login hardening (bad state ignored, Host check, nonce). 
- F3 FR-R14 humanauth >= 90% (refresh rotation failure, store errors, R01/R09 paths); typed Store usage notes to G.
### WS-G
- G5 FR-R10 audit dry_run outcome / resource suffix support in auditx (+ audit line tests).
- G6 FR-R14 typed Env fields, replace Keychain any / Extra, Guard AllowedFields usage, policy-error port adapter wiring.
### WS-H
- H5 FR-R10 resource refs in use cases (incident:INC...). Depends: G5 contract from S0.2.
- H6 FR-R11 idempotency edge cases and key validation (exit 9 before guard).
- H7 FR-R12 untrusted marking inverted + golden tests; FR-R13 field lists, 5xx mapping, oversize body; sn >= 90%.

## Phase 3 - Serial
- H8 FR-R15 docs pass: fold/delete docs/ws-*-requests.md, update technical-details/ADR/user-docs/README, assumptions register. Depends: F, G merged.
- M1 Reviewer pass per branch (security read R01, R03, R04, R05, R06); merge F, H, G; run build, vet, gofmt -l, golangci-lint, go test -race ./... Acceptance: all gates green, 15/15 FRs.
