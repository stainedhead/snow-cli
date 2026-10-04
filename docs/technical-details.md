# Technical details

Module `github.com/stainedhead/snow-cli`, Go 1.27, one direct dependency besides the shared core (`github.com/goccy/go-yaml` for the strict config). Requires `github.com/stainedhead/agent-cli-core v0.1.0` with no `replace` and no pseudo-version.

## Package layout (dependencies point inward)

| Package | Role |
|---|---|
| `cmd/snow` | Entry point; composition root call; ldflags-stamped `version`, `commit`, `date`. |
| `internal/cli` | Command router (stdlib `flag`, no CLI framework), global flags, command registration (`cmd_*.go`), rendering via core `output`, exit mapping, skill generation from the command tree. |
| `internal/app` | Composition root: loads config, loads policy, builds the token source, the `sn` client, the audit logger and the guard, then wires ports (`wire_*.go`, `human.go`). |
| `internal/usecase` (`read`, `write`, `selftest`, `whoami`) | Command services and the ports they need. No `net/http`, keychain or filesystem. Coverage gate 90%. |
| `internal/domain` | Records, identifiers, modes, impact/urgency scale. |
| `internal/sn` | ServiceNow HTTP adapter: pinned `/api/now/v1` paths, status to error-category mapping, Table, Aggregate, Service Catalog and CMDB relationship calls, writes, identity. |
| `internal/policymap` | Verb/resource vocabulary shared by use cases, policy files and the skill. |
| `internal/auditx` | Guard that runs policy check, audit (block for writes, warn for reads) and the action. |
| `internal/config` | Strict YAML config, host validation, defaults. |
| `internal/humanauth` | Okta PKCE and device flows, token refresh with rotation, credential stores (memory, 0600 file, fail-closed stubs). |
| `internal/agentauth` | Fail-closed daemon client (ADR-002). |
| `internal/idempotency`, `internal/provenance` | Default idempotency key; provenance work-note prefix and `correlation_display`. |
| `internal/repocheck` | Repository invariants as tests (`go.mod` shape, assumptions register vs tests). |
| `policies` | Embedded `agent.policy.yaml` and `human.policy.yaml`. |

Request flow: flags, then use case, then `policy.Engine.Check`, then a pending audit record (writes), then the `sn` client over the core `httpx` transport (`AllowedHosts: [instance.host]`, token attached by an `auth.Authorizer`, one refresh on 401), then error mapping, then `output.Write`.

## ServiceNow API usage

Table API (`/api/now/v1/table/...` with `sysparm_fields`, `sysparm_exclude_reference_link=true`, deterministic ordering, `X-Total-Count`), Aggregate API for counts (`/api/now/v1/stats/{table}?sysparm_count=true`), Service Catalog API (search, item, variables, `order_now`), record producer submit for incident create (or Table API via `incident.create_via: table`), and a custom scripted `whoami` endpoint. CI relationships use the Table API on `cmdb_rel_ci`; the CMDB Instance API is not used. All response shapes marked in `assumptions.md` are unverified against a real instance.

### HTTP status to exit code

401 refresh once then auth (3); 403 forbidden (4); 404 not found (5); 409/412 conflict (7); 400/422 validation (9, message from ServiceNow `error.message`, scrubbed and bounded); 429 and 5xx after the core's bounded retries rate limited (8); other 4xx general (1). Policy denial is 6.

### Retry safety

The core retries only idempotent methods unless a request is marked safe. Incident create POSTs are not marked safe at the transport (FR-R07): the adapter runs its own bounded loop (default 3 attempts, 200 ms doubling backoff) and re-runs the `correlation_id` dedupe lookup before each re-send; a hit returns `deduplicated: true`, a lookup failure stops with the original error. This is still not atomic across concurrent runs (check-then-create). Catalog order POSTs are never marked safe (A-07). PATCH is never marked safe (a re-sent applied PATCH would duplicate work notes); a 503 on PATCH exits 8.

### Conflicts (FR-R08)

The adapter reads `sys_mod_count` before the PATCH and re-reads it after. A caller-supplied `ExpectedModCount` (`--expected-mod-count N`) is a hard precondition: a mismatch sends no PATCH (exit 7, "not applied"). A post-write advance of more than one exits 7 with a message that the change WAS applied, names the record and says not to repeat it; the audit outcome is `applied_conflict`.

### Encoded queries (FR-R05)

`--query` is parsed, not pattern-matched: only `field OP value` clauses joined by `^` (and `^OR`) are accepted. Rejected with exit 9: `NQ`/`EQ`, `DYNAMIC`, `javascript`/`gs.` (any case or encoding), control characters, unknown operators, and values containing `^` or line breaks. Field names in clauses, in `--order-by` and in `count --query` are submitted to the policy as requested fields (allowlist denial is exit 6). ORDERBY is detected by clause position only.

### Idempotency (FR-R11)

An explicit `--idempotency-key` uses the charset `[A-Za-z0-9._:-]`, at most 64 characters; otherwise exit 9 before the guard (no audit, no request). The derived key checks the current and previous hour bucket. The dedupe lookup no longer filters on `active=true`, so explicit keys also match closed incidents (reuse returns the original). CI names and sys_ids give different keys (case and surrounding space are normalised, names and ids are not).

### Untrusted marking (FR-R12)

