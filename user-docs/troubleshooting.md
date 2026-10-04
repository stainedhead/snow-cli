# Troubleshooting

## Exit codes

| Code | Category | Meaning | What to do |
|---|---|---|---|
| 0 | `ok` | success | Use `data`; check `meta.truncated`. |
| 1 | `general` | general error, audit failure, failed selftest, declined confirmation | Read `error.message`. |
| 2 | `usage` | bad command line, config or missing policy | Fix the arguments or config. |
| 3 | `auth` | not signed in, credentials unavailable, daemon unreachable | A person must act; do not retry. |
| 4 | `forbidden` | ServiceNow refused (403), or a request to a host other than `instance.host` | Final; roles/ACLs are the boundary. |
| 5 | `not_found` | target not found (also what an ACL-hidden record usually looks like) | Check the identifier. |
| 6 | `policy_denied` | refused by client-side policy | Final; use a different policy or ask a human. |
| 7 | `conflict` | record changed meanwhile (409/412 or `sys_mod_count` moved) | Re-read, then decide. |
| 8 | `rate_limited` | rate limited or transient failure after bounded retries | Wait, retry later (not for orders: check first). |
| 9 | `validation` | invalid input, ServiceNow validation error, invalid policy file | Fix the field named in the message. |

Error output is `{"ok":false,"error":{"code","message","hint"}}`; `hint` says what to do next.

## Common problems

| Symptom | Cause and fix |
|---|---|
| Exit 3, "credential daemon unreachable at socket ..." | Agent mode. The daemon is not running or the socket path is wrong. Start `agent-okta-d`, or set `daemon.socket` or `AGENT_OKTA_D_SOCKET` to its socket. |
| Exit 3, "a human action is needed" (reauth required or revoked) | Agent mode. The daemon cannot refresh the credential. Ask the `agent-okta-d` operator to re-enroll it; do not retry. |
| Exit 8, "credential daemon cannot serve right now" | Agent mode. The daemon is degraded; wait the time in the hint and retry. |
| Exit 3, "credential store ... is unavailable" | Human mode with no keychain backend. Use `--insecure-store` or `SNOW_INSECURE_STORE=1` (see [human login](human-login.md#where-credentials-are-stored)). |
| Exit 3, "not logged in" | Run `snow auth login`. |
| Exit 3 on every human call after login | ServiceNow may reject the token type; try `okta.token_type: id`. |
| Exit 2, "no policy configured (fail closed)" | Pass `--policy agent\|human\|<file>` or set `policy.path`. |
| Exit 2, "invalid config" / unknown key | The config is strict; remove or fix the named key. |
| Exit 2, "profile ... has no instance.host" | Set `instance.host` or `SNOW_INSTANCE_HOST`. |
| Exit 2, "confirmation required but no terminal" | Human mode write without a terminal: add `--yes`. |
| Exit 9, "invalid policy" | The policy file failed the strict parse; the tool refuses to run with it. |
| Exit 6, `--trace` | `--trace` works only in human mode. |
| Exit 6, `auth login` | Agent profiles cannot sign in. |
| Exit 6 for impact/urgency 1 | The agent policy allows 2 and 3 only. |
| Exit 6 on a table or field | It is outside the policy allowlist. Policy messages name the rule. |
| Exit 5 for a record that exists | ServiceNow often answers 404 for records your roles cannot see (unverified). |
| Empty list but `acl_filtered_possible: true` | Records may be hidden by ACLs; the page limit is applied before ACLs (unverified). |
| Empty names for reference fields | Your role cannot read the referenced table (unverified). Ask for read access or use sys_ids. |
| Exit 1, "cannot open audit log" | Make `audit.path` (default `~/.local/state/snow/audit.jsonl`) writable. Writes are blocked if the audit record cannot be written. |
| Exit 1 from a write after the request was sent | The outcome record could not be written; the message says the write may have happened and gives the record number. Check ServiceNow before retrying. |
| Exit 7 on update | The record changed during the update; re-read it. |
| Exit 8 on `catalog order` | Orders are not retried automatically. Run `snow request list` before ordering again. |
| `whoami` fails | The custom identity endpoint (`whoami.path`) may be missing or differ (unverified). |
| `table count` fails | The Aggregate API response shape is unverified; report it. |
| `incident create` cannot return the record | The record producer response shape is unverified; try `incident.create_via: table`. |
| Exit 4 on a host error | Requests go only to `instance.host`; a redirect elsewhere is refused. |

## Not built in this release

- Agent mode against a real daemon and ServiceNow instance (tested only with a fake daemon).
- Real OS keychain backends and a WSL2 credential store (use `--insecure-store`).
- Fixed loopback port setting for the Okta redirect.
- `change create`, attachments, CMDB writes, approvals, deletes, user/group/role administration, scripts.
- Redaction hook and signed policy files.
- Native Windows.
- Release artifacts (see [verifying releases](verifying-releases.md)).

## Unverified against a real instance

Treat these as expectations until your ServiceNow and Okta administrators confirm them: the impact/urgency scale; ACL-hidden records appearing as 404 or short pages; `correlation_id`/`correlation_display` on `task` (repeat protection and provenance); `sys_mod_count` conflict detection; journal fields (`work_notes`, `comments`) readable and writable; the Aggregate API count shape; the custom `whoami` endpoint; the record producer response; which Okta token ServiceNow accepts; Okta loopback redirect handling; rate-limit header behaviour; and the roles the identity needs for each table. Only `snow selftest` against a sub-production instance can show whether the roles are as narrow as intended.
