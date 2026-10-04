# snow-cli

`snow` is a Go CLI for ServiceNow, built for autonomous SDLC agents and human teammates. It reads tables, looks up CMDB configuration items (CIs), reads work items (incidents, requests, requested items, catalog tasks, changes, problems), creates and updates incidents and catalog tasks, and orders catalog items.

**Status: implemented and tested against fakes; not yet run against a real ServiceNow instance or Okta tenant.** Agent mode is not usable end to end until the `agent-okta-d` client adapter exists (it fails closed with exit 3). Human mode works but needs `--insecure-store` until real OS keychain backends are built. See [docs/deferred.md](docs/deferred.md) and [docs/assumptions.md](docs/assumptions.md).

For the purpose, wider context (Okta-secured agent access) and scope boundary, see [INTENT.md](INTENT.md).

## Install and build

Requires Go 1.27.

```
make build            # writes bin/snow
bin/snow help
bin/snow version
```

There are no published releases or signed binaries yet.

## Why

There is no suitable stock CLI for this: ServiceNow's own `now-sdk` targets application development. `snow` is a thin purpose-built CLI instead.

## Key design points

- Narrow, task-shaped verbs; no raw REST passthrough (`snow raw` / `snow api` are not provided in any mode).
- Two authentication modes on one command surface: agent mode (short-lived Okta token from the `agent-okta-d` daemon) and human mode (Okta OAuth 2.0 authorization code + PKCE, with a device flow for headless sessions). ServiceNow validates the Okta token and applies the roles and ACLs of the mapped `sys_user`.
- ServiceNow roles and ACLs are the security boundary; the client-side policy engine is a guardrail and usability layer, never the control.
- Output is bounded and structured, with free text from other people marked untrusted so it is treated as data, not instructions.
- Writes are attributable and idempotent on retry. Several mechanisms for this (for example `correlation_id` based de-duplication and provenance fields) are not yet confirmed against vendor documentation and must be validated in a sub-production instance.
- No CMDB writes, approvals, deletes, user/group/role administration or scripts in v1.

The PRD uses an evidence legend: items marked confirmed were checked against vendor documentation on 2026-10-03; items marked not confirmed (community source or engineering judgment) must be validated before being depended on. This README does not upgrade any of them.

## Shared core: agent-cli-core (build dependency)

`snow` is built on [agent-cli-core](https://github.com/stainedhead/agent-cli-core), a separate Go library repository (auth, policy, output, audit, httpx, selftest, docgen) that also serves `outlook` and `teams`. It originated in PRD section 5, and its own PRD now owns the specification. `snow` requires the released tag `agent-cli-core v0.1.0` (no `replace`, no pseudo-version). Gaps found in the core are listed in [docs/core-change-requests.md](docs/core-change-requests.md).

## Related repositories

Part of the set rooted at [stainedhead/agentic-teams](https://github.com/stainedhead/agentic-teams):

- [agent-cli-core](https://github.com/stainedhead/agent-cli-core) - shared CLI core library this tool depends on
- [agent-okta-d](https://github.com/stainedhead/agent-okta-d) - credential daemon that supplies agent tokens
- [outlook-cli](https://github.com/stainedhead/outlook-cli) - companion CLI, builds on the shared core
- [teams-cli](https://github.com/stainedhead/teams-cli) - companion CLI, builds on the shared core
- [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) - sibling repository in the same set

## Layout

```
cmd/snow/          binary entry point
internal/cli/      command router, flags, rendering, skill generation
internal/app/      composition root (config, policy, auth, audit, client wiring)
internal/usecase/  read, write, selftest and whoami services (no I/O)
internal/domain/   records, modes, impact/urgency scale
internal/sn/       ServiceNow client: Table, Aggregate, Service Catalog, CMDB relationships
internal/humanauth/ Okta PKCE and device flows, credential stores
internal/agentauth/ fail-closed daemon client stub
policies/          embedded agent and human policy files
docs/              product and technical docs, ADRs, assumptions, deferred items
specs/             feature specs (archive/ for completed)
user-docs/         install, configuration and usage guides
```

Auth, policy, output, audit, HTTP transport, selftest and skill generation come from `agent-cli-core` rather than being copied here.

## Documentation

User guides ([user-docs/](user-docs/)):

- [Getting started](user-docs/getting-started.md)
- [Configuration reference](user-docs/configuration.md)
- [Usage examples](user-docs/usage.md)
- [Human login](user-docs/human-login.md)
- [Troubleshooting and exit codes](user-docs/troubleshooting.md)
- [Verifying releases](user-docs/verifying-releases.md)

Project documents:

- [INTENT.md](INTENT.md) - why this tool exists and where it fits in the set
- [PRD](specs/261003-snow-cli/snow-cli-PRD.md) and [spec](specs/261003-snow-cli/spec.md) - requirements and feature specification
- [AGENTS.md](AGENTS.md) - contributor and agent rules
- [docs/](docs/) - product summary and details, technical details, ADRs, assumptions register, deferred items, M0 spike checklist

## Development

```
make fmt vet lint test race build
```
