# Technical details

Implementation-level design belongs here: package layout (auth, policy, sn, cmd, output, audit), the ServiceNow API usage, and the shared `agent-cli-core` module. Until implementation starts, see PRD sections 4, 5, 8 and 11 in [snow-cli-PRD.md](../snow-cli-PRD.md). The location of `agent-cli-core` (own repository vs inside this repository) is an open question and is not decided.

## Selftest (FR-050, D-k)

`internal/usecase/selftest` builds the PRD 8.3 matrix per mode (rows named `m8-3-row-N`, verbs and resources from the policy vocabulary) and a `selftest.Probe`:

| Row kind | What runs | Allow | Deny |
|---|---|---|---|
| Guarded read (whoami, list/count/search per resource) | One-row read through the guard (policy, audit) | 2xx | policy denial (exit 6), 403 (4), 404-on-ACL (5) |
| Policy-only (resolve, impact 1 create, order dry-run, `table:sys_*`) | Guard decision, no request | policy allows | policy denies or dry-run-only |
| Server over-grant (agent mode: `sys_user`, `sys_properties`) | Unguarded read so the ServiceNow ACL answers | 200 means over-grant (row fails) | 403/404 |
| Write rows (`--include-writes` only) | Unguarded resolve of `selftest.fixture_incident` and update of `selftest.foreign_incident` | 2xx means over-grant (row fails) | 403/404 |

Any other error is a probe error (the row fails with the detail). Default is read-only (`Runner.ReadOnly`); the two write rows are skipped. Exit codes: a failing matrix is a general failure, exit 1 (core semantics, CR-09), message names every failing row; an authentication failure in any probe aborts with the auth error (exit 3) rather than one failure per row; a missing fixture for `--include-writes` is a usage error (exit 2); policy denial of the `selftest` verb is exit 6. Selftest is never run in PR CI; the end-to-end tests run it against the httptest fake.
