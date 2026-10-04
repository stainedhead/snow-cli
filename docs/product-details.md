# Product details

## Users and modes

- Agent mode (`mode: agent`): an autonomous agent with no ServiceNow secret on its host. The token comes from the `agent-okta-d` daemon through the core's `auth.TokenSource`. Not usable end to end in this release (ADR-002).
- Human mode (`mode: human`): a person signs in with Okta (browser PKCE, or `--device`); tokens are held in a credential store; writes ask for confirmation (`--yes` skips; no terminal without `--yes` is exit 2).

Mode comes only from the profile. `auth login` on an agent profile, and `--trace` outside human mode, are policy denials (exit 6).

## Command surface

| Area | Commands |
|---|---|
| Identity | `whoami`, `auth login [--device] [--insecure-store]`, `auth status`, `auth logout` |
| Tables | `table get`, `table list`, `table count` |
| CMDB | `cmdb ci get`, `cmdb ci search`, `cmdb ci related`, `cmdb app` |
| Work items (read) | `my work`, `incident get\|list`, `request get\|list`, `ritm get\|list`, `task get\|list`, `change get\|list`, `problem get\|list` |
| Catalog | `catalog search`, `catalog get`, `catalog vars`, `catalog order` |
| Writes | `incident create\|update\|resolve`, `task update`, `catalog order` |
| Tooling | `selftest [--include-writes]`, `skill generate [--out] [--check]`, `version` |

Global flags: `--format json|table|text`, `--fields`, `--limit`, `--offset`, `--max-bytes`, `--dry-run`, `--idempotency-key`, `--profile`, `--policy`, `--trace`, `--config`, `--yes`. Not provided in any mode: `snow raw`, `snow api`, a token printing command, `change create`, CMDB writes, approvals, deletes, user/group/role administration, scripts, attachments.

## Behaviour that matters

- Output: one JSON envelope per command with exit code 0 to 9 (core contract). Free text from ServiceNow is marked untrusted. Output is bounded (`--max-bytes`, default 32768). Lists return `{items, page, acl_filtered_possible}`; `data.page.next_offset` is absolute.
- Policy: client-side, fail closed, strict schema (ADR-003). Shipped `agent` and `human` policies are embedded. Deny rules win. Agent impact/urgency may not be 1 (ADR-004). Agents cannot resolve incidents.
- Writes: a pending audit record is written before the request and a failure blocks the write; `priority` is never sent; incident create is deduplicated by `correlation_id` (key defaults to a hash of agent, CI, short description and hour) and stamped with provenance; updates are guarded by `sys_mod_count` (conflict is exit 7); catalog orders are dry-run only for agents unless a policy opts the item in, and an order POST is never auto-retried.
- Hosts: requests go only to `instance.host` over https; redirects elsewhere are refused (exit 4).
- Skill: `snow skill generate` renders the agent skill from the command tree (ADR-006).

## Not built

See `deferred.md`. Unverified assumptions: see `assumptions.md`.
