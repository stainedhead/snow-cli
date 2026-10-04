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

### Verified core API (agent-cli-core v0.1.0, read from source)
- `output`: `Envelope`, `Meta{Truncated,NextOffset,Count,RequestID}`, `Success(data, *Meta)`, `Failure(Category,msg,hint)`, `FromError(err)`, `FromErrorWithSecrets`, `Write(w, env, Options{Format,Bounds{MaxBytes,Offset},Secrets}) error`, `Render`, `ParseFormat`, `Untrusted{Value,Author,Timestamp}`, `ExitCode` (0..9), `Category*`, `CategoryError`/`Hinter` interfaces, `ExitOf`, `Envelope.ExitCode()`.
- `auth`: `Token` (`NewToken`, redacting), `TokenSource{Token(ctx)}`, `Refresher`, `DaemonClient{Fetch,Refresh(ctx, provider)}`, `NewDaemonTokenSource(client, provider, ...Option{WithRemediation})`, `NewAuthorizer(src)` (`Authorize(ctx, req)`, `Refresh(ctx)`), `*UnreachableError`, `*ActionRequiredError`, `*TokenError`, `ErrReauthRequired`; test fake `auth/authtest` (`New(Scenario, WithSocket)`).
- `httpx`: `NewClient(Config) *http.Client`, `NewTransport(base, Config)`, `Config{MaxRetries,BaseDelay,MaxDelay,MaxWait,Jitter,Clock,Rand,Refresher,VendorCode,Trace,AllowedHosts,AllowInsecureHTTP,Redactor}` (Redactor type lives in core `internal/redact`; leave nil), `MarkSafeToRetry(req)`, `*ForbiddenHostError`, `*ForbiddenError`, `*AuthError`, `*RateLimitedError`.
- `policy`: `Load(path, ...WithWritable)`, `Parse`, `Policy.Evaluate(Request{Verb,Resource,Fields})`, `NewEngine(p, Clock).Check(req) Decision`, `Decision{Allowed,Mode,RuleID,Reason}`, `Decision.Err()` -> `*DeniedError` (does NOT implement `CategoryError`; snow needs the adapter), `Limits.ClampResults/ClampBytes`.
- `audit`: `Open(Config{Path,OnFailure}, ...Option)`, `NewLogger(w, ...)`, `WithSecrets/WithFailureMode/WithOnWriteError/WithClock`, `Warn`/`Block`, `Record{Tool,AgentID,RunID,Verb,Resource,Outcome,HTTPStatus,Duration,PolicyDecision}` (no free-form fields: "pending" is an `Outcome` label), `Log(rec)`, `Handle(rec, actionErr)`.
- `selftest`: `Row{Name,Verb,Resource,Expect(Allow|Deny),ReadOnly}`, `Probe func(ctx, Row)(Outcome, error)`, `Runner{Rows,Probe,ReadOnly}.Run`, `Result.Write/Envelope/ExitCode`.
- `docgen`: `Generate(CommandTree{Name,Description,Commands[]Command{Name,Description,Usage,Examples,Forbidden}})`.
Any signature drift found while coding is recorded in `docs/core-change-requests.md`, never fixed in core.

### Branching and ownership rules
Each parallel workstream works in its own worktree on branch `feat/snow-cli-ws-<x>` cut from the WS-A freeze commit (tag-free; record the SHA in status.md) and merges into `feat/snow-cli` with no-ff. A stream edits ONLY the paths it owns (see ownership table); changes to a frozen port or to another stream's package go through a request noted in `implementation-notes.md` and are applied by the integration owner. Shared files (`go.mod`, `go.sum`, `Makefile`, `internal/cli/root.go` command registry, `cmd/snow/main.go`) are WS-A-owned; streams add commands by creating their own file `internal/cli/cmd_<area>.go` that registers via `init()`-free explicit `Register<Area>(r *Router)` functions, which WS-A pre-stubs in the router (one empty stub per area) so no stream edits `root.go`. Each stream commits tests first (red), then implementation (green), per task.