The rule is inverted: every string field is `untrusted` except a documented set of structured fields (sys_id, number, state codes, timestamps, class names, shape-checked references); the set is in `internal/usecase/read/present.go`. Display-name references and CI names are untrusted. `sys_updated_by`/`sys_created_by` stay plain only for lower-case login-id shapes. `read.NodeData`, `read.EdgeData` and `domain.CatalogVariable` wrap authored text through custom `MarshalJSON`; the catalog item `name` in `catalog vars` data is an `output.Untrusted` value. Author is `sys_updated_by` and timestamp `sys_updated_on` (UTC) when read.

### Read policy coverage (FR-R13)

Policy requests list every field fetched, including dot-walked relationship fields; `cmdb ci related` and `cmdb app` also check a `list` read on `table:cmdb_rel_ci` (the shipped agent rule `read-cmdb-related` lists `parent.name`, `child.name`, `parent.sys_class_name`, `child.sys_class_name`, `type.name`). An unclassified 5xx is a `ServerError` (exit 8, "ServiceNow server error (HTTP n)"); 429/502/503/504 stay `RateLimitedError`; bodies over 8 MiB fail with "response too large" (exit 1).

### Audit records (FR-R10, FR-R04)

`resource` is `<base>:<ref>`; `outcome` may be `pending`, `ok`, `error`, `denied`, `dry_run` or `applied_conflict`; selftest probes use verbs `selftest:probe-*` with `policy_decision: probe_bypass`. See ADR-014 and ADR-015.

### Read-path decisions

Default page size 25 (`read.DefaultLimit`), clamped by `limits.max_results`; `--order-by` always gets an `^ORDERBYsys_id` tiebreak; `--mine` filters `assigned_to.user_name=<whoami user>` (`requested_for` for requests); `cmdb ci related` is one policy check with depth 2 (max 5), a 200-node cap and cycle safety, `down` meaning the CI is the parent; `cmdb ci search --class` accepts `cmdb_ci*` names only; the read path is mode-agnostic (only the token source differs).

### Write-path decisions

Policy requests carry only caller-chosen fields; system-added `correlation_id`, `correlation_display` and the provenance work note are not subject to the caller's allowlist. `--ci` and `--app` both map to `cmdb_ci`. Dry-run of `incident create` sends no request; a catalog order dry-run still performs read-only catalog lookups. Confirmation (FR-047) runs inside the guarded action after policy allow and the pending record; a declined prompt is an error outcome (exit 1); non-interactive human mode without `--yes` exits 2. Unassigned tasks may be claimed; any other task not assigned to the caller is refused (exit 6).

### Human auth decisions

`--insecure-store` is a flag on `auth login|status|logout` and affects only that command; other commands read the same 0600 file only with `SNOW_INSECURE_STORE=1` (default `~/.config/snow/credentials.json`). Real OS keychain backends are fail-closed stubs (exit 3). `auth status|logout` are refused in agent mode (exit 6). A refresh failure from Okta is a login-required error (exit 3) showing the Okta code; credentials are kept; rotated refresh tokens are persisted atomically. Okta endpoints are `<issuer>/v1/authorize|token|revoke|device/authorize` (unverified); token lifetime defaults to 3600 s when `expires_in` is absent.

### Build notes

`policies.Named` is passed by `cmd/snow/main.go`; CI cross-builds with `CGO_ENABLED=0` for every target (NFR-005); `make skill-check` fails on skill drift.

## Output and bounds

Core `output.Meta` is fixed, so extras (`acl_filtered_possible`, `deduplicated`, `page`, `dry_run`) live in `data` (ADR-007). `snow` trims list `items` to `--max-bytes` itself because the core does not truncate arrays nested in an object (CR-04).

## Build and quality gates

`make fmt vet lint test race build cover skill skill-check`. Gates: `gofmt -l .` empty, `go vet ./...`, `golangci-lint run` (v2 config), `go test -race ./...`, 90% coverage on domain and use-case packages. CI is `.github/workflows/ci.yml` (PR checks only; no release workflows). The generated skill is pinned by `internal/cli/testdata/skill.golden.md`.

## Selftest (FR-050, D-k)

`internal/usecase/selftest` builds the PRD 8.3 matrix per mode (rows named `m8-3-row-N`, verbs and resources from the policy vocabulary) and a `selftest.Probe`:

| Row kind | What runs | Allow | Deny |
|---|---|---|---|
| Guarded read (whoami, list/count/search per resource) | One-row read through the guard (policy, audit) | 2xx | policy denial (exit 6), 403 (4), 404-on-ACL (5) |
| Policy-only (resolve, impact 1 create, order dry-run, `table:sys_*`) | Guard decision, no request | policy allows | policy denies or dry-run-only |
| Server over-grant (agent mode: `sys_user`, `sys_properties`) | Unguarded read so the ServiceNow ACL answers | 200 means over-grant (row fails) | 403/404 |
| Write rows (`--include-writes` only) | Unguarded resolve of `selftest.fixture_incident` and update of `selftest.foreign_incident` | 2xx means over-grant (row fails) | 403/404 |

Any other error is a probe error (the row fails with the detail). Default is read-only (`Runner.ReadOnly`); the two write rows are skipped. Exit codes: a failing matrix is a general failure, exit 1 (core semantics, CR-09), message names every failing row; an authentication failure in any probe aborts with the auth error (exit 3) rather than one failure per row; a missing fixture for `--include-writes` is a usage error (exit 2); policy denial of the `selftest` verb is exit 6. Selftest is never run in PR CI; the end-to-end tests run it against the httptest fake.
