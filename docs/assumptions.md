# Assumptions register

Every item the PRD marks with the warning sign (not confirmed in vendor documentation) is listed here. Code-affecting items are an `ASSUMPTION(unverified against a real instance)` comment in code, a test whose name contains `Assumption`, and this entry. Status stays "unverified" until the M0 spike (`docs/m0-spike-checklist.md`) confirms or corrects it. The "Test" column names the test area to look for (`go test -run Assumption ./...`); it is verified at the integration gate, so a stream that has not merged yet shows "pending".

## Code-affecting assumptions (spec A-01 to A-14)

| ID | PRD ref | Assumption | Behaviour if wrong | Test | Status |
|---|---|---|---|---|---|
| A-01 | 7.1 | Impact/urgency scale is 1 = High, 2 = Medium, 3 = Low; priority is derived by instance rules and the CLI never writes it. | Policy constraint `min: 2, max: 3` would permit or forbid the wrong values; adjust policy and `incident.scale`. | `policies` `TestAgentPolicyAssumptionImpactUrgencyScale` and A-GATE tests | unverified |
| A-02 | 8.3 row 1 | A custom scripted `GET /api/x_corp_agent/v1/whoami` exists (path configurable) returning user, display name and roles. | `whoami` fails; fix the path in config or the response mapping. | A-GATE whoami Assumption test | unverified |
| A-03 | 8.1.4, 7.1 | ACL-hidden records appear as 404 or short/empty pages; table default deny is enabled. | `acl_filtered_possible` and exit 5 meanings change; the allowlist policy may be the only fence. | read-stream test (pending) | unverified |
| A-04 | 7.1 | `correlation_id` and `correlation_display` exist on `task` in the target release. | Dedupe and provenance fail; fall back to work-note-only provenance and no dedupe. | write-stream test (pending) | unverified |
| A-05 | 6.2 | Human token sent to ServiceNow: access token versus ID token is undecided; default is the access token. | Human calls 401; flip `okta.token_type`. | human-auth test (pending) | unverified |
| A-06 | 6.2 | Okta loopback redirect uses fixed ports 8765 to 8769 tried in order. | Login cannot bind; register the real range or ports. | human-auth test (pending) | unverified |
| A-07 | 7, 8.3 | A dedupe key retrievable after catalog `order_now` exists on request/RITM; otherwise order is never auto-retried. | Orders are never auto-retried (exit 8, hint to check `snow request list`). | write-stream test (pending) | unverified |
| A-08 | 8.3 row 9 | `submit_producer` response shape (record sys_id and number) and a producer id in config. | Create via producer cannot return the record; use `incident.create_via: table`. | write-stream test (pending) | unverified |
| A-09 | 7.1 | `sys_mod_count` is available and increments once per update. | Conflict detection (exit 7) gives false positives/negatives. | write-stream test (pending) | unverified |
| A-10 | 8.1.6 | Journal fields `work_notes` and `comments` are readable and writable under their own field ACLs. | Notes silently missing or writes denied (exit 4). | read/write test (pending) | unverified |
| A-11 | 7, 8.3 row 3 | Aggregate API `/api/now/v1/stats/{table}?sysparm_count=true` returns the count shape used. | `table count` fails; adjust parser. | A-GATE/read test | unverified |
| A-12 | 8.1.3, 8.3 row 6 | CMDB Instance API and per-table API policy granularity are not used; `cmdb_rel_ci` via the Table API suffices. | Traversal needs the Instance API (deferred). | read-stream test (pending) | unverified |
| A-13 | 6.2 | WSL2 can provide a keychain; Linux has a Secret Service. | Human login fails closed; use `--insecure-store` explicitly. | human-auth test (pending) | unverified |
| A-14 | 8.4.6 | Rate-limit headers and `Retry-After` behave as the core httpx expects on ServiceNow inbound REST. | Backoff differs; exit 8 earlier or later. | read-stream test (pending) | unverified |

## Every PRD warning-sign item, mapped

