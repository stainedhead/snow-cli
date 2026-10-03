# snow CLI — Product Requirements Document

| | |
|---|---|
| **Status** | Draft v0.1 |
| **Date** | 2026-10-03 |
| **Owner** | Enterprise Architecture (owner TBD) |
| **Companion docs** | `agent-okta-d-PRD.md` (credential daemon), `outlook-cli-PRD.md`, `teams-cli-PRD.md` (both reuse the shared core defined in §5) |
| **Binary** | `snow` |

**Evidence legend.** ✅ = confirmed against vendor documentation during research (2026-10-03). ⚠️ = not confirmed in vendor docs this session (community source or engineering judgment). Validate every ⚠️ item in a sub-production instance before depending on it.

---

## 1. Summary

`snow` is a purpose-built CLI for ServiceNow work that autonomous SDLC agents and human teammates both need: read tables, look up CMDB configuration items (CIs), and read, create and update work items (incidents, requests, catalog tasks), including opening an outage ticket. There is no suitable stock CLI for this (ServiceNow's own `now-sdk` targets application development), so we build a thin one.

Two properties define it:

1. **Narrow verbs, no raw API passthrough.** Agents get task-shaped commands, not "call any REST endpoint". This keeps tool use reliable and gives us a place for client-side guardrails.
2. **Two authentication modes on one command surface.** *Agent mode* takes a short-lived Okta token from `agent-okta-d`. *Human mode* signs in with Okta OAuth 2.0 (authorization code + PKCE). In both cases ServiceNow validates the Okta token and applies the roles and ACLs of the mapped `sys_user`.

**The ServiceNow roles and ACLs are the security boundary. The CLI's policy engine is a guardrail and a usability layer, never the control.**

## 2. Goals and non-goals

**Goals**

- G1. Agents can complete the SDLC-team ITSM tasks in §6 without any ServiceNow secret on the host.
- G2. Humans can run the same commands as themselves (debugging, sharing workload with agents).
- G3. Every write is attributable to one identity and idempotent on retry.
- G4. Output is safe to feed to an LLM: bounded size, structured, free text clearly marked untrusted.
- G5. ServiceNow-side permissions are minimal, explicit and testable (§8, `snow selftest`).

**Non-goals**

- No raw REST passthrough (`snow raw`/`snow api`). Not provided in any mode.
- No CMDB writes in v1 (CI create/update goes through ServiceNow's Identification and Reconciliation engine and is a different risk class).
- No approvals, no deleting records, no user/group/role administration, no scripts or flows.
- No ServiceNow-native MCP server use (see Concerns, §13).

**Working assumption A1.** "Service items" in the request is interpreted as ServiceNow **work items**: incidents (read/create/update), requests/RITMs/catalog tasks (read, update assigned work), catalog items (search and order), change requests (read; draft creation later) and problems (read), plus CMDB CI lookup. Confirm in §14.

## 3. Users and modes

| | **Agent mode** | **Human mode** |
|---|---|---|
| Identity | Agent's Okta service app → mapped `sys_user` (e.g. `agent.reviewer-01`) | The person's own Okta identity → their own `sys_user` |
| Token source | `agent-okta-d` unix socket (`pkg/client`) | `snow auth login` (PKCE) with refresh tokens in the OS keychain |
| Okta authorization server | `agents-snow` (aud `snow-agents`) | `humans-snow` (aud `snow-humans`) |
| ServiceNow OIDC provider record | #1 (maps `sub`/agent claim → `sys_user`) | #2 (maps `email`/`preferred_username` → `sys_user`) |
| ServiceNow roles | Dedicated agent roles (§8) | Whatever the person already has |
| Default client-side policy | `agent` (strict) | `human` (permissive, confirmation prompts) |

Mode is chosen by config/profile, not by flag, so an agent cannot "switch" to human mode: agent hosts have no human login configured and `snow auth login` is disabled there by policy.

**Debugging as an agent.** A human cannot obtain an agent's token (the daemon socket is restricted to the agent OS user). To reproduce agent behavior, run `snow --policy agent …` as yourself (client-side restrictions only) and use ServiceNow's security debugging and user impersonation in a sub-production instance for server-side behavior.

## 4. Architecture

```
 snow (Go binary)
 ├─ auth/        TokenSource: daemon client (agent) | OAuth PKCE + keychain (human)
 ├─ policy/      client-side guardrails (shared core, §5)
 ├─ sn/          ServiceNow client: Table API, CMDB, Service Catalog, Aggregate, whoami
 ├─ cmd/         task-shaped commands (§6)
 ├─ output/      envelope, truncation, untrusted-content marking (shared core)
 └─ audit/       JSONL audit log (shared core)
        │ Authorization: Bearer <Okta token>
        ▼
 ServiceNow instance
   OIDC provider record (validates Okta JWT, maps claim → sys_user)  ✅
   API access policies / REST API auth scope  ✅
   Roles + ACLs on tables and fields  ✅
```

APIs used: Table API (`/api/now/table/{table}`), Service Catalog API (`/api/sn_sc/servicecatalog/…`), CMDB Instance API (optional, §8 caveat), Aggregate API (counts), plus one small custom scripted endpoint for `whoami` (§8.3). Pin API versions in URLs (`/api/now/v1/…`) ✅.

## 5. Shared CLI core (applies to `snow`, `outlook`, `teams`)

All three agent-facing CLIs are separate binaries (so harness allow-lists and permission prompts can key on command name) built from one Go module, `agent-cli-core`:

| Package | Responsibility |
|---|---|
| `auth` | `TokenSource` interface; daemon client (`pkg/client` from `agent-okta-d`); single retry on 401 after forcing a refresh; **never prints or logs tokens**; no `token`/`print-token` command |
| `policy` | YAML policy engine: allow/deny per verb and resource, field allowlists, value constraints, rate limits, write modes (`allow` \| `dry_run_only` \| `deny`), max-results caps. Policy file lives in a root-owned directory the agent user can read but not write; optional detached-signature check |
| `output` | Response envelope, size truncation, **untrusted-content marking**, stable exit codes |
| `audit` | JSONL audit log (`ts, tool, agent_id, run_id, verb, resource, outcome, http_status, duration, policy_decision`), no secrets, no free-text bodies by default |
| `httpx` | Retries with jitter, 429/`Retry-After` handling, request tracing with header/body redaction |
| `selftest` | Expected-allow/deny matrix runner against the live server (§10) |
| `docgen` | Generates the harness skill document (`SKILL.md`) from the command tree: usage, examples, forbidden actions |

**Output envelope**

```json
{ "ok": true,
  "data": { "...": "..." },
  "meta": { "truncated": false, "next_offset": null, "count": 12, "request_id": "..." } }
```
```json
{ "ok": false,
  "error": { "code": "policy_denied", "message": "closing incidents is not allowed for agents", "hint": "ask a human to resolve" } }
```

**Exit codes:** `0` ok · `1` general · `2` usage · `3` auth · `4` forbidden by server (403/ACL) · `5` not found · `6` denied by client policy · `7` conflict/precondition · `8` rate-limited/transient · `9` validation (mandatory field).

**Untrusted content.** Free text written by other people (ticket descriptions, comments, work notes, email bodies, chat messages) can contain instructions aimed at the model. The core marks such fields `"untrusted": true` in JSON and, in text output, wraps them in explicit delimiters with the author and timestamp. The generated skill document tells the agent that marked content is data, never instructions. This is a mitigation, not a guarantee; server-side limits remain the real control.

**Output bounds.** Default `--max-bytes 32768`; truncated outputs set `meta.truncated` and `next_offset`.

## 6. Authentication requirements

### 6.1 Agent mode

| ID | Requirement | Pri |
|---|---|---|
| AUTH-A1 | Obtain the token from the daemon via `pkg/client` (`servicenow` provider); send `Authorization: Bearer`. | P0 |
| AUTH-A2 | On HTTP 401, ask the daemon to refresh once and retry; on a second 401 exit `3`. | P0 |
| AUTH-A3 | Refuse to run if a daemon socket is not reachable (no fallback credentials). | P0 |
| AUTH-A4 | `snow whoami` shows the mapped ServiceNow user, roles summary and policy profile (§8.3). | P0 |

### 6.2 Human mode (Okta OAuth 2.0)

| ID | Requirement | Pri |
|---|---|---|
| AUTH-H1 | `snow auth login`: authorization code flow with **PKCE** and a **loopback redirect** (RFC 8252). The CLI opens the browser, listens on `127.0.0.1` on a free port, validates `state`, exchanges the code ✅ (pattern widely used; Okta recommends PKCE for all code flows ✅). | P0 |
| AUTH-H2 | `snow auth login --device`: device authorization grant for SSH/headless/cloud IDE sessions ✅ (PKCE cannot redirect there). Prefer PKCE by default; device-code flows are easier to phish. | P0 |
| AUTH-H3 | Scopes: `openid profile email offline_access snow.user`. Store access and refresh tokens in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service); no plaintext fallback unless `--insecure-store` is passed. | P0 |
| AUTH-H4 | Silent refresh using the refresh token; honor Okta refresh-token rotation; `snow auth status`, `snow auth logout` (revokes at Okta). | P0 |
| AUTH-H5 | Respect Okta policies (MFA, session lifetime, device posture) and show the Okta error when blocked. | P0 |
| AUTH-H6 | Work on macOS, Linux and Windows (human users commonly use Windows). | P0 |

**Which token goes to ServiceNow.** ServiceNow's third-party token flow accepts an ID token or access token ✅. Community guidance for Okta stresses using the `id_token` and a matching user mapping ✅. Spike (M0) both and standardize on one for humans; for agents it is always the access token from `agents-snow`.

**Okta setup for humans:** a *native* OIDC app (public client, PKCE required, refresh tokens with rotation, device authorization grant enabled), assigned to the `humans-snow` custom authorization server (aud `snow-humans`, scope `snow.user`) and to a group such as `snow-cli-users`. Loopback redirect URIs: confirm whether Okta accepts a port range or needs fixed ports ⚠️; if fixed, register a small set (e.g. 8765–8769) and have the CLI try them in order.

### 6.3 ServiceNow-side identity mapping

- Two OIDC provider records, one per audience, because each record needs a unique Client ID equal to the token's `aud` ✅ and carries one claim→field mapping ✅:
  - Record #1 (`snow-agents`): User Claim `sub` (Okta client ID for client-credentials tokens ✅) → a field on the agent's `sys_user` (e.g. `user_name` = Okta client ID), **or** a custom claim holding the agent id ⚠️.
  - Record #2 (`snow-humans`): User Claim `email` (or `preferred_username`) → `sys_user.email` (or `user_name`).
- Disable **JTI claim verification** on record #1 so a cached token can be reused within its short TTL; ServiceNow's own documentation notes that enabling it forces a fresh token per request ✅. Compensate with the 10-minute TTL and `sys_user` deactivation as the immediate cut-off.
- Agent `sys_user` records: "web service access only" (no interactive login), no password, named for the agent, with owner/sponsor recorded (description or custom field).
- Do **not** enable runtime auto-provisioning of users from tokens for the agent audience.

## 7. Command surface

Global flags: `--format json|table|text` (default `json` in agent mode, `table` for humans), `--fields`, `--limit`, `--max-bytes`, `--dry-run`, `--idempotency-key`, `--profile`, `--policy`, `--trace` (human mode only; prints method, URL, status, timing, never tokens).

| Command | Purpose | API | W |
|---|---|---|---|
| `snow whoami` | Mapped user, roles, instance, policy profile | custom scripted endpoint (§8.3) | |
| `snow auth login\|logout\|status` | Human sign-in (disabled by policy on agent hosts) | Okta | |
| `snow table get <table> <sys_id>` | Read one record from an **allowlisted** table | Table API | |
| `snow table list <table> --query <encoded> --fields … --limit N` | Query an allowlisted table (encoded query, pagination) | Table API | |
| `snow table count <table> --query …` | Count matching records | Aggregate API | |
| `snow cmdb ci get <name\|sys_id>` | CI details (allowlisted fields) | Table API on `cmdb_ci` hierarchy | |
| `snow cmdb ci search --class <c> --query …` | Find CIs by class and attributes | Table API | |
| `snow cmdb ci related <ci> [--direction up\|down] [--depth N]` | Relationships (dependencies/impact) | `cmdb_rel_ci` via Table API (P1); CMDB Instance API (P2, see §8.2) | |
| `snow cmdb app <name>` | Resolve an application/business service and its CIs, owners and support group | `cmdb_ci_service`, `cmdb_ci_appl`, relationships | |
| `snow my work [--kind incident\|request\|task\|change]` | Open work assigned to me (agent or human) | Table API on `task` | |
| `snow incident get\|list` | Read incidents (`--mine`, `--ci`, `--app`, `--group`, `--state`) | Table API | |
| `snow incident create` | Open an incident (outage, defect) | Record producer (default) or Table API | ✔ |
| `snow incident update <number>` | Add notes; correct allowed fields | Table API | ✔ |
| `snow incident resolve <number>` | Resolve with code and notes (**off for agents by default**) | Table API | ✔ |
| `snow request get\|list`, `snow ritm get\|list` | Read requests and request items | Table API | |
| `snow task get\|list\|update` | Catalog tasks (`sc_task`): read, add notes, limited field/state updates on assigned tasks | Table API | ✔ |
| `snow catalog search\|get\|vars` | Find catalog items and their variables | Service Catalog API | |
| `snow catalog order <item>` | Order a catalog item (creates request/RITM) | `order_now` ✅ | ✔ |
| `snow change get\|list` | Read changes (P1); `change create` for **draft** changes (P2); no approval or state advance, ever | Table API | (P2) |
| `snow problem get\|list` | Read problems | Table API | |
| `snow selftest` | Run the allow/deny matrix for the current identity (§10) | all of the above | |

Not provided: delete, approve/reject, impersonate, script execution, import sets, user/group/role administration, attachment upload (P2, policy-gated).

### 7.1 Command behavior highlights

- **List/get always use `sysparm_fields` and `sysparm_exclude_reference_link=true`**; display values only when asked (`--display`). Pagination uses `sysparm_limit`/`sysparm_offset` and the total-count header ✅ pattern. **Gotcha:** the Table API applies the record limit *before* ACL evaluation, so a page can legitimately come back empty even though accessible records exist ✅. The CLI therefore always combines tight encoded queries with explicit ordering, and returns `meta.acl_filtered_possible: true` when a page is empty or short while more pages exist.
- **`incident create`** requires `--short-description`, `--description`, `--ci` (or `--app`), `--impact`, `--urgency`. It sets `impact` and `urgency` (priority is normally derived by ServiceNow rules ⚠️, so the CLI never writes it). It resolves the CI's support group into `assignment_group` unless overridden. Incident `state` values and category lists are instance-specific and come from config.
- **Idempotency:** `--idempotency-key` (default: hash of agent id + CI + short description + hour bucket) is stored in `correlation_id`; before creating, the CLI queries for an active record with that key and returns it with `"deduplicated": true` instead of creating a second one ⚠️ (confirm `correlation_id`/`correlation_display` exist on `task` in your release).
- **Provenance:** every write adds a work-note prefix `[snow-cli agent=<id> run=<run_id>]` and sets `correlation_display` to `agent:<id>` ⚠️.
- **`incident update`** accepts only policy-allowed fields (e.g. `work_notes`, `comments`, `short_description`, `description`, `cmdb_ci`, `business_service`, `impact`, `urgency`, `assignment_group`) and only on records where the identity is caller, opener, assignee or a member of an allowed group (enforced by ACL and checked client-side).
- **Optimistic concurrency:** the Table API has no ETag; the CLI reads `sys_mod_count` before patching and re-reads after to detect interleaving ⚠️, and reports `conflict` (exit 7) if it changed.
- **`catalog order`** supports `--var name=value` and validates against `catalog vars` first.

## 8. ServiceNow permissions (the part to hand to your ServiceNow platform team)

### 8.1 Design rules

1. **Dedicated roles and ACLs, not blanket `itil`.** `itil` is the common baseline for incident work (community reports that work notes and state updates over REST fail without it ✅), but it is broad and typically a licensed fulfiller role ⚠️. Create custom roles and grant exactly the table/field operations below. Where a role requirement is stated for an out-of-box (OOB) role, it is the fallback if you accept the breadth.
2. **Package everything as one scoped application** (e.g. `x_corp_agent`) containing roles, ACLs, API access policy, the `whoami` endpoint and the record producer; deploy it via source control / update sets so permissions are code-reviewed. The ServiceNow SDK's Fluent API can define roles and catalog objects as code ✅ (confirm fit with your tooling).
3. **Restrict the API surface** with REST API access policies and OAuth/REST API auth scopes so the agent identity can only reach: Table API on the listed tables, Service Catalog API, Aggregate API, the `whoami` endpoint, and (optionally) the CMDB Instance API ✅ (policies can restrict by role, group, IP or scope ✅). Confirm per-table granularity on your release ⚠️.
4. **Default deny.** Confirm table default-deny is enabled so tables without an ACL are inaccessible ⚠️.
5. **Reference fields need read access too.** Showing a caller's or group's name (display values, dot-walking) needs read access to the referenced tables (`sys_user`, `sys_user_group`, `location`, …), otherwise values come back empty ⚠️. Grant field-limited read on those.
6. **Journal fields** (`work_notes`, `comments`) are separate from the record and have their own field ACLs; plan for them explicitly ⚠️.

### 8.2 Proposed roles

| Role | Grants |
|---|---|
| `x_corp_agent.base` | Authenticate via OIDC provider; call allowed API endpoints; `whoami` |
| `x_corp_agent.cmdb_reader` | Read `cmdb_ci` hierarchy (selected fields), `cmdb_rel_ci`, `cmdb_ci_service`, `cmdb_ci_appl`, relationship tables, `sys_user_group` (name/manager only) |
| `x_corp_agent.work_reader` | Read `task`, `incident`, `problem`, `change_request`, `sc_request`, `sc_req_item`, `sc_task`, `task_ci`; read journal fields on those records |
| `x_corp_agent.incident_writer` | Create via the record producer; update allowed fields/journals on incidents the identity opened, is assigned, or whose group it belongs to |
| `x_corp_agent.task_updater` | Update allowed fields/journals on `sc_task`/`sc_req_item` assigned to the identity |
| `x_corp_agent.catalog_requester` | Browse and order catalog items the agent is eligible for (item user criteria) |
| `x_corp_agent.change_drafter` (P2) | Create **draft** changes only |
| `x_corp_agent.resolver` (**not granted by default**) | Resolve/close incidents the identity is assigned to |

**Never grant to agent identities:** `admin`, `security_admin`, `itil_admin`, `impersonator`, any `*_admin` CMDB role, approver roles, import/transform roles, or delete ACLs.

### 8.3 Action → permission matrix

Confidence: ✅ documented, ⚠️ verify in sub-prod (use ServiceNow's security rule debugging to find the exact ACLs each call hits).

| # | CLI action | API call | Tables / fields | Minimum ServiceNow permission | OOB fallback | Notes |
|---|---|---|---|---|---|---|
| 1 | `whoami` | `GET /api/x_corp_agent/v1/whoami` (custom scripted REST) | session user, roles | `x_corp_agent.base` on the endpoint ACL | custom only | Returns `gs.getUserName()`, display name, role names, instance. Avoids granting `sys_user` read for identity checks ⚠️ |
| 2 | `table get/list` | `GET /api/now/table/{t}` | allowlisted tables only | read ACL on table + each requested field; API access policy allows Table API for that table | `rest_api_explorer` is cited by integration vendors as a minimal REST role, with ACLs adding rights ✅ | Only tables in policy allowlist |
| 3 | `table count` | `GET /api/now/stats/{t}` | same | read ACL; Aggregate API allowed by policy | | |
| 4 | `cmdb ci get/search` | `GET /api/now/table/cmdb_ci_*` | `cmdb_ci` + child classes | `x_corp_agent.cmdb_reader`; no class-specific ACL needed because the parent `cmdb_ci` ACL applies to child tables ✅ | `cmdb_read` ✅ | |
| 5 | `cmdb ci related` (P1) | `GET /api/now/table/cmdb_rel_ci` | `cmdb_rel_ci` (+ target CI names) | read ACL on `cmdb_rel_ci` and referenced CIs | `cmdb_read` ⚠️ | Multi-level traversal is done client-side (bounded depth) |
| 6 | `cmdb ci related` (P2) | `GET /api/now/cmdb/instance/{class}/{sys_id}` | CI + inbound/outbound relations in one call | **The CMDB Instance API requires the `itil` role** per ServiceNow support through the Zurich release, with granular roles planned ✅ | `itil` | Prefer #5 unless you accept `itil` or your release has the granular role |
| 7 | `cmdb app` | Table API on `cmdb_ci_service`, `cmdb_ci_appl`, `svc_ci_assoc` | service/app CIs, support group, owner | `cmdb_reader` | `cmdb_read` | |
| 8 | `incident get/list`, `my work` | `GET table/incident`, `table/task` | incident/task + journals | `work_reader` (read ACL scoped by group/CI/assignee) | `itil` for fulfiller-wide reads ✅ | Out-of-box, non-fulfillers see only incidents they are involved in ⚠️ |
| 9 | `incident create` (default) | `POST /api/sn_sc/servicecatalog/items/{sys_id}/submit_producer` ✅ | creates `incident` via a **record producer** "Report outage (agent)" | `catalog_requester` + item availability for the agent's user criteria | none needed beyond the item | Record producers create a record directly in the target table ✅. The producer script enforces mandatory fields, defaults, `correlation_id`, assignment group, and blocks P1. Avoids giving agents `itil` create rights |
| 10 | `incident create` (alt) | `POST /api/now/table/incident` | `incident` create | create ACL (field-limited) in `incident_writer` | `itil` | Use only if no producer is available |
| 11 | `incident update` (notes) | `PATCH /api/now/table/incident/{sys_id}` | `work_notes`, `comments` | write ACL on those journal fields in `incident_writer` | `itil` (needed for work notes/state via REST ✅) | Customer-visible `comments` on one's own ticket may be possible without a fulfiller role ⚠️ |
| 12 | `incident update` (fields) | same | `short_description`, `description`, `cmdb_ci`, `business_service`, `impact`, `urgency`, `assignment_group` | field-level write ACLs in `incident_writer` | `itil` | State changes beyond what policy allows are denied |
| 13 | `incident resolve` | same | `state`, `close_code`, `close_notes` | `resolver` role (off by default) | `itil` | Enabled for humans; for agents only if risk accepts |
| 14 | `request/ritm/task get/list` | Table API | `sc_request`, `sc_req_item`, `sc_task` | `work_reader` | `itil`/catalog roles ⚠️ | |
| 15 | `task update` | `PATCH table/sc_task/{sys_id}` | `work_notes`, `comments`, limited `state`, `assigned_to` (self only) | `task_updater` write ACLs, restricted to tasks assigned to the identity | `itil` ⚠️ | |
| 16 | `catalog search/get/vars` | `GET /api/sn_sc/servicecatalog/items?sysparm_text=…`, `…/items/{id}`, `…/items/{id}/variables` ✅ | catalog items | `catalog_requester`; items must be available to the user (user criteria) | | Service Catalog API namespace must be in the API access policy |
| 17 | `catalog order` | `POST /api/sn_sc/servicecatalog/items/{id}/order_now` ✅ | creates `sc_request` + `sc_req_item` ✅ | `catalog_requester` | | `order_now` creates a formal request; `submit_producer` creates a record directly ✅ |
| 18 | `change get/list` | Table API | `change_request`, `change_task` | `work_reader` | `itil` | |
| 19 | `change create` (P2) | via record producer | draft `change_request` | `change_drafter` | `itil` + change roles | Never grant approval or state-advance rights |
| 20 | `problem get/list` | Table API | `problem` | `work_reader` | `itil` | |

### 8.4 Required ServiceNow configuration checklist

1. OIDC provider records #1 and #2 (Machine Identity Console → Inbound integrations → *Third party ID token issued by OIDC supporting IdP*, or the `oidc_provider_configuration` table): metadata URL of the Okta authorization server, Client ID = token `aud`, user claim → user field, JTI verification off for agents ✅.
2. Agent `sys_user` records (web-service-only) mapped as in §6.3, with the roles in §8.2.
3. The scoped app: roles, ACLs (table- and field-level), `whoami` endpoint, record producer(s).
4. REST API access policy / auth scope for the agent identities (§8.1 rule 3).
5. Audit: confirm inbound REST calls appear in transaction logs per user ✅ and forward to your SIEM.
6. Rate limits: set an inbound REST rate-limit rule for the agent role ⚠️.

## 9. Client-side policy (guardrail layer)

```yaml
# /etc/agent-cli/snow.policy.yaml  (root-owned; agent user read-only)
profile: agent
tables:
  allow:
    incident:    { verbs: [get, list], fields: [number, short_description, description, state, impact, urgency, priority, cmdb_ci, business_service, assignment_group, assigned_to, caller_id, opened_at, sys_updated_on, work_notes, comments] }
    sc_task:     { verbs: [get, list, update], update_fields: [work_notes, comments, state] }
    sc_req_item: { verbs: [get, list] }
    cmdb_ci:     { verbs: [get, list], include_children: true }
    cmdb_rel_ci: { verbs: [list] }
  deny: [ "sys_*", "sys_user", "sys_user_has_role", "sysapproval_approver", "sys_properties" ]
incident:
  create:
    mode: allow                     # allow | dry_run_only | deny
    require: [short_description, description, cmdb_ci, impact, urgency]
    max_impact_urgency: 2           # i.e. nothing above "High"; a human must raise P1
    max_per_hour: 5
  update:
    allow_fields: [work_notes, comments, short_description, description, cmdb_ci, business_service, impact, urgency, assignment_group]
  resolve: { mode: deny }
catalog:
  order: { mode: dry_run_only, allow_items: [] }   # opt in per item
limits:
  max_results: 200
  max_bytes: 32768
  max_writes_per_run: 10
audit: { path: /var/log/agent-cli/snow.audit.jsonl }
```

Human profile keeps the same shape with broader tables, `resolve: allow`, and interactive confirmation for writes (`--yes` to skip).

## 10. Testing and validation

- **`snow selftest [--profile agent]`** runs a built-in matrix of expected allows and denies against the live server for the current identity (read each allowlisted table; attempt each forbidden table and verb; confirm `resolve` is denied; confirm cross-record update is denied). This is the acceptance test the ServiceNow platform team runs after deploying the scoped app, and again after every role change.
- Unit tests with recorded fixtures (query building, field filtering, pagination, ACL-short-page detection, idempotency, redaction).
- Integration against a sub-production instance with both identity types; negative tests for every row of §8.3.
- Security tests: no token in logs, `ps` or error output; policy file not writable by the agent user; untrusted-content marking present on all free-text fields.
- Load/limits: 429 and `Retry-After` handling; respect inbound rate limits.

## 11. Non-functional requirements

- **Stack:** Go (static binary; shares `agent-cli-core` and `pkg/client` with the daemon). Platforms: macOS, Linux; Windows for human mode.
- **Latency:** local overhead < 50 ms; typical command dominated by ServiceNow response time.
- **Data handling:** ServiceNow records can contain personal or confidential data, and agent output flows into the model's context. Apply your data-classification rules through the field allowlist (§9) and an optional redaction hook (regex/field masks) before output.
- **Observability:** audit JSONL per command; correlate with ServiceNow transaction logs by user and timestamp.
- **Compatibility:** ServiceNow family release recorded in config; fail with a clear message if a required API is absent.

## 12. Delivery plan and acceptance

| Milestone | Scope | Acceptance |
|---|---|---|
| **M0 Spikes** | OIDC provider records #1/#2 on a sub-prod instance; agent token accepted; human token (ID vs access) decision; user mapping; `whoami`; confirm ⚠️ items (correlation fields, default deny, producer role needs) | Spike report with chosen mapping and role list |
| **M1 Read path** | Core, `table`, `cmdb`, `my work`, read verbs, selftest (read matrix) | Agent and human can read allowed data; forbidden tables return exit 4/6 |
| **M2 Write path** | `incident create` (producer), `incident update`, `task update`, idempotency, provenance, policy | Duplicate create is deduplicated; denied fields rejected; audit records complete |
| **M3 Human mode** | PKCE + device login, keychain storage, Windows support | Human runs the same commands as self; MFA enforced by Okta |
| **M4 Catalog & change** | `catalog` verbs, `change get/list`; `change create` (P2) | Ordering works for opted-in items |
| **M5 Hardening** | Signed policy, redaction hook, release signing, harness skill doc | Security review sign-off; skill doc generated and tested with Hermes |

## 13. Concerns and recommendations

1. **Boundary confusion.** Because the CLI has guardrails, it is tempting to loosen ServiceNow roles. Don't. *Recommendation:* review ServiceNow roles as if the CLI did not exist; `selftest` proves it.
2. **`itil` creep.** The CMDB Instance API and many incident operations default to `itil`. *Recommendation:* custom roles/ACLs and the record-producer pattern; use Table API for CMDB reads in v1.
3. **Two audiences, two mappings.** Agents and humans cannot share one OIDC provider record. *Recommendation:* settle the agent claim→field mapping in M0 because it shapes the `sys_user` data model.
4. **Ticket content is attacker-controllable.** Anyone who can file a ticket can plant instructions an agent later reads. *Recommendation:* keep the untrusted-content marking, narrow field allowlists, and no write verbs that act on read content without human-visible effects (for example, resolving).
5. **P1 outages.** An agent creating a top-priority incident can trigger paging and major-incident processes. *Recommendation:* cap impact/urgency in policy and the record producer; allow agents to create up to "High", and let a human escalate.
6. **Native MCP alternative.** ServiceNow now documents an MCP server feature that accepts third-party IdP tokens with an Okta example ✅ (Australia release docs). It could reduce custom code. *Recommendation:* spike it in M0, but keep the CLI as primary because MCP was ruled out for this stack and the CLI gives client-side policy and bounded output; the OIDC provider configuration you build here is reusable either way.
7. **Concurrency.** The Table API gives no ETag, so concurrent human and agent edits can interleave. *Recommendation:* limit agent updates to journals and a few fields; accept `sys_mod_count` checks as best effort.
8. **Licensing.** Fulfiller-role licensing for agent identities can be material ⚠️. *Recommendation:* ask your ServiceNow account team how custom roles that avoid `itil` are counted before scaling.
9. **Release drift.** Role requirements for CMDB APIs are changing across releases ✅. *Recommendation:* re-run `selftest` on every ServiceNow upgrade.

## 14. Open questions

1. Confirm assumption A1 (what "service items" covers) and whether change creation is wanted at all.
2. ServiceNow release/family and whether the third-party ID token flow is enabled and approved.
3. Who owns the scoped application and its deployment pipeline?
4. Instance-specific values: incident states, categories, assignment-group conventions, record-producer owner.
5. Should agents be allowed to resolve or close tickets in any circumstance?
6. Human mode: which user populations (developers, SREs) and which operating systems?
7. Is there an existing CMDB data-quality owner for the applications agents will look up?

## Appendix — Sources consulted

- ServiceNow: [OAuth inbound](https://www.servicenow.com/docs/r/zurich/platform-security/authentication/oauth-inbound.html) · [Federated token authentication (community)](https://www.servicenow.com/community/platform-privacy-security-blog/federated-token-authentication-for-servicenow-api-access-inbound/ba-p/3367827) · [Configure OIDC provider for third-party tokens](https://www.servicenow.com/docs/r/oJu9n0q6rU~F9UHAnNCt5w/DWASCzLZOp2AjTW1J252Aw) · [Third-party ID token](https://www.servicenow.com/docs/bundle/zurich-platform-security/page/integrate/machine-identity/task/configure-a-third-party-id-token.html) · [ServiceNow IdP/MCP configuration](https://www.servicenow.com/docs/r/XRgEFx7nPN6ciT_BBIvnbg/wHMdBoDSi6AUH2QGt19rhA) · [REST API overview, access policies](https://www.servicenow.com/docs/r/MVRCUFVyKWd7vUoUukbW6g/5p0ILxi5PWo5cXuREsZCAQ) · [CMDB Instance API roles (KB2655604)](https://support.servicenow.com/kb?id=kb_article_view&sysparm_article=KB2655604) · [order_now vs submit_producer (KB2446676)](https://support.servicenow.com/kb?id=kb_article_view&sysparm_article=KB2446676) · [Identification and Reconciliation API](https://www.servicenow.com/docs/r/yokohama/api-reference/rest-apis/c_IdentifyReconcileAPI.html) · [cmdb_read for CI tables (community)](https://www.servicenow.com/community/servicenow-ai-platform-forum/role-needed-for-rest-api-access-to-cmdb-ci-computer/m-p/1062004) · [itil needed for work notes via REST (community)](https://www.servicenow.com/community/developer-forum/required-roles-for-rest-api-is-itil-required/m-p/2762208) · [Service Catalog API (Fluent)](https://www.servicenow.com/docs/r/application-development/servicenow-sdk/fluent-service-catalog-api.html)
- Integrations referencing roles/policies: [SquaredUp ServiceNow plugin](https://docs.squaredup.com/data-sources/servicenow-plugin) · [Dust ServiceNow tool](https://docs.dust.tt/docs/user-documentation/agents/tools/servicenow) · [ConductorOne ServiceNow](https://conductorone.com/docs/baton/servicenow)
- Okta / OAuth: [OAuth 2.0 for native apps (RFC 8252)](https://www.rfc-editor.org/pdfrfc/rfc8252.txt.pdf) · [Implement OAuth for Okta (PKCE recommended)](https://developer.okta.com/docs/guides/implement-oauth-for-okta/main/) · [OAuth 2.0 for native and mobile apps](https://developer.okta.com/blog/2018/12/13/oauth-2-for-native-and-mobile-apps) · [PKCE vs device code for CLIs](https://workos.com/blog/how-to-add-enterprise-sso-to-your-cli)
