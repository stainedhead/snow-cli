# Configuration reference

## Config file

Location, first match wins: `--config <file>`, then `SNOW_CONFIG`, then `~/.config/snow/config.yaml`. The file is strict YAML: unknown or duplicate keys fail with exit 2. At least one profile is required.

Select a profile with `--profile <name>`, else `default_profile`, else the only profile if there is exactly one.

```yaml
default_profile: agent
profiles:
  agent:
    mode: agent
    agent_id: sdlc-agent-1
    instance:
      host: acme.service-now.com
      release: xanadu
    daemon:
      socket: /var/run/agent-okta-d/agent-okta-d.sock
      provider: snow
    audit:
      path: /var/log/snow/audit.jsonl
    policy:
      path: /etc/snow/agent.policy.yaml
    incident:
      create_via: producer
      producer: <record producer name or sys_id>
      scale: { high: 1, medium: 2, low: 3 }
    whoami:
      path: /api/x_corp_agent/v1/whoami
    selftest:
      fixture_incident: INC0010001
      foreign_incident: INC0010002
  human:
    mode: human
    instance:
      host: acme.service-now.com
    okta:
      issuer: https://acme.okta.com/oauth2/default
      client_id: 0oa1example
      token_type: access
```

### Keys (per profile)

| Key | Default | Meaning |
|---|---|---|
| `mode` | required | `agent` (token from the credential daemon) or `human` (Okta sign-in). Mode comes only from the profile; there is no flag. |
| `agent_id` | `SNOW_AGENT_ID` env | Identifier written to audit records and provenance. |
| `instance.host` | `SNOW_INSTANCE_HOST` env if the key is unset | Bare DNS name, optional port (for example `acme.service-now.com`). No scheme, path, user info or wildcard. All requests go to this host over https only. A redirect to another host is refused (exit 4). |
| `instance.release` | none | Free text note of the ServiceNow release. Not used for behaviour. |
| `okta.issuer` | none | Okta issuer URL (https). Human mode only. |
| `okta.client_id` | none | Okta native app client id. Human mode only. |
| `okta.token_type` | `access` | `access` or `id`: which Okta token is sent to ServiceNow. Unverified which one your instance accepts; flip it if human calls return 401. |
| `daemon.socket` | `/var/run/agent-okta-d/agent-okta-d.sock` | Credential daemon socket (agent mode). Named in the error message when unreachable. |
| `daemon.provider` | `snow` | Provider name requested from the daemon. |
| `audit.path` | `~/.local/state/snow/audit.jsonl` | Audit log (JSON lines). |
| `policy.path` | none | Policy file. |
| `policy.allow_override` | `false` | When false, `--policy` is refused in agent mode (exit 6, nothing is sent) and, in human mode, a named policy (`agent` or `human`) must match the profile mode. Passing the same value as `policy.path` is not an override. Set true only on trusted profiles. |
| `incident.create_via` | `producer` | `producer` (record producer) or `table` (Table API). Unverified against a real instance. |
| `incident.producer` | none | Record producer name or sys_id used when `create_via: producer`. |
| `incident.scale` | 1 high, 2 medium, 3 low | Impact/urgency values on your instance. Validated on load. |
| `incident.states` | none | Map of state names to instance values; the `resolved` entry is used by `incident resolve`. |
| `incident.categories` | none | Accepted by the loader; not used for behaviour in this release. |
| `whoami.path` | `/api/x_corp_agent/v1/whoami` | Path of the scripted identity endpoint (unverified; it is a custom endpoint your ServiceNow team must provide). |
| `selftest.fixture_incident`, `selftest.foreign_incident` | none | Incidents used by `snow selftest --include-writes`. |

### Environment variables

| Variable | Effect |
|---|---|
| `SNOW_CONFIG` | Config file path (lower priority than `--config`). |
| `SNOW_INSTANCE_HOST` | Instance host when the profile has none. A configured host always wins. |
| `SNOW_AGENT_ID` | Agent id when the profile has none. |
| `SNOW_RUN_ID` | Run id for audit and provenance; random if unset. |
| `SNOW_INSECURE_STORE` | `1` stores human credentials in a plain file (same as `--insecure-store`). |

## Global flags

Place them after the command.

| Flag | Meaning |
|---|---|
| `--format json\|table\|text` | Output format (default `json`). |
| `--fields a,b` | Fields to return. Must be inside the policy allowlist, if the policy has one. |
| `--limit N`, `--offset N` | Page size (clamped by the policy `limits.max_results`) and offset. |
| `--max-bytes N` | Output byte budget (default 32768, bounded by the policy). |
| `--dry-run` | Preview a write without sending it. |
| `--idempotency-key K` | Key for `incident create`. |
| `--profile`, `--policy`, `--config` | Profile, policy (`agent`, `human` or a file; see `policy.allow_override`) and config file. |
| `--trace` | Trace HTTP requests to standard error with tokens redacted. Human mode only; in agent mode it is denied (exit 6). |
| `--yes` | Skip the human-mode write confirmation. |

