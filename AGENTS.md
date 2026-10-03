# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`snow` is a planned Go CLI for ServiceNow work needed by autonomous SDLC agents and human teammates: read tables, look up CMDB configuration items, and read, create and update work items (incidents, requests, catalog tasks). It exposes narrow task-shaped verbs (no raw REST passthrough) and two authentication modes on one command surface: agent mode (short-lived Okta token from the `agent-okta-d` daemon) and human mode (Okta OAuth 2.0 authorization code + PKCE). ServiceNow roles and ACLs are the security boundary; the CLI policy engine is a guardrail and usability layer, never the control.

Status: Draft PRD, no implementation yet. The PRD is [snow-cli-PRD.md](snow-cli-PRD.md); keep it at the repo root. Evidence markers in the PRD (confirmed vs not confirmed) must be preserved when summarizing it.

## Shared core (open question)

PRD section 5 defines the shared `agent-cli-core` Go module (auth, policy, output, audit, httpx, selftest, docgen) used by `snow`, `outlook` and `teams`. Whether it lives in its own repository or inside this repository is an open question. Do not decide it unilaterally; raise it with the maintainers.

## Layout

Planned Go layout (from PRD section 4; create directories only when code needs them):

- `cmd/snow/` - binary entry point
- `internal/auth/`, `internal/policy/`, `internal/sn/`, `internal/output/`, `internal/audit/` - packages per the PRD architecture (some may come from `agent-cli-core`)
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
