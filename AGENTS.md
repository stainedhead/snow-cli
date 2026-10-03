# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`snow` is a planned Go CLI for ServiceNow work needed by autonomous SDLC agents and human teammates: read tables, look up CMDB configuration items, and read, create and update work items (incidents, requests, catalog tasks). It exposes narrow task-shaped verbs (no raw REST passthrough) and two authentication modes on one command surface: agent mode (short-lived Okta token from the `agent-okta-d` daemon) and human mode (Okta OAuth 2.0 authorization code + PKCE). ServiceNow roles and ACLs are the security boundary; the CLI policy engine is a guardrail and usability layer, never the control.

Status: Draft PRD, no implementation yet. The PRD is [snow-cli-PRD.md](snow-cli-PRD.md); keep it at the repo root. Evidence markers in the PRD (confirmed vs not confirmed) must be preserved when summarizing it.

## Shared core (agent-cli-core)

`snow` depends on [agent-cli-core](https://github.com/stainedhead/agent-cli-core), its own repository (auth, policy, output, audit, httpx, selftest, docgen), which originated in PRD section 5. Rules:

- Core changes are made in `agent-cli-core`, never copied into this repository.
- Depend on released tags only: no pseudo-versions, no `replace` directives on `main`.
- Do NOT add a `require` for `agent-cli-core` to `go.mod` yet: no release exists. Add it when the first tag exists.
- If the core PRD and PRD section 5 differ, the core PRD wins.

## Layout

Doc routing: a shift in goal, direction or scope goes in [INTENT.md](INTENT.md) (why and where the tool fits); requirements go in the PRD; contributor rules go here.

- `INTENT.md` - purpose, wider context, goals, non-goals, scope boundary
- `snow-cli-PRD.md` - product requirements (the how)

Planned Go layout (from PRD section 4; create directories only when code needs them):

- `cmd/snow/` - binary entry point
- `internal/auth/`, `internal/policy/`, `internal/sn/`, `internal/output/`, `internal/audit/` - packages per the PRD architecture (shared-core packages come from `agent-cli-core`)
- `docs/` - product summary, product details, technical details, architectural decision record
- `specs/` - feature specs; `specs/archive/` for completed ones
- `user-docs/` - see rule below

## user-docs/ rule

`user-docs/` holds only files that help a user adopt, configure and use the tool: install, getting started, configuration reference, usage examples, troubleshooting. It is NOT for design, requirements, spec or process material, and it must not link into `specs/`.

## Standards

- Clean Architecture: dependencies point inward; domain/policy logic has no dependency on HTTP, the OS keychain or the filesystem; adapters sit at the edges behind interfaces.
- TDD: write a failing test first, make it pass, then refactor. Use recorded fixtures for ServiceNow responses.
- Security: never commit credentials, tokens or instance secrets. Never print or log tokens. There is no `token`/`print-token` command and no raw API passthrough.

## Verification

Before committing, all of these must pass:

```
gofmt -l .        # must print nothing
go vet ./...
golangci-lint run
go test ./...
```

`make fmt`, `make vet`, `make lint`, `make test` and `make build` wrap these.

## Git

Use clear commit messages. Do not force-push. Do not commit build output or `.env` files.
