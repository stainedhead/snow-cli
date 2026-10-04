# Product summary

`snow` is a purpose-built Go CLI for ServiceNow work that autonomous SDLC agents and human teammates both need: read tables, look up CMDB configuration items, read work items (incident, request, requested item, catalog task, change, problem), create and update incidents, update catalog tasks, and order catalog items. It offers narrow, task-shaped verbs with no raw API passthrough and no token-printing command, and two authentication modes on one command surface: agent (token from the `agent-okta-d` daemon) and human (Okta OAuth 2.0 PKCE or device flow). ServiceNow roles and ACLs are the security boundary; the client-side policy is a guardrail only.

## Status

Implemented and tested against fakes, `httptest` servers and the core's `authtest.Fake`. Not run against a real ServiceNow instance or Okta tenant: the PRD's unconfirmed items remain assumptions (`assumptions.md`) until the M0 spikes (`m0-spike-checklist.md`) run.

Usable now: all read commands, all write commands, `selftest`, `skill generate`, human mode (with `--insecure-store`).
Not usable end to end: agent mode against a real instance (the daemon adapter is a fail-closed stub, exit 3), and human mode without `--insecure-store` (real OS keychain backends are fail-closed stubs). See `deferred.md`.

Built on `github.com/stainedhead/agent-cli-core` v0.2.0 for the output envelope and exit codes, auth interfaces, policy engine, audit log, HTTP transport, selftest runner and skill generator.

Source requirements: [snow-cli-PRD.md](../specs/archive/261003-snow-cli/snow-cli-PRD.md) (Draft v0.1) and [spec.md](../specs/archive/261003-snow-cli/spec.md).
