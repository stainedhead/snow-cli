# Architecture: snow-cli (2026-10-03) - Status: Draft

## Architecture Overview
Clean Architecture; dependencies point inward. Core packages (auth, policy, output, audit, httpx, selftest, docgen) are consumed, not copied.

## Component Architecture
- internal/domain: records, ids, scale, page, errors-as-data.
- internal/usecase: read services, write services (incident, task, order), selftest matrix, whoami; ports defined here.
- internal/sn: HTTP adapter for ports; status mapping to CategoryError (D-b).
- internal/policymap: request builders/verb-resource vocabulary, policy file helpers (D-f).
- internal/config, internal/idempotency, internal/provenance, internal/auditx (Block/Warn wiring).
- internal/humanauth: pkce, device, session store, keychain (interface, fakes, stubs).
- internal/agentauth: daemon client stub (fails closed).
- internal/cli: command tree, flags, rendering; cmd/snow: composition root.

## Layer Responsibilities
Domain/usecase: no net/http, no os keychain, no filesystem. Adapters: all I/O.

## Data Flow
cmd -> flags -> usecase.Execute -> policy.Engine.Check -> audit pending (writes) -> sn client (httpx transport with Authorizer, AllowedHosts) -> map response/errors -> output.Write.

## Sequence Diagrams
[TBD] incident create: validate -> policy -> audit pending -> dedupe query -> POST (marked safe-to-retry) -> audit outcome -> envelope.

## Integration Points
ServiceNow REST (instance host only), Okta (human mode, own transport + AllowedHosts), credential daemon (stub), OS keychain (interface).

## Architectural Decisions
Decisions D-a..D-l in spec.md are the ADR seeds; CLI parsing uses stdlib `flag` with a small command router (no third-party CLI framework) [revisitable in ADR].