Negative `--limit`, `--offset` or `--max-bytes`, and unknown formats, exit 2.

## Policy files

The client-side policy is a guardrail. ServiceNow roles and ACLs are the real boundary, and a server refusal (exit 4) is final. A missing or invalid policy stops the command (exit 2 if none is configured, exit 9 if invalid).

Built-in policies: `--policy agent` and `--policy human` (embedded in the binary; the sources are `policies/agent.policy.yaml` and `policies/human.policy.yaml` in this repository, usable as starting points for your own file).

### Format

```yaml
version: 1
limits:
  max_results: 200
  max_bytes: 32768
rules:
  - id: deny-sys-tables
    effect: deny                 # deny always wins
    verbs: ["*"]
    resources: ["table:sys_*"]
  - id: incident-create
    effect: allow
    mode: allow                  # allow | dry_run_only | deny
    verbs: [create]
    resources: [incident]
    fields: [short_description, description, cmdb_ci, impact, urgency]
    constraints:
      impact:  { min: 2, max: 3 }
    rate_limit: { per_hour: 5, per_run: 10 }
```

Unknown or duplicate keys make the file invalid. Anything not matched by a rule is denied.

- Verbs: `get`, `list`, `count`, `search`, `related`, `create`, `update`, `resolve`, `order`, `vars`, `whoami`, `selftest`.
- Resources: `table:<name>` (globs such as `table:sys_*`), `cmdb:ci`, `cmdb:app`, `incident`, `request`, `ritm`, `task`, `change`, `problem`, `catalog:item:<sys_id>`, `catalog:search`, `whoami`, `selftest`.
- `fields` is an allowlist. For reads without `--fields`, `snow` requests exactly the allowlist.
- `rate_limit.per_hour` and `per_run` are enforced across invocations from a locked state file `ratelimit.json` next to the audit log (mode 0600; unix only). `per_hour` is a sliding hour window per agent id and rule. `per_run` is counted per agent id, `SNOW_RUN_ID` and rule: export a stable `SNOW_RUN_ID` for each agent run, otherwise every invocation is its own run and `per_run` only bounds one process. A denied call exits 6 with a "retry in ..." reason (hourly). Only allowed requests count; a refused one uses no budget. If the state file cannot be read or is corrupt, the limited action is refused (exit 1, the message names the file). There is no counter shared across rules, so the agent policy puts `per_run: 10` on each write rule (an agent can exceed 10 writes in total).

### What the shipped policies do

| | agent | human |
|---|---|---|
| `sys_*` tables, `sysapproval_approver` | denied | denied |
| Typed reads (incident, request, ritm, task, change, problem, CMDB) | allowed, field allowlists | allowed |
| Other tables | not allowed | read (`get`, `list`, `count`) |
| `incident create` | `impact` and `urgency` 2 or 3 only (1 denied), 5 per hour | 1 to 3, 30 per hour |
| `incident update` | allowlisted fields | allowlisted fields |
| `incident resolve` | denied | allowed |
| `task update` | `work_notes`, `comments`, `state`, `assigned_to` | allowed |
| `catalog order` | dry-run only | allowed |
| `priority` | never written | never written |

Impact and urgency use the ServiceNow scale (1 high, 2 medium, 3 low). If your instance differs, adjust both `incident.scale` and your policy; unverified against a real instance.

To let an agent order a specific catalog item, copy the agent policy and add, before the `catalog-order-dry-run` rule:

```yaml
  - { id: order-laptop, effect: allow, mode: allow, verbs: [order], resources: ["catalog:item:<sys_id>"], rate_limit: { per_run: 10 } }
```

Signed policies and a redaction hook are not built (a `policy.signature` key is rejected).

## Audit log

Every command writes JSON lines to `audit.path`. Writes use block-on-failure: a "pending" record is written before the request; if it cannot be written, nothing is sent and the command exits 1. Records carry no request bodies or tokens. Reads only warn on audit failure.

Each record's `resource` names the target record where there is one, as `<base>:<ref>` (for example `incident:INC0010001`; for `incident create` the ref is the idempotency key). The `outcome` is `pending`, `ok`, `error`, `denied`, `dry_run` (a preview; nothing was sent) or `applied_conflict` (the write was applied, then another writer's change was detected). Selftest probes appear with verbs `selftest:probe-list`, `selftest:probe-resolve` and `selftest:probe-update` and `policy_decision: probe_bypass`: they skip the client policy on purpose to ask the server ACL, and are still audited.