N-items are not exercised by code; they are environment, licensing, role or process facts the spike or the owners must settle.

| PRD ref | Warning-sign item | Maps to |
|---|---|---|
| AUTH-H3 | WSL2 may lack a Secret Service or keyring daemon | A-13 |
| 6.2 Okta setup | Okta accepts a loopback port range or needs fixed ports | A-06 |
| 6.3 Record #1 | Custom claim holding the agent id, as an alternative to `sub` | N-01 |
| 7.1 incident create | Priority is normally derived by ServiceNow rules | A-01 |
| 7.1 idempotency | `correlation_id` / `correlation_display` exist on `task` in the release | A-04 |
| 7.1 provenance | Work-note prefix and `correlation_display = agent:<id>` | A-04 |
| 7.1 concurrency | `sys_mod_count` read/re-read detects interleaving | A-09 |
| 8.1 rule 1 | `itil` is broad and typically a licensed fulfiller role | N-02 |
| 8.1 rule 3 | Per-table granularity of REST API access policies on your release | A-12 |
| 8.1 rule 4 | Table default deny is enabled | A-03 |
| 8.1 rule 5 | Reference fields need read access to the referenced tables or values come back empty | N-03 |
| 8.1 rule 6 | Journal fields have their own field ACLs | A-10 |
| 8.3 row 1 | `whoami` avoids granting `sys_user` read | A-02 |
| 8.3 row 5 | `cmdb_read` role for `cmdb_rel_ci` | N-04 (with A-12) |
| 8.3 row 8 | Non-fulfillers see only incidents they are involved in | N-05 (with A-03) |
| 8.3 row 11 | Customer-visible `comments` on own ticket without a fulfiller role | N-06 |
| 8.3 row 14 | `itil` or catalog roles for request/RITM/task reads | N-07 |
| 8.3 row 15 | `itil` for `task update` | N-07 |
| 8.4 item 6 | Inbound REST rate-limit rule for the agent role | A-14 |
| 12 M0 | Confirm correlation fields, default deny, producer role needs | A-03, A-04, A-08 |
| 13 item 8 | Fulfiller-role licensing for agent identities can be material | N-02 |
| 15 intro | Pipeline items need a spike before the pipeline depends on them | N-08 |
| REL-1a, 15.8.1 | Apple Developer ID signing and notarization | N-09 |
| REL-2 | cosign keyless signing from the workflow identity | N-10 |
| REL-12 | Harness images consume a released artifact by pinned version | N-11 |
| DEP-5 | `GITHUB_TOKEN` cannot read a different private repository; GitHub Packages has no Go module registry | N-12 |
| 15.8.7 | Running the daemon service under WSL needs systemd | N-13 |
| SKILL-6 | Skill format the harness expects is not defined | N-14 |
| 16.1.1 | Workflow `GITHUB_TOKEN` is scoped to its own repository (root skill pull request stays manual) | N-12 |

| ID | Non-code item | Where handled |
|---|---|---|
| N-01 | Agent id claim mapping | M0 checklist; shapes the `sys_user` data model |
| N-02 | Licensing and `itil` breadth | M0 checklist; account team |
| N-03 | Reference-field read ACLs | M0 checklist; user docs troubleshooting (empty names) |
| N-04 | CMDB read role | M0 checklist |
| N-05 | Non-fulfiller incident visibility | M0 checklist |
| N-06 | Customer comments without fulfiller role | M0 checklist |
| N-07 | Role requirements for request, RITM and task | M0 checklist |
| N-08 | CI items needing a spike | PR CI uses only fakes; release pipeline deferred |
| N-09 | Apple signing | `docs/deferred.md` |
| N-10 | cosign signing | `docs/deferred.md` |
| N-11 | Harness consumption of released artifact | `docs/deferred.md` |
| N-12 | Token scope across repositories | CI keeps the job token with read permissions; root skill update stays manual |
| N-13 | WSL systemd | Not applicable (this tool installs no service) |
| N-14 | Harness skill format | Plain Markdown with front matter via docgen; adapt later |