| Stream | Owns (exclusive) | May read only |
|---|---|---|
| WS-A | go.mod, go.sum, Makefile, .golangci.yml, cmd/snow, internal/app, internal/config, internal/domain, internal/usecase/ports.go, internal/sn (client, errors, query, page), internal/auditx, internal/agentauth, internal/policymap (vocabulary + skeleton), internal/cli (router, flags, render, stubs) | - |
| WS-B | internal/usecase/read/**, internal/sn/tables_*.go (read endpoints), internal/cli/cmd_read.go, testdata/fixtures/read/** | A-owned |
| WS-C | internal/usecase/write/**, internal/idempotency/**, internal/provenance/**, internal/sn/writes_*.go, internal/cli/cmd_write.go, testdata/fixtures/write/** | A-owned, B sn helpers via ports |
| WS-D | internal/humanauth/**, internal/cli/cmd_auth.go, internal/app/human.go (single file hook) | A-owned |
| WS-E | policies/**, internal/policymap/builders*.go + tests, internal/usecase/selftest/**, internal/cli/cmd_selftest.go, cmd_skill.go, .github/**, docs/**, user-docs/** | all |

### WS-A Foundation (serial, first; nothing else starts until its exit gate passes)
go.mod + require core v0.1.0 (no replace), Makefile targets (build, test, race, vet, lint, cover, skill), cmd skeleton + command router with per-area Register stubs, config loader (host validation), sn client with httpx transport (AllowedHosts, Refresher), status-to-CategoryError map plus policy-denial adapter, ports (usecase/ports.go), domain types, policymap vocabulary and request-builder skeleton, composition root with daemon stub, auditx (Block/Warn, pending/outcome via `Record.Outcome`), render helper, `snow version`, `whoami`, fake ServiceNow + fake Okta test harness packages (`internal/testsupport/snfake`, `oktafake`) used by all streams. Exit gate: build/vet/race/lint green, coverage gate met, ports + router + testsupport frozen (SHA recorded).

### Parallel after WS-A (separate worktrees)
- **WS-B Read path (M1)**: table get/list/count, cmdb ci get/search/related, cmdb app, my work, incident get/list, request/ritm get/list, task get/list, catalog search/get/vars, change get/list, problem get/list; pagination, acl_filtered_possible, untrusted marking, fixtures.
- **WS-C Write path (M2, M4 order)**: incident create/update/resolve, task update, catalog order, idempotency, provenance, dry-run, confirmation hook, sys_mod_count guard, safe-to-retry marking, audit pending/outcome.
- **WS-D Human auth (M3)**: PKCE loopback, device flow, httptest Okta, session store, keychain interface + fakes + fail-closed stubs, `--insecure-store`, auth login/logout/status, refresh/rotation, human TokenSource wired to httpx.
- **WS-E Policy, selftest, docs, CI**: policy files + mapping tests (parse via core), policymap request builders, docgen skill generation + drift check, CI workflow, docs/ and user-docs/ drafts, root-skill-update-needed, deferred, core-change-requests, m0 checklist. Selftest rows/probe (E3) start only after B and C merge.

Dependencies: WS-B/C/E consume policymap only through the vocabulary frozen in WS-A. WS-E selftest probes consume WS-B/C usecases through ports, so E3 runs in the integration phase.

### Integration phase (owner: one agent, serial)
Merge B, C, D, E in that order; resolve nothing by editing another stream's files except mechanical conflicts; run E3, E6/E7 finalization, end-to-end fake-server tests per milestone acceptance, assumption register, user-docs.

## 4. Critical Path
WS-A -> (B || C || D || E1/E2/E4/E5) -> integration (E3, E6, E7). WS-C depends on WS-B only through WS-A ports; catalog order (C6) needs the catalog read client, so catalog read endpoints (B5) are placed in WS-A-defined port `CatalogReader` and C6 uses the fake until merge.

## 5. Testing Strategy
Table-driven unit tests; httptest fake ServiceNow (fixture JSON, injectable status/latency/header faults, POST counter); httptest fake Okta; core authtest.Fake for tokens; failing-writer for audit; in-memory keychain; race detector; token-leak grep tests; Assumption-named tests.

## 6. Rollout Strategy
Merge to feat/snow-cli, PR to main later (release workflows out of scope).

## 7. Success Metrics
All milestone acceptance rows in spec section 10 pass; coverage at least 90% domain/usecase; lint/vet/race clean.
