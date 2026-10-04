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

The core retries only idempotent methods unless a request is marked safe. `incident create` POSTs are marked safe only after the `correlation_id` dedupe query ran in the same invocation and found nothing. Catalog order POSTs are never marked safe (A-07). PATCH updates rely on the `sys_mod_count` guard.

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
