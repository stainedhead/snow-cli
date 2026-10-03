# Plan: snow-cli (2026-10-03) - Status: Planning

## 1. Development Approach
Strict TDD, fakes/httptest only, 90% coverage on domain/usecase. Foundation first (WS-A), then four parallel workstreams each in its own worktree/branch merged to feat/snow-cli. Ports are frozen at the end of WS-A so streams do not collide.

## 2. Package Layout
```
go.mod                      module github.com/stainedhead/snow-cli; go 1.27; require agent-cli-core v0.1.0
cmd/snow/main.go            composition root (wires config, token source, httpx, policy, audit, usecases, cli)
internal/app/               builder: Deps struct, newDaemonClient(), newKeychain()
internal/cli/               command tree, flags, render helper, confirm prompt
internal/config/            strict YAML config, profiles, host validation
internal/domain/            records, SysID/Number, Page, scale
internal/usecase/           read/, write/, selftest/, ports.go
internal/sn/                client, errors (CategoryError), query builders, pagination, catalog
internal/policymap/         verb/resource vocabulary, request builders, mapping helpers
internal/idempotency/       key derivation, dedupe
internal/provenance/        note prefix, correlation_display
internal/auditx/            Block/Warn wiring, pending/outcome records
internal/agentauth/         daemon stub
internal/humanauth/         pkce/, device/, session/, keychain/ (iface, fake, stubs)
policies/                   agent.policy.yaml, human.policy.yaml
docs/                       ADR, deferred.md, core-change-requests.md, root-skill-update-needed.md, m0-spike-checklist.md, assumptions.md, technical-details etc.
user-docs/                  install, getting started, config reference, usage, troubleshooting
.github/workflows/ci.yml
```

## 3. Phase Breakdown and Parallel Workstreams

### WS-A Foundation (serial, first)
go.mod + require core v0.1.0, Makefile targets, cmd skeleton + command router, config loader (host validation), sn client with httpx transport (AllowedHosts, Refresher), status-to-CategoryError map, ports (usecase/ports.go), composition root with daemon stub, audit wiring (Block/Warn), render helper, `snow version`, `whoami`. Exit gate: build/vet/test/lint green; ports frozen.

### Parallel after WS-A (separate worktrees)
- **WS-B Read path (M1)**: table get/list/count, cmdb ci get/search/related, cmdb app, my work, incident get/list, request/ritm get/list, task get/list, catalog search/get/vars, change get/list, problem get/list; pagination, acl_filtered_possible, untrusted marking, fixtures.
- **WS-C Write path (M2, M4 order)**: incident create/update/resolve, task update, catalog order, idempotency, provenance, dry-run, confirmation hook, sys_mod_count guard, safe-to-retry marking, audit pending/outcome.
- **WS-D Human auth (M3)**: PKCE loopback, device flow, httptest Okta, session store, keychain interface + fakes + fail-closed stubs, `--insecure-store`, auth login/logout/status, refresh/rotation, human TokenSource wired to httpx.
- **WS-E Policy, selftest, docs, CI**: policy files + mapping tests (parse via core), policymap request builders, selftest rows/probe, docgen skill generation + drift check, `snow version` details, CI workflow, docs/ and user-docs/ drafts, root-skill-update-needed, deferred, core-change-requests, m0 checklist.

Dependencies: WS-B/C use policymap from WS-E only through the frozen vocabulary in WS-A (policymap skeleton lands in WS-A); WS-E selftest probes consume WS-B/C usecases, so selftest rows are finalized in integration.

### Integration phase
Merge streams, end-to-end fake-server tests per milestone acceptance, assumption register, user-docs.

## 4. Critical Path
WS-A -> WS-B (reads feed selftest/my work) -> integration; WS-C depends on WS-B's sn query helpers only via WS-A ports.

## 5. Testing Strategy
Table-driven unit tests; httptest fake ServiceNow (fixture JSON, injectable status/latency/header faults, POST counter); httptest fake Okta; core authtest.Fake for tokens; failing-writer for audit; in-memory keychain; race detector; token-leak grep tests; Assumption-named tests.

## 6. Rollout Strategy
Merge to feat/snow-cli, PR to main later (release workflows out of scope).

## 7. Success Metrics
All milestone acceptance rows in spec section 10 pass; coverage at least 90% domain/usecase; lint/vet/race clean.
