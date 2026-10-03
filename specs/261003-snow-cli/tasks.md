# Tasks: snow-cli (2026-10-03) - Status: Planning

Progress: 0/34 tasks complete

Format: ID | depends | est | acceptance.

## WS-A Foundation
- A1 go.mod/Makefile/CI skeleton | - | 1h | build green with core v0.1.0
- A2 config loader + host validation (FR-002, D-a) | A1 | 2h | strict parse tests
- A3 sn client + httpx wiring + status map (FR-006, D-b) | A2 | 3h | every status row tested
- A4 usecase ports + domain types | A1 | 2h | frozen
- A5 auditx Block/Warn (D-c) | A1 | 1h | failing-writer test
- A6 CLI router, global flags, render, exit mapping (FR-003..005) | A4 | 3h | envelope tests
- A7 composition root + daemon stub (FR-016) | A3,A6 | 1h | exit 3 names socket
- A8 version + whoami (FR-001, FR-010) | A7 | 1h | tests
## WS-B Read
- B1 table get/list/count (FR-020..022) | A | 3h
- B2 pagination + acl_filtered_possible (D-h) | B1 | 2h
- B3 cmdb ci get/search/related, app (FR-023..026) | B1 | 4h
- B4 incident/request/ritm/task/problem/change reads, my work (FR-030..037) | B1 | 4h
- B5 catalog search/get/vars (FR-035) | A | 2h
- B6 untrusted marking + field allowlist substitution | B1 | 2h
## WS-C Write
- C1 idempotency + dedupe (FR-043) | A | 2h
- C2 provenance (FR-044) | A | 1h
- C3 incident create (FR-040) | C1,C2 | 4h
- C4 incident update/resolve (FR-041,042) | C2 | 3h
- C5 task update (FR-045) | C2 | 2h
- C6 catalog order (FR-046) | B5,C2 | 3h
- C7 dry-run + confirmation (FR-047,048) | C3 | 2h
## WS-D Human auth
- D1 keychain iface/fakes/stubs/insecure store | A | 2h
- D2 PKCE loopback flow vs httptest Okta (FR-011) | D1 | 4h
- D3 device flow (FR-012) | D1 | 3h
- D4 refresh/rotation/status/logout (FR-013..015) | D2 | 3h
- D5 wire human token source (FR-017) | D4 | 1h
## WS-E Policy/selftest/docs/CI
- E1 policy files + parse/load tests (FR-052, D-d, D-f) | A | 3h
- E2 policymap request builders | A | 2h
- E3 selftest rows/probe (FR-050, D-k) | B,C | 3h
- E4 docgen skill + drift check (FR-051) | A6 | 2h
- E5 CI workflow (FR-054) | A1 | 2h
- E6 docs: ADR, deferred, CRs, root-skill note, m0 checklist, assumptions register, user-docs | all | 4h
- E7 token-leak and security tests (FR-053) | all | 2h
