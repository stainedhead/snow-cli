# Tasks: snow-cli (2026-10-03) - Status: Planning

Progress: 10/40 tasks complete

Format: `ID | depends | est | owner paths | acceptance`. Every task is TEST-FIRST: commit the failing tests (red), then the implementation (green), then refactor; a task is done only when `go build ./... && go vet ./... && go test -race ./...` and `golangci-lint run` pass and domain/usecase coverage stays >= 90%. Ownership and branch rules are in plan.md section 3: a worker edits only the paths in its stream.

## Critical rule for workers
Shared files (go.mod, Makefile, internal/cli/root.go, cmd/snow/main.go, ports.go) are WS-A-owned and frozen after gate A-GATE. Need a change? Append to implementation-notes.md and stop; do not edit.

## WS-A Foundation (serial; one worker; branch feat/snow-cli directly)
- [x] A1 go.mod (module github.com/stainedhead/snow-cli, go 1.27, require agent-cli-core v0.1.0), Makefile (build test race vet lint cover skill), .golangci.yml check | - | 1h | go.mod, Makefile | `go build ./...` green; no replace/pseudo-version (test greps go.mod)
- [x] A2 testsupport fakes: `internal/testsupport/snfake` (httptest ServiceNow: fixture JSON, status/latency/header fault injection, POST counter, X-Total-Count) and `oktafake` skeleton | A1 | 3h | internal/testsupport/** | self-tests for fault injection and POST counter
- [x] A3 domain types (SysID, Number, Page, Record, scale) + usecase ports (`ports.go`: TableReader, CatalogReader, IncidentWriter, TaskWriter, OrderWriter, Identity, Clock, IDGen) | A1 | 2h | internal/domain, internal/usecase/ports.go | table-driven validation tests
- [x] A4 config loader + host validation (FR-002, D-a): strict YAML, profiles, rejects scheme/path/userinfo/wildcard hosts, unknown keys exit 2 | A1 | 2h | internal/config | strict-parse tests incl. SNOW_INSTANCE_HOST precedence
- [x] A5 sn client + httpx wiring (AllowedHosts, Refresher=auth.Authorizer) + status map to CategoryError (FR-006, D-b) + policy-denial adapter (DeniedError -> policy_denied) | A2,A4 | 4h | internal/sn | every status row of D-b tested against snfake; passthrough of core Forbidden/Auth/RateLimited asserted; redirect to other host -> exit 4
- [x] A6 policymap vocabulary (verb/resource constants, D-f) + builder skeleton | A3 | 1h | internal/policymap | vocabulary table test
- [x] A7 auditx Block/Warn wiring, pending/outcome records via `audit.Record.Outcome` (D-c) | A1 | 2h | internal/auditx | failing-writer test: Block aborts before HTTP; outcome-write failure joined with result
- [x] A8 CLI router, global flags, render helper (output.Write), exit mapping, per-area Register stubs (read, write, auth, selftest, skill) (FR-003..005) | A3 | 3h | internal/cli | envelope golden tests; unknown flag exit 2
- [x] A9 composition root + `newDaemonClient()` stub (auth.UnreachableError, exit 3 naming socket) + `newKeychain()` placeholder (FR-016) | A5,A7,A8 | 2h | cmd/snow, internal/app, internal/agentauth | exit-3 test with authtest.Fake and stub
- [x] A10 `snow version` + `whoami` (FR-001, FR-010) | A9 | 1h | internal/cli | tests
- A-GATE freeze: build/vet/race/lint/cover green; record freeze SHA in status.md; create worker branches | A1-A10 | 0.5h | status.md | gate checklist ticked

## WS-B Read path (parallel; branch feat/snow-cli-ws-b; owns usecase/read, sn/tables_*.go, cli/cmd_read.go, testdata/fixtures/read)
- B1 table get/list/count (FR-020..022) | A-GATE | 3h | acceptance: fixtures + policy check per call
- B2 pagination + acl_filtered_possible + deterministic ORDERBY (D-h) | B1 | 2h | offset math tests incl. empty/short page
- B3 cmdb ci get/search/related, cmdb app (FR-023..026) | B1 | 4h
- B4 incident/request/ritm/task/problem/change reads, my work (FR-030..037) | B1 | 4h
- B5 catalog search/get/vars (FR-035) implementing `CatalogReader` | A-GATE | 2h
- B6 untrusted marking (output.Untrusted) + field allowlist substitution | B1 | 2h | injection-text fixture test; R-04 truncation of `items` check (CR if needed)

## WS-C Write path (parallel; branch feat/snow-cli-ws-c; owns usecase/write, idempotency, provenance, sn/writes_*.go, cli/cmd_write.go, testdata/fixtures/write)
- C1 idempotency key derivation + dedupe (FR-043) | A-GATE | 2h
- C2 provenance note prefix + correlation_display (FR-044) | A-GATE | 1h
- C3 incident create (FR-040) incl. safe-to-retry only after dedupe miss | C1,C2 | 4h | POST-count test under injected 503
- C4 incident update/resolve with sys_mod_count guard (FR-041,042) | C2 | 3h
- C5 task update (FR-045) | C2 | 2h
- C6 catalog order, unmarked POST until A-07 (FR-046) against `CatalogReader` fake | C2 | 3h
- C7 dry-run + confirmation hook (FR-047,048) | C3 | 2h | dry-run sends zero requests

## WS-D Human auth (parallel; branch feat/snow-cli-ws-d; owns humanauth/**, cli/cmd_auth.go, app/human.go)
- D1 keychain interface + fake + fail-closed stubs + `--insecure-store` | A-GATE | 2h
- D2 PKCE loopback flow vs oktafake (FR-011) | D1 | 4h
- D3 device flow (FR-012) | D1 | 3h
- D4 refresh/rotation/status/logout (FR-013..015) | D2 | 3h
- D5 human TokenSource wired via app/human.go; `auth login` disabled in agent mode, exit 6 (FR-017) | D4 | 1h

## WS-E Policy/docs/CI (parallel; branch feat/snow-cli-ws-e; owns policies, policymap builders, usecase/selftest, cli/cmd_selftest.go, cmd_skill.go, .github, docs, user-docs)
- E1 policy files + Parse/Load tests, strict unknown keys (FR-052, D-d, D-f) | A-GATE | 3h
- E2 policymap request builders | A6 | 2h
- E4 docgen skill generation + drift check (`make skill`) (FR-051) | A8 | 2h
- E5 CI workflow (FR-054) | A1 | 2h
- E8 docs drafts: ADR, deferred, core-change-requests, root-skill-update-needed, m0-spike-checklist, assumptions register | A-GATE | 3h

## Integration (serial; one worker on feat/snow-cli; merge order B, C, D, E)
- I1 merge B, C, D, E; gofmt/vet/race/lint green | all | 2h
- E3 selftest rows/probe over merged usecases (FR-050, D-k), read-only default, `--include-writes` | I1 | 3h
- I2 end-to-end fake-server tests per milestone acceptance (spec section 10) | I1 | 4h
- E6 user-docs finalization and assumptions register cross-check | I2 | 2h
- E7 token-leak and security tests (FR-053) | I1 | 2h
- I3 final gate: coverage, assumption tests present (name contains Assumption), status.md updated | all | 1h
