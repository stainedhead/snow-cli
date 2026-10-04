# Plan: snow-cli Auto Review Fixes
Date: 2026-10-03 | Status: Planning

## Development Approach
TDD per FR; three parallel workstreams in linked worktrees (feat/snow-cli-ws-f, -ws-g, -ws-h) with exclusive package ownership; P1 before P2 in each stream; one reviewer pass per branch.

## Phase Breakdown
- Phase 0 (serial): decide open questions; S0 shared-file prelude on feat/snow-cli (ports.go, domain audit types, docs skeleton).
- Phase 1 (parallel): P1 fixes. Phase 2 (parallel): P2 fixes.
- Phase 3 (serial): merge F, G, H into feat/snow-cli, R15 docs pass, assumptions register, final gates.

## Critical Path
Open question decisions -> S0 -> Phase 1 -> Phase 2 -> Phase 3. WS-H is the longest stream (R05, R07, R08, R03 use-case part, R11, R12, R13, R15).

## Testing Strategy
httptest Okta/ServiceNow fakes, authtest.Fake, audit sink failure injection; gofmt, vet, golangci-lint, go test -race, coverage >= 90%.

## Rollout Strategy
Merge to feat/snow-cli only after reviewer pass. No tags or releases.

## Success Metrics
15/15 FRs accepted; gates green; no docs/ws-*-requests.md remain.
