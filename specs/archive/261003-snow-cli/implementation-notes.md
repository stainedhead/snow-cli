# Implementation Notes: snow-cli (2026-10-03)

Purpose: record decisions, edge cases and deviations as work proceeds; update after each task.

## Technical Decisions
## Edge Cases & Solutions
## Deviations from Plan
## Lessons Learned

## WS-A Foundation notes (A-GATE)
Frozen contracts for B/C/D/E (change only through a request appended here, applied by the integration owner):
- `internal/usecase/ports.go`: TableReader (Get/List/Count), CatalogReader (Search/Item/Variables), IncidentWriter (FindByCorrelation, CreateIncident, UpdateIncident, ResolveIncident), TaskWriter (UpdateTask), OrderWriter (Order), Identity, Clock, IDGen, plus the `Guard` (policy check, audit pending/outcome, action) with `Action{Kind, Request}` and `ActionFunc(ctx, policy.Decision) (httpStatus, error)`. `dry_run_only` decisions reach the ActionFunc with `Allowed=false, DryRunOnly()=true`.
- `internal/cli`: `Router.Register(Command{Path, Summary, Usage, Examples, Forbidden, NoEnv, Flags, Run})`, `Call{Env, Global, Args, Flags}`, `Result{Data, Meta}`, `Env` (ports, Guard, Limits, Profile, Mode, AgentID, RunID, In/Err, Keychain placeholder, Extra map). Each stream implements `Register<Area>` in its own `cmd_<area>.go` (read, write, auth, selftest, skill). Global flags are parsed after the command path (`snow incident get INC1 --format table`); `--` ends flag parsing.
- Stream wiring hooks in `internal/app`: `wire_read.go` (`wireRead`, WS-B), `wire_write.go` (`wireWrite`, WS-C), `human.go` (`newHumanTokenSource`, `newKeychain`, WS-D). Each receives `*Wiring{Env, Client *sn.Client, Profile}` and sets the Env ports. Orchestrator note: these three files are additional single-owner paths beyond the plan table; WS-E may need an equivalent `wire_selftest.go` (create it in the integration phase, not in WS-E).
- `internal/sn`: `Client.Do(ctx, Call{Method, Path (/api/...), Query, Body, SafeToRetry})`, typed errors NotFound/Conflict/Validation/API (all with `HTTPStatus()`), `TableParams` (+ `DefaultOrder`), `TablePath`, `StatsPath`, `Identity`, `DeniedError`/`AdaptPolicyError`. B adds `tables_*.go`, C adds `writes_*.go` on top of `Do`.
- `internal/testsupport/snfake` and `oktafake` (frozen API: `New(t)`, `On/OnFunc/Fail/Records/Error/FixtureFile`, `Requests/Count/Posts`; oktafake `QueueToken/QueueDevice/SetRevoke/Requests`). WS-D extends oktafake by adding new files only (e.g. `authorize.go`).
- `policymap`: verb/resource constants, `IsVerb/IsResource`, `NewRequest/WithFields/WithValues`. E2 adds `builders*.go`.

Decisions and deviations recorded during WS-A:
- D-a host precedence: the configured `instance.host` wins; `SNOW_INSTANCE_HOST` only fills a missing host (the spec sentence was ambiguous). Config hosts may carry an optional `:port` (needed for httptest); schemes, paths, userinfo and wildcards are rejected (exit 2).
- D-b: statuses the core transport does not classify (500, 501, 505 and other 5xx) are mapped by `sn` to `*httpx.RateLimitedError{Attempts: 1}` (exit 8) so the table row "5xx -> rate_limited" holds; 401/403/429/502/503/504 pass through from core unchanged (asserted).
- `--format` with an unknown value is a usage error (exit 2) in the router (core returns a general error from `ParseFormat`).
- `--trace` outside human mode is a policy denial (exit 6) produced by the router after the Env is built; the human-mode trace writer is `os.Stderr` via `httpx.Config.Trace`.
- Policy loading is fail closed: no policy configured -> exit 2; invalid policy -> exit 9 (category validation per the core doc); `--policy agent|human` needs the built-in policies that arrive with WS-E (`app.Options.NamedPolicy` hook; unavailable until then, exit 2).
- Audit: the guard uses `audit.Logger.Log` directly (Block semantics for writes, Warn for reads) so one log file serves both; the pending record uses `Outcome: "pending"`; outcome-write failure after a write returns `errors.Join(actionErr, *auditx.Error{MayHaveHappened: true})` (exit 1).
- Default audit path when the profile sets none: `~/.local/state/snow/audit.jsonl`.
- Daemon: `agentauth.NewUnavailableClient(socket)` fails closed with `*auth.UnreachableError` wrapping `agentauth.ErrAdapterNotBuilt` (exit 3, names the socket from `daemon.socket`; default socket `/var/run/agent-okta-d/agent-okta-d.sock` is a placeholder). `agent-okta-d` is not in go.mod.
- Process note: commit 1c501af (A8) was pushed without the Co-Authored-By trailer because of a shell variable slip; history is not rewritten (no force-push rule).
- `make cover` prints domain/usecase coverage; `make skill` calls `snow skill generate`, which WS-E (E4) implements.

## Spec review (step 2) findings
- Verified against agent-cli-core v0.1.0 source: spec cites are accurate. Clarified: `policy.DeniedError` does not implement `output.CategoryError`, so snow adapts it (A5). `audit.Record` has fixed fields; "pending" is an `Outcome` label. `httpx.Config.Redactor` uses a core-internal type; leave nil.
- tasks.md/plan.md rewritten: test-first ordering, exclusive package ownership per stream, branch/freeze rules, shared test fakes (snfake/oktafake) in WS-A, selftest moved to integration.

## Integration (I1, E3, I2, E7) notes
- Applied the WS-B/C/D/E requests (dispositions at the end of docs/ws-{b,c,d,e}-requests.md). `auditx.Guard.AllowedFields` added; `main` passes `policies.Named`; `make skill-check`; skill golden regenerated.
- E3: `internal/usecase/selftest` + `cmd_selftest.go` + `app/wire_selftest.go`; config gained `selftest.fixture_incident` / `selftest.foreign_incident`; exit mapping documented in docs/technical-details.md. Row expectations are independent of the loaded policy so policy drift fails the matrix.
- I2/E7: `internal/app/integration_test.go` and `security_test.go` drive `cli.RegisterAll` through `NewEnvFactory` with the shipped policies, `snfake`, `oktafake`, `authtest` and an in-memory credential store (`Options.AuditWriter` is a failure-injection seam for the audit-block test).
- Assumption register is machine-checked by `internal/repocheck.TestAssumptionRegisterMatchesTests`.
