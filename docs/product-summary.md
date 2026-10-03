# Product summary

`snow` is a purpose-built CLI for ServiceNow work that autonomous SDLC agents and human teammates both need: read tables, look up CMDB configuration items, and read, create and update work items (incidents, requests, catalog tasks), including opening an outage ticket. It offers narrow, task-shaped verbs with no raw API passthrough, and two authentication modes (agent via `agent-okta-d`, human via Okta OAuth 2.0 PKCE) on one command surface. ServiceNow roles and ACLs are the security boundary; the CLI policy engine is a guardrail only.

Source: [snow-cli-PRD.md](../snow-cli-PRD.md) (Draft v0.1, section 1).
