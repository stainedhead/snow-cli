# Feature Specification: snow CLI

- **Spec dir:** specs/261003-snow-cli
- **Created:** 2026-10-03 (PRD Draft v0.1)
- **Source PRD:** `specs/261003-snow-cli/snow-cli-PRD.md`
- **Status:** Draft for review (dev-flow step 1)
- **Core dependency:** `github.com/stainedhead/agent-cli-core` v0.1.0 (no `replace`, no pseudo-version). Where the core PRD and PRD section 5 differ, the core wins.

Evidence markers: items the PRD marks unconfirmed stay ASSUMPTIONS (section 13, A-nn). Each appears in code as `ASSUMPTION(unverified against a real instance): ...`, in a test whose name contains `Assumption`, and in `docs/` (assumptions register).

## 1. Executive Summary

`snow` is a Go CLI giving autonomous SDLC agents and human teammates task-shaped access to ServiceNow: table reads, CMDB CI lookup, work items (incident, request, RITM, task, change, problem), catalog search/order, incident create/update/resolve, and a `selftest` allow/deny matrix. No raw REST passthrough. Two authentication modes share one command surface: agent mode (Okta token via the credential daemon) and human mode (Okta PKCE or device flow, tokens in the OS keychain). ServiceNow roles and ACLs are the security boundary; the client policy engine is a guardrail only.

## 2. Problem Statement

No suitable stock ServiceNow CLI exists for agent use (`now-sdk` targets app development). Agents need bounded, structured, attributable, idempotent ITSM operations without holding any ServiceNow secret, and humans need the same verbs as themselves.

## 3. Goals / Non-Goals

Goals G1-G5 are as in PRD section 2 (no ServiceNow secret on agent hosts; human parity; attributable and idempotent writes; LLM-safe output; testable minimal permissions through `selftest`).
Non-goals: raw REST (`snow raw`/`snow api`), CMDB writes, approvals, deletes, user/group/role admin, scripts/flows, ServiceNow MCP use. Out of scope for this build (per orchestration rules): M0 spikes (a checklist doc is delivered instead), the ServiceNow scoped app, native Windows, release workflows, real OS keychain backends beyond fail-closed stubs, the real daemon adapter.

## 4. Cross-cutting Design Decisions (resolve the PRD review)

### D-a Instance host and host allowlist
The instance is configured, never derived from a token or flag: `instance.host` in the snow config file (and `SNOW_INSTANCE_HOST`, config wins over nothing else; the `--profile` selects the block). The sn client builds URLs only from that host, `https` only (loopback http permitted solely for httptest). The httpx transport is created with `AllowedHosts: [instance.host]` (plus the Okta issuer host for human-mode token endpoints on a separate transport that has its own AllowedHosts). A redirect or any other host yields the core's `*httpx.ForbiddenHostError` (exit 4). The config loader rejects hosts that are not a bare DNS name (no scheme, path, userinfo, wildcards). API versions are pinned in URLs (`/api/now/v1/...`; PRD section 4).

### D-b HTTP status to exit code map, via output.CategoryError
`internal/sn` defines typed errors that implement `output.CategoryError` (and `output.Hinter`), so `output.FromError`/`ExitOf` yield exit codes with no switch in `cmd`:

| ServiceNow response | Error type (package) | Category | Exit |
|---|---|---|---|
| 401 | core httpx: refresh once via `Refresher`, retry, second 401 -> `*httpx.AuthError` | auth | 3 |
| 403 | core `*httpx.ForbiddenError` | forbidden | 4 |
| 404 | `sn.NotFoundError` | not_found | 5 |
| 409, 412 | `sn.ConflictError` | conflict | 7 |
| 400, 422 | `sn.ValidationError` (message from SN `error.message`, scrubbed, bounded) | validation | 9 |
| 429, 5xx (after core's bounded retries) | core `*httpx.RateLimitedError` | rate_limited | 8 |
| other 4xx | `sn.APIError` | general | 1 |
| client policy denial | wraps `*policy.DeniedError` via a small adapter implementing CategoryError | policy_denied | 6 |

Clarifications: ServiceNow commonly returns 404 ("No Record found") for ACL-hidden records and 403 for operation denial (A-03); exit 5 for a record the identity cannot see is therefore expected. If core httpx already classifies 403/429/5xx the snow client does not re-map them (test asserts passthrough). Error bodies are never echoed beyond the SN `error.message`/`detail` fields, scrubbed by the core redactor and length-capped.

### D-c Write safety: audit block-on-failure, retry safety, correlation dedupe
- Writes (incident create/update/resolve, task update, catalog order) run under an audit logger in `audit.Block` mode. A "pending" audit record is written BEFORE the HTTP request; failure to write it aborts with no request sent (exit 1, message names the audit path). The outcome record is written after; if that write fails, the command returns the audit error joined with the action result (exit 1) and the message states that the write may have happened and gives the record number. Reads use `audit.Warn`.
- Core httpx retries only idempotent methods unless `httpx.MarkSafeToRetry(req)` is applied. `snow` marks POST create/order requests safe to retry ONLY after the correlation_id dedupe query ran in the same invocation and found no existing record, and only for operations whose dedupe key is stored server-side (incident create via `correlation_id`). Catalog `order_now` is marked safe only if A-07 (a retrievable dedupe key on `sc_request`/`sc_req_item`) is confirmed; until then order POSTs are NOT marked safe-to-retry and a transient failure exits 8 with a hint to check `snow request list` before retrying. PATCH is retried only with the `sys_mod_count` guard (D-j below). Tests: the fake server counts POSTs under injected 503s for marked and unmarked requests.
- Idempotency key: `--idempotency-key`, default `sha256(agent_id|ci|short_description|hour_bucket)` truncated, stored in `correlation_id`. Dedupe query: active record with that `correlation_id`; hit returns the record with `deduplicated: true` in `data` (see D-e).

### D-d impact/urgency scale (inversion defect)
ServiceNow scale: 1 = High, 2 = Medium, 3 = Low (priority 1 = Critical is derived). PRD section 9's `max_impact_urgency: 2  # i.e. nothing above "High"` is an inversion defect: the core `Constraint.Max: 2` would allow 1 (the HIGHEST) and 2, and forbids Low (3), the opposite of the intent. PRD 13.5 intends: agents may be allowed to create up to "High" but never P1/Critical, and a human escalates. Spec decision: the agent policy constrains `impact` and `urgency` with `min: 2, max: 3` (agent may not set 1), and the config exposes `incident.scale` (default values 1/2/3) so an instance with a different scale can adjust. The policy key `max_impact_urgency` is not carried into the file format; the mapping doc records the inversion and the corrected constraint. Also, the P1 block belongs in the record producer (server side, out of scope) too. A-01 records that the scale is the OOB scale. Test: Assumption-named test that value 1 is denied (exit 6), 2 and 3 allowed, for both fields.

### D-e Envelope extras live in `data`
Core `output.Meta` is fixed (`truncated`, `next_offset`, `count`, `request_id`). `acl_filtered_possible`, `deduplicated`, pagination detail and `dry_run` go in `data`. List commands return `data = {"items": [...], "page": {"offset": N, "returned": n, "total": T|null, "next_offset": M|null}, "acl_filtered_possible": bool}`; single-record commands return the record (untrusted free text wrapped in `output.Untrusted`) plus `deduplicated` where relevant. Core sets `meta.count` only for array data, so list commands set `meta.count` themselves (core keeps caller values for non-array data). Open check R-04: confirm how core truncation handles object data with an `items` array; if it only truncates top-level arrays, file a core change request in `docs/core-change-requests.md` and fall back to snow trimming `items` to fit `--max-bytes` itself.

### D-f Policy schema mapping (core strict schema)
The PRD section 9 sketch is NOT a loadable format. Shipped policy files (`policies/agent.policy.yaml`, `policies/human.policy.yaml`) are written in the core's schema (`version: 1`, `limits`, `rate_limit`, ordered `rules` with `id`, `effect`, `verbs`, `resources`, `mode`, `fields`, `constraints`, `rate_limit`) and a unit test `policy.Parse`s each (strict: unknown keys fail). Conventions:
- **Verbs** (opaque strings chosen by snow): `get`, `list`, `count`, `search`, `related`, `create`, `update`, `resolve`, `order`, `vars`, `whoami`, `selftest`.
- **Resources**: `table:<name>` (get/list/count/update on any table, incl. the typed verbs below), `cmdb:ci`, `cmdb:app`, `incident`, `request`, `ritm`, `task` (sc_task), `change`, `problem`, `catalog:item:<sys_id>` (order/get/vars), `catalog:search`.
- **Mapping table** (PRD section 9 to core):

| PRD construct | Core rule |
|---|---|
| `tables.allow.<t>.verbs` | allow rule, `verbs` = those verbs, `resources: [table:<t>]` |
| `tables.allow.<t>.fields` / `update_fields` | rule `fields` allowlist on the request field names (for reads, the effective `--fields`; when `--fields` is omitted snow substitutes the allowlist, so it never reads outside it) |
| `include_children` | pattern `table:cmdb_ci*` plus resolving children through the class hierarchy cache |
| `tables.deny` globs | deny rules (deny always wins in core): `table:sys_*`, `table:sys_user`, `table:sys_user_has_role`, `table:sysapproval_approver`, `table:sys_properties` |
| `incident.create.mode` | `mode` on the create allow rule |
| `incident.create.require` | NOT policy (core cannot express required fields): validated by snow before policy (exit 9) |
| `incident.create.max_impact_urgency` | `constraints.impact/urgency: {min: 2, max: 3}` (D-d) |
| `incident.create.max_per_hour` | rule `rate_limit.per_hour` |
| `incident.update.allow_fields` | rule `fields` |
| `incident.resolve: {mode: deny}` | allow-less (default deny) or explicit rule with `mode: deny` |
| `catalog.order.mode: dry_run_only`, `allow_items` | one rule `catalog:item:*` `mode: dry_run_only` default; per-opted-in item a rule with that sys_id `mode: allow` placed before it |
| `limits.max_results/max_bytes` | top-level `limits` |
| `limits.max_writes_per_run` | `rate_limit.per_run` on EACH write rule (core has no shared write counter; deviation documented; a global `rate_limit.per_run` also counts reads so is not used for this) |
| `audit.path`, `profile` | snow config file, not policy |

Per-field list reads: snow evaluates one policy request per command with `Fields` = requested field names and values for writes. Policy file permission checks use the core loader (fail closed on invalid; warn or refuse on writable file per mode). `--policy` selects `agent`, `human` or a path.

### D-g Deferred
Redaction hook (PRD section 11 optional hook) and signed/detached-signature policy (core CORE-POL-7 is unbuilt) are deferred: documented in `docs/deferred.md`; the config accepts no `policy.signature` key (strict parse rejects it) so nothing silently pretends to verify. M5 items that depend on release signing are out of scope.

### D-h Pagination
`--limit N` -> `sysparm_limit` (clamped by `limits.max_results`); `--offset N` -> `sysparm_offset`; deterministic ordering is always applied (`ORDERBYDESCsys_updated_on^ORDERBYsys_id` unless the caller orders) so offsets are stable. Total comes from `X-Total-Count`. `data.page.next_offset` is the absolute `sysparm_offset` for the next page (null when exhausted). Core `meta.next_offset` is set only when core truncates for `--max-bytes`; core's value is an index into the returned items of THIS page, so the next call is `--offset <previous offset + meta.next_offset>`; both are documented in the generated skill and user-docs, and a core change request asks for an `OffsetBase` option (R-05). Empty/short page with more pages remaining sets `acl_filtered_possible: true` (PRD 7.1: the Table API applies the limit before ACLs).

### D-i Authentication wiring
Agent mode: `auth.NewDaemonTokenSource` over `newDaemonClient()` which in this release returns a client reporting the daemon unavailable (core `*auth.UnreachableError`, exit 3, message names the socket path from config; `agent-okta-d` is NOT in go.mod; ADR + `docs/deferred.md`). `httpx.Config.Refresher` is an `auth.Authorizer` so 401 -> single refresh -> retry -> exit 3. Human mode: see FR-AUTH-*. Mode comes from config/profile only (no flag), and `auth login` is disabled when the profile mode is `agent` (policy-and-config enforced, exit 6).

### D-j Concurrency
`incident update`/`task update`: read `sys_mod_count` and fields, PATCH, re-read; if `sys_mod_count` advanced by more than one the result is `conflict` (exit 7, A-09). No ETag.

### D-k Selftest mapping
`snow selftest` builds `[]selftest.Row` from the policy/identity matrix (PRD sections 8.3 and 10) and a `selftest.Probe` that performs the real read probe or a policy+dry-run/negative probe and maps the outcome to `selftest.Allow`/`selftest.Deny`: HTTP 2xx -> Allow; 403/404-on-ACL, client policy denial -> Deny; any other error -> probe error (row fails with detail). Each row carries `Verb` and `Resource` using the policy vocabulary in D-f, `Name` = matrix id (`m8-3-row-N`), and `ReadOnly` true for every row that cannot mutate. Write rows (confirm `resolve` is denied; cross-record update denied) are executed against a configured selftest fixture record only with `--include-writes`; the default is `Runner.ReadOnly = true`. Core semantics noted: a failing matrix is a CategoryGeneral failure so `snow selftest` exits 1 (not 4/6); the per-row detail is in the failure message. The selftest needs a live instance and is never run in PR CI (BLD-4); CI runs it against an httptest fake.

### D-l Human mode and testing posture
Okta PKCE (loopback 127.0.0.1, state check, RFC 8252, port list 8765-8769 tried in order, A-06) and device flow are tested against an httptest Okta (authorization, token, device, revoke, introspection-free). Keychain access is behind `keychain.Store` with in-memory and failing fakes; real macOS/Linux backends and WSL2 storage are fail-closed stubs returning a clear error (exit 3) unless `--insecure-store` is given, which stores to a 0600 file with an explicit warning. Refresh rotation is honoured (new refresh token persisted atomically). `auth logout` revokes at Okta and deletes the store entry even if revoke fails (reports both).

## 5. Functional Requirements

Commands (PRD section 7). Every command honours global flags `--format json|table|text`, `--fields`, `--limit`, `--offset`, `--max-bytes`, `--dry-run`, `--idempotency-key`, `--profile`, `--policy`, `--trace`. Policy resource/verb pairs follow D-f. All free text from ServiceNow (`description`, `short_description`, `work_notes`, `comments`, CI descriptions) is emitted as `output.Untrusted`.

### 5.1 Foundation
- **FR-001** Single static binary `snow`; `snow version` prints semver, commit, build date (ldflags; REL-4); exit 0.
- **FR-002** Config loader (strict YAML, `~/.config/snow/config.yaml` or `--config`/`SNOW_CONFIG`): profiles with `mode` (agent|human), `instance.host`, `instance.release`, `okta.*`, `daemon.socket`, `audit.path`, `policy.path`, `incident.states/categories/scale`; unknown keys rejected (exit 2).
- **FR-003** Global flags per PRD section 7; invalid flag values exit 2; `--trace` only in human mode (otherwise exit 6) and never prints tokens.
- **FR-004** Every command renders through `output.Write` with the core envelope; errors via `output.FromError`; exit code from the envelope.
- **FR-005** Every command: policy check (`policy.Engine.Check`) then audit record then action; denial exit 6; audit record per D-c.
- **FR-006** `internal/sn` client: pinned `/api/now/v1` paths, `sysparm_exclude_reference_link=true`, `sysparm_fields` always sent, display values only with `--display`, ordering default, status mapping per D-b, `X-Total-Count` parsing.

### 5.2 Identity and auth
- **FR-010** `snow whoami`: GET the scripted `/api/x_corp_agent/v1/whoami` (path configurable, A-02); returns user, display name, role names, instance, policy profile, mode. Unreachable endpoint -> exit per D-b.
- **FR-011** `snow auth login` (human mode): PKCE with loopback listener, state validation, scopes `openid profile email offline_access snow.user`, opens browser (injectable launcher; prints URL when launch fails).
- **FR-012** `snow auth login --device`: device authorization grant; prints verification URI and code; polls with the interval/`slow_down` rules; honours expiry.
- **FR-013** `snow auth status`: shows mode, issuer, subject, token expiry (never token values); exit 3 when not logged in.
- **FR-014** `snow auth logout`: revoke at Okta, delete keychain entry; partial failure reported.
- **FR-015** Silent refresh with rotation, atomic persist; refresh failure -> exit 3 with Okta error shown (AUTH-H5).
- **FR-016** Agent mode token source via daemon client (stub that fails closed, exit 3 naming the socket); single 401 refresh then exit 3; `auth login` refused on agent profiles.
- **FR-017** Token to ServiceNow: access token for agents; human token type is config `okta.token_type: access|id` (default access; M0 decides, A-05).

### 5.3 Table and CMDB reads
- **FR-020** `snow table get <table> <sys_id>`: allowlisted table only (policy), allowlisted fields only.
- **FR-021** `snow table list <table> --query <encoded> --fields --limit --offset`: encoded query pass-through validated (no `javascript:` / script), pagination per D-h, `acl_filtered_possible`.
- **FR-022** `snow table count <table> --query`: Aggregate API (`/api/now/v1/stats/{t}?sysparm_count=true`), policy verb `count`.
- **FR-023** `snow cmdb ci get <name|sys_id>`: resolve by sys_id (32 hex) else exact name; allowlisted fields; ambiguous name -> exit 9 with candidates.
- **FR-024** `snow cmdb ci search --class <c> --query`: class must be in `cmdb_ci` hierarchy per policy; pagination.
- **FR-025** `snow cmdb ci related <ci> [--direction up|down] [--depth N]`: `cmdb_rel_ci` via Table API, bounded depth (default 2, max 5), cycle-safe, node cap; P2 CMDB Instance API path not implemented (documented deferred).
- **FR-026** `snow cmdb app <name>`: resolves `cmdb_ci_service` / `cmdb_ci_appl`, owner, support group, related CIs.

### 5.4 Work item reads
- **FR-030** `snow my work [--kind incident|request|task|change]`: open `task` records assigned to the caller (resolved identity from `whoami`/config), default all kinds.
- **FR-031** `snow incident get <number|sys_id>` / `list` with `--mine --ci --app --group --state`.
- **FR-032** `snow request get|list`, **FR-033** `snow ritm get|list`.
- **FR-034** `snow task get|list` (sc_task; `update` is FR-045).
- **FR-035** `snow catalog search <text>`, `catalog get <item>`, `catalog vars <item>` via Service Catalog API.
- **FR-036** `snow change get|list` (read only; `change create` is P2 and not built: command absent, documented deferred).
- **FR-037** `snow problem get|list`.

### 5.5 Writes
- **FR-040** `snow incident create`: requires `--short-description --description --ci|--app --impact --urgency` (exit 9 otherwise); never writes `priority`; resolves CI support group into `assignment_group` unless overridden; default via record producer `submit_producer` (item sys_id/name in config), alternative Table API (config `incident.create_via: producer|table`); `--dry-run` prints the intended payload without sending; correlation dedupe and provenance (FR-043/044).
- **FR-041** `snow incident update <number>`: only policy-allowed fields; `sys_mod_count` guard; conflict exit 7.
- **FR-042** `snow incident resolve <number> --close-code --close-notes`: policy default deny for agents; human profile allows with confirmation.
- **FR-043** Idempotency per D-c: default key, `--idempotency-key`, deduplicated result in `data.deduplicated`.
- **FR-044** Provenance: work-note prefix `[snow-cli agent=<id> run=<run_id>]` and `correlation_display=agent:<id>` (A-04).
- **FR-045** `snow task update <number|sys_id>`: `work_notes`, `comments`, limited `state`, `assigned_to` only self; client-side check that the task is assigned to the identity.
- **FR-046** `snow catalog order <item> --var name=value`: validate variables via `catalog vars` first (exit 9 on missing mandatory/unknown); policy default `dry_run_only`; per-item opt-in.
- **FR-047** Human mode confirmation prompt on writes unless `--yes`; non-interactive without `--yes` -> exit 2.
- **FR-048** `--dry-run` on all writes: policy-allowed or dry_run_only decisions both preview; never sends a mutating request.

### 5.6 Selftest, docs, hardening
- **FR-050** `snow selftest [--profile] [--include-writes]` per D-k; exit 0 pass, 1 on failure (core semantics).
- **FR-051** Skill document generated by `docgen.Generate` from the command tree (`make skill`), including forbidden actions per command; CI drift check; the root `skills/snow-cli.md` is NOT edited: changes recorded in `docs/root-skill-update-needed.md`.
- **FR-052** Sample policy files `policies/agent.policy.yaml` and `policies/human.policy.yaml` parse with `policy.Parse` (D-f); a test also loads each through `policy.Load`.
- **FR-053** No command prints, logs or returns a token; no `token`/`print-token`, no raw passthrough command; tests grep stdout/stderr/trace/audit for planted token strings.
- **FR-054** CI workflow (`.github/workflows/ci.yml`) satisfying BLD-1..6 (gofmt, go mod tidy diff, vet, golangci-lint pinned, `go test -race`, govulncheck, cross-compile darwin/arm64 and linux/amd64+arm64, skill drift check). Release workflows are out of scope.

## 6. Non-Functional Requirements
- NFR-001 Local overhead under 50 ms (benchmark on the fake server).
- NFR-002 Output bounded: default `--max-bytes 32768`, free text marked untrusted.
- NFR-003 Coverage at least 90% on domain and use-case packages; `go test -race ./...`, gofmt, vet, golangci-lint v2 clean.
- NFR-004 Clean Architecture: domain/use-case packages import neither net/http, os keychain nor filesystem.
- NFR-005 Platforms: macOS arm64, Linux amd64/arm64 (WSL2 = Linux build); `CGO_ENABLED=0`.
- NFR-006 Security: tokens never in logs, `ps`, errors, audit, traces; policy file permission checks via core loader; no plaintext credential store without `--insecure-store`.

## 7. System Architecture
See `architecture.md`. Layers: domain (records, scale, ids, request/response value objects) <- usecase (command services, ports) <- adapters (sn HTTP client, config, keychain, daemon stub, Okta flows, policy mapping, audit wiring) <- cmd/cli. Composition root `cmd/snow/main.go` + `internal/app`.

## 8. Scope of Changes
New module `github.com/stainedhead/snow-cli` (go.mod `go 1.27`, require agent-cli-core v0.1.0); directories per plan.md section 2; `policies/`, `docs/`, `user-docs/`, `.github/workflows/ci.yml`. No change to core, the root skill, outlook-cli or other repos.

## 9. Breaking Changes
None (greenfield). Compatibility commitments: exit codes and envelope are the core's public contract; policy vocabulary (D-f) becomes a stable contract with the generated skill.

## 10. Milestone Acceptance (testable)

| Milestone | Testable acceptance (all against fakes/httptest; no live systems) |
|---|---|
| M1 Read path | `go test` suite: fake SN serves fixture tables; `table list` on an allowlisted table returns items, envelope valid; on a denied table exit 6 (client policy) and when the fake returns 403 exit 4; 404 -> 5; empty page with `X-Total-Count` > offset -> `acl_filtered_possible:true`; `--fields` outside allowlist -> exit 6; truncation sets `meta.truncated` and `meta.next_offset`; agent and human profile (fake token sources) both read; selftest read matrix passes against the fake and fails (exit 1) when the fake is configured to over-grant. |
| M2 Write path | Duplicate `incident create` with same key issues exactly one POST and returns `deduplicated:true`; denied field in `update` exit 6 with zero HTTP requests; priority never sent; impact=1 denied (D-d); injected 503 on POST with dedupe done retries and creates once, without dedupe mark makes one attempt; audit write failure (failing writer) blocks the write (zero requests for the pending record; command exit 1); audit records contain required fields and no bodies; `sys_mod_count` change -> exit 7; provenance prefix present in the PATCH body. |
| M3 Human mode | Against httptest Okta: PKCE flow validates state (wrong state -> failure), exchanges code with verifier, stores tokens in fake keychain; device flow handles `authorization_pending`, `slow_down`, `expired_token`; rotation persisted; refresh failure -> exit 3 showing Okta error; logout revokes and clears; keychain stub backends return the documented fail-closed error; `--insecure-store` writes 0600 file; human profile runs the same read/write commands via the same use cases. MFA enforcement is Okta-side and not testable (documented). |
| M4 Catalog | `catalog search/get/vars` against fixtures; `order` default dry_run_only (no POST); opted-in item orders with validated vars; order POST not marked safe-to-retry (A-07); `change get/list` read; `change create` absent (usage error exit 2). |
| M5 Hardening (partial) | Skill doc generated, drift check passes, forbidden actions listed; token-leak test passes; deferred items documented (redaction hook, signed policy, release signing). |
| M0 | Not executed; `docs/m0-spike-checklist.md` lists the spikes and the assumptions each confirms. |

## 11. Quality Gates
`gofmt -l .` empty; `go mod tidy` no diff; `go build ./... && go vet ./... && go test -race ./...`; `golangci-lint run`; coverage gate 90% on domain/usecase; policy files parse; docgen drift check.

## 12. Risks and Mitigation
| Risk | Mitigation |
|---|---|
| Unverified SN behaviours (section 13) | Isolated behind ports; assumption-named tests; M0 checklist; config switches (create_via, scale) |
| Core lacks needed features (shared write counter, absolute offsets, required-field policy) | Documented deviations; `docs/core-change-requests.md` |
| Policy semantics inversion (impact scale) | D-d constraint test |
| Retried non-idempotent POST creates duplicates | D-c rules and tests |
| Audit failure after successful write | pending + outcome record, explicit message |
| Daemon adapter unreleased | Fail-closed stub, ADR |

## 13. Assumptions (PRD unconfirmed items; each is an ASSUMPTION in code, an Assumption-named test, and a docs entry)

| ID | Assumption (PRD ref) |
|---|---|
| A-01 | Impact/urgency scale is OOB 1=High,2=Medium,3=Low; priority derived by instance rules; CLI never writes priority (7.1) |
| A-02 | Custom scripted `whoami` endpoint path and response shape (8.3 row 1) |
| A-03 | ACL-hidden records appear as 404 / empty pages; default table deny is enabled (8.1.4, 7.1) |
| A-04 | `correlation_id` / `correlation_display` exist on `task` in the target release (7.1) |
| A-05 | Human token to ServiceNow: access token vs id token undecided (6.2); default access token |
| A-06 | Okta loopback redirect: fixed ports 8765-8769 tried in order (6.2) |
| A-07 | A dedupe key retrievable after `order_now` exists (request/RITM correlation field); else order is never auto-retried |
| A-08 | Record producer `submit_producer` response shape (record sys_id/number) and its producer id in config (8.3 row 9) |
| A-09 | `sys_mod_count` available and increments per update (7.1) |
| A-10 | Journal fields (`work_notes`, `comments`) readable and writable per field ACLs (8.1.6) |
| A-11 | Aggregate API path/params for count (`/api/now/v1/stats/{table}`) (7, 8.3 row 3) |
| A-12 | CMDB Instance API / per-table API policy granularity not used (8.1.3, 8.3 row 6) |
| A-13 | WSL2 keychain availability; Linux Secret Service present (6.2) |
| A-14 | Rate-limit headers/`Retry-After` behaviour of inbound REST (8.4.6) |

## 14. Timeline and Milestones
M0 (checklist only) -> WS-A foundation -> parallel WS-B read, WS-C write, WS-D human auth, WS-E policy/selftest/docs/CI -> integration -> docs/user-docs -> review. See `plan.md`.

## 15. Deferred / Not Built
Redaction hook; signed policy; `change create`; CMDB Instance API; attachments; real daemon adapter; real OS keychain backends; release workflows; the scoped app. All listed in `docs/deferred.md`.

## 16. References
- Source PRD: `specs/261003-snow-cli/snow-cli-PRD.md`
- Core: agent-cli-core v0.1.0 README, `docs/technical-details.md`, `user-docs/`
