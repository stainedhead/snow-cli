# M0 spike checklist

M0 needs a sub-production ServiceNow instance and an Okta tenant; the build does not run it. Each item produces evidence (a short note and the chosen value) that is fed back into config defaults, the policy files and the assumptions register (`docs/assumptions.md`). Record results as "confirmed", "differs: <what>" or "blocked".

## Identity and tokens

- [ ] OIDC provider record #1 (`snow-agents`) created from the Okta authorization server metadata URL; Client ID equals the token `aud`; JTI verification off for agents.
- [ ] Agent token (client credentials) is accepted by the Table API for a web-service-only `sys_user`.
- [ ] Agent user claim mapping chosen: `sub` (Okta client ID) to a `sys_user` field, or a custom claim holding the agent id. Record the field.
- [ ] OIDC provider record #2 (`snow-humans`) works for human tokens. Decide access token versus ID token (A-05) and set the `okta.token_type` default.
- [ ] Okta native app: PKCE enforced, refresh-token rotation on, device authorization grant enabled, group assignment `snow-cli-users`.
- [ ] Loopback redirect: does Okta accept a port range, or must ports be fixed? Confirm 8765 to 8769 tried in order (A-06).
- [ ] Human keychain: macOS Keychain, Linux Secret Service, WSL2 (is a Secret Service or keyring daemon available?) (A-13).

## Scoped application and endpoints

- [ ] `whoami` scripted REST endpoint path and response shape (user name, display name, role names, instance) (A-02).
- [ ] Record producer `submit_producer` response: where the record sys_id and number are, and the producer id for config (A-08).
- [ ] Aggregate API `GET /api/now/v1/stats/{table}?sysparm_count=true` returns the count shape the client expects (A-11).
- [ ] Decide `incident.create_via`: `producer` or `table`.

## Table, field and ACL behaviour

- [ ] Impact and urgency scale is 1 = High, 2 = Medium, 3 = Low; priority is derived and the CLI need not write it (A-01).
- [ ] ACL-hidden records: confirm a 404 (or empty page) rather than 403; confirm the Table API applies the limit before ACLs (so short pages can hide records) (A-03).
- [ ] Default table deny: tables without an ACL are inaccessible (A-03).
- [ ] `correlation_id` and `correlation_display` exist on `task` and `incident` in the target release and are writable by the agent role (A-04).
- [ ] `sys_mod_count` is readable and increments by one per update (A-09).
- [ ] Journal fields `work_notes` and `comments` are readable and writable under field ACLs; test with the custom roles (A-10).
- [ ] Reference fields (caller, group, location) return values for the agent role, or come back empty (read ACL on referenced tables).
- [ ] CMDB: roles needed to read `cmdb_ci` children and `cmdb_rel_ci` (`cmdb_read`?); per-table API granularity of REST API access policies (A-12).
- [ ] Catalog `order_now`: is there a dedupe key retrievable afterwards on `sc_request` or `sc_req_item` (A-07)?
- [ ] Role set: confirm which operations need `itil` and which work with custom roles only; record the licensing question for the ServiceNow account team.
- [ ] Non-fulfiller visibility: out-of-box users see only incidents they are involved in.
- [ ] Customer-visible `comments` on own tickets without a fulfiller role.

## Platform behaviour

- [ ] Inbound REST rate-limit rule for the agent role; header names and `Retry-After` behaviour (A-14).
- [ ] Inbound REST calls appear in transaction logs per user (SIEM forwarding).
- [ ] `snow selftest` matrix run against the instance (on demand only).

## CI and release (not instance-bound)

- [ ] Whether `GITHUB_TOKEN` can read the other repositories' contents if they become private (PRD DEP-5), and whether GitHub Packages has a Go module registry.
- [ ] Apple Developer ID and notarization availability; cosign keyless signing from the workflow identity.
- [ ] Agent skill format expected by the harness (PRD SKILL-6).
- [ ] WSL service support (systemd in WSL), only if a service definition is installed.

## Exit

Spike report with the chosen mappings and role list. Update `docs/assumptions.md` statuses; change policy files, config defaults or tests where an assumption was wrong.
