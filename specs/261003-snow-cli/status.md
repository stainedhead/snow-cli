# Status: snow-cli

Created: 2026-10-03

| Phase | Name | Status |
|---|---|---|
| 0 | Spec creation | In Progress |
| 1 | WS-A Foundation | Done (A1-A10, A-GATE passed) |
| 2 | WS-B/C/D/E parallel workstreams | Not Started |
| 3 | Integration and docs | Not Started |
| 4 | Review and hardening | Not Started |

## Phase 0 checklist
- [x] Spec created from PRD (spec.md)
- [x] Research questions identified (research.md)
- [x] Phase files initialized
- [x] Spec review (dev-flow step 2), tasks and plan revised, core v0.1.0 API verified

## A-GATE (WS-A freeze)
- [x] gofmt -l . empty
- [x] go vet ./... clean
- [x] golangci-lint run: 0 issues
- [x] go test -race ./... green
- [x] coverage: domain 98.3%, usecase 100%, usecase/whoami 100% (gate 90%)
- [x] go mod tidy: no diff; no replace, no pseudo-version, no agent-okta-d in go.mod
- [x] layer guard test: domain/usecase import no net/http, os, keychain or adapters
- [x] Assumption-named tests exist for A-01, A-02, A-03, A-05, A-11 (the assumptions introduced in WS-A)

**Freeze SHA: 8bfc2770985d3eb0fb1d6647896fb26798665524** (last WS-A code commit on feat/snow-cli). Frozen surface: internal/usecase/ports.go, internal/cli (router, flags, render, env, root, cmd_* stubs), internal/testsupport/snfake and oktafake, internal/policymap vocabulary, internal/sn client/errors/query/identity, internal/domain, internal/config, internal/auditx, internal/app (except wire_read.go, wire_write.go, human.go which are stream hooks), go.mod, go.sum, Makefile, cmd/snow. Worker branches are created by the orchestrator from this SHA.

## Blockers
None.

## Recent activity
- 2026-10-03: spec created, PRD moved into spec dir.
- 2026-10-03: WS-A A1-A10 and A-GATE complete; freeze recorded above.
- 2026-10-03: (earlier) WS-A A1-A8 complete on feat/snow-cli (go.mod, testsupport, domain+ports, config, sn client, policymap vocab, auditx, CLI router).
