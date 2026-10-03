# Intent

## Purpose
`snow` is the ServiceNow command-line tool for the agentic-teams set. It gives autonomous SDLC
agents (and the humans who work beside them) task-shaped commands to read tables, look up CMDB
configuration items, and read, create and update work items such as incidents, requests and catalog
tasks, including opening an outage ticket. There is no suitable stock CLI for this, so the PRD
proposes a thin purpose-built one.

**Status: draft PRD (v0.1) and project scaffold. No code exists yet.**

### The wider context
The agentic-teammate project aims to let AI agents work as real teammates. Each agent runs inside
Hermes, or inside a CLI harness we provide, in a container built from the images in
`agentic-team-w-paperclip`. The Go tools in this set exist so that **Okta secures the agent's access
to the key tooling** it needs: AWS, GitHub, ServiceNow, Microsoft 365 and Atlassian.

`snow` is deployed to the machine or container the agent identity runs inside. Access flows like this:

```
agent host: harness (Hermes / CLI) in a container
   -> agent-okta-d daemon  +  CLIs (snow, outlook, teams)  +  stock tools (aws, git, gh)
   -> Okta (identity root)
   -> AWS / GitHub / ServiceNow / M365 / Atlassian
```

Identity is **per agent and attributable**. Each agent has its own Okta identity, which ServiceNow
maps to its own `sys_user`. Every action is therefore traceable to one agent, every write can be
attributed and retried safely, and access can be cut off for that one agent. The agent holds no
long-lived ServiceNow secret. In agent mode the token comes from the daemon; in human mode a person
signs in as themselves, and ServiceNow applies that person's roles and ACLs.

## Where this fits
Root map: [stainedhead/agentic-teams](https://github.com/stainedhead/agentic-teams).

| Repository | What it is | Relation to `snow` |
|---|---|---|
| [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) | Container images with the Hermes, OMP and OpenCode harnesses, plus Paperclip | Provides the harness and container the agent runs in; `snow` is placed on that host |
| [agent-okta-d](https://github.com/stainedhead/agent-okta-d) | Credential daemon; Okta OIDC is the identity root | Supplies the short-lived Okta token in agent mode (daemon unix socket) |
| [snow-cli](https://github.com/stainedhead/snow-cli) | This repository | Defines the shared CLI core (PRD section 5) |
| [outlook-cli](https://github.com/stainedhead/outlook-cli) | Mail as the agent's own Entra user | Peer tool; reuses the same core |
| [teams-cli](https://github.com/stainedhead/teams-cli) | Teams messaging as the agent's own Entra user | Peer tool; reuses the same core |

PRD section 5 defines the shared `agent-cli-core` Go module (auth, policy, output, audit, httpx,
selftest, docgen) that `snow`, `outlook` and `teams` are built from. **Where it lives, in its own
repository or inside this one, is an open question and is not decided here.**

## Goals
- **Complete the agent's ITSM tasks without any ServiceNow secret on the host** (PRD G1).
- **Let humans run the same commands as themselves**, for debugging and for sharing workload with
  agents (G2).
- **Make every write attributable to one identity and idempotent on retry** (G3). The PRD marks
  several mechanisms for this, such as `correlation_id` de-duplication and provenance fields, as
  not yet confirmed (the PRD's warning-sign items). They are not assumed to work.
- **Keep output safe to feed to an LLM**: bounded, structured, with other people's free text marked
  untrusted (G4).
- **Keep ServiceNow-side permissions minimal, explicit and testable** (G5), proven by
  `snow selftest`.

## Non-goals
- **Being the security control.** ServiceNow roles and ACLs are the boundary. The CLI's policy engine
  is a guardrail and usability layer, never the control.
- **Raw REST passthrough** (`snow raw` or `snow api`), in any mode.
- **CMDB writes in v1, approvals, deleting records, user/group/role administration, scripts or flows.**
- **Issuing or exposing tokens.** There is no `token` or `print-token` command.
- **Running the identity infrastructure.** Okta setup and the credential daemon belong to
  `agent-okta-d` and the Okta tenant. The ServiceNow scoped app (roles, ACLs) is handed to the
  ServiceNow platform team; its owner is still to be decided.
- **Deploying or operating the agent fleet**, and building the harness images.

## Scope boundary in one line
> `snow` is the agent's narrow, attributable doorway into ServiceNow, not the lock on that door:
> ServiceNow decides what is allowed, and Okta and the daemon decide who is asking.

## How this file is used
INTENT.md captures *why* this tool exists and where it sits in the wider set. The *how* lives in
[snow-cli-PRD.md](snow-cli-PRD.md), and contributor rules in [AGENTS.md](AGENTS.md). Update this file
when the tool's goals, direction or scope shift, or when the set around it changes, not when
implementation details change. The PRD's evidence markers (confirmed versus not confirmed) are
authoritative; this file does not upgrade any of them.
