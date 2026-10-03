# snow-cli

`snow` is a planned Go CLI for ServiceNow, built for autonomous SDLC agents and human teammates. It reads tables, looks up CMDB configuration items (CIs), and reads, creates and updates work items (incidents, requests, catalog tasks), including opening an outage ticket.

**Status: Draft PRD (v0.1), no implementation yet.** This repository currently holds the PRD and project scaffolding.

For the purpose, wider context (Okta-secured agent access) and scope boundary, see [INTENT.md](INTENT.md).

## Why

There is no suitable stock CLI for this: ServiceNow's own `now-sdk` targets application development. The PRD proposes a thin purpose-built CLI instead.

## Key design points

- Narrow, task-shaped verbs; no raw REST passthrough (`snow raw` / `snow api` are not provided in any mode).
- Two authentication modes on one command surface: agent mode (short-lived Okta token from the `agent-okta-d` daemon) and human mode (Okta OAuth 2.0 authorization code + PKCE, with a device flow for headless sessions). ServiceNow validates the Okta token and applies the roles and ACLs of the mapped `sys_user`.
- ServiceNow roles and ACLs are the security boundary; the client-side policy engine is a guardrail and usability layer, never the control.
- Output is bounded and structured, with free text from other people marked untrusted so it is treated as data, not instructions.
- Writes are meant to be attributable and idempotent on retry. Several mechanisms for this (for example `correlation_id` based de-duplication and provenance fields) are marked in the PRD as not yet confirmed against vendor documentation and must be validated in a sub-production instance.
- No CMDB writes, approvals, deletes, user/group/role administration or scripts in v1.

The PRD uses an evidence legend: items marked confirmed were checked against vendor documentation on 2026-10-03; items marked not confirmed (community source or engineering judgment) must be validated before being depended on. This README does not upgrade any of them.

## Shared core: agent-cli-core (build dependency)

`snow` is built on [agent-cli-core](https://github.com/stainedhead/agent-cli-core), a separate Go library repository (auth, policy, output, audit, httpx, selftest, docgen) that also serves `outlook` and `teams`. It originated in PRD section 5, and its own PRD now owns the specification. `snow` will depend on a released tag of it; no release exists yet, so `go.mod` has no `require` for it for now.

## Related repositories

Part of the set rooted at [stainedhead/agentic-teams](https://github.com/stainedhead/agentic-teams):

- [agent-cli-core](https://github.com/stainedhead/agent-cli-core) - shared CLI core library this tool depends on
- [agent-okta-d](https://github.com/stainedhead/agent-okta-d) - credential daemon that supplies agent tokens
- [outlook-cli](https://github.com/stainedhead/outlook-cli) - companion CLI, builds on the shared core
- [teams-cli](https://github.com/stainedhead/teams-cli) - companion CLI, builds on the shared core
- [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) - sibling repository in the same set

## Planned layout

```
cmd/snow/          binary entry point
internal/auth/     TokenSource: daemon client (agent) | OAuth PKCE + keychain (human)
internal/policy/   client-side guardrails (shared core)
internal/sn/       ServiceNow client: Table API, CMDB, Service Catalog, Aggregate, whoami
internal/output/   envelope, truncation, untrusted-content marking (shared core)
internal/audit/    JSONL audit log (shared core)
docs/              product and technical docs, ADRs
specs/             feature specs (archive/ for completed)
user-docs/         install, configuration and usage guides (none yet)
```

Packages marked "shared core" come from `agent-cli-core` rather than being copied here.

## Documentation

- [INTENT.md](INTENT.md) - why this tool exists and where it fits in the set
- [snow-cli-PRD.md](snow-cli-PRD.md) - the product requirements document
- [AGENTS.md](AGENTS.md) - contributor and agent rules
- [docs/](docs/) - product and technical docs
- [user-docs/](user-docs/) - end-user guides (to come once there is something to use)

## Development

```
make fmt vet lint test build
```
