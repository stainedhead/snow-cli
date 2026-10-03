# Architectural decision record

One entry per decision: context, decision, consequences. Entries are drafted
from the approved spec (`D-*` references are spec decision numbers) and are
revised at the integration review. Status values: Accepted (decided in the spec),
Proposed (drafted here, needs review), Open (undecided, tracked elsewhere).

## ADR-001 Depend on agent-cli-core at a released tag

- Status: Accepted
- Context: auth, policy, output envelope, audit, httpx, selftest and docgen are shared by the CLI set and live in their own repository, `agent-cli-core`.
- Decision: `go.mod` requires `github.com/stainedhead/agent-cli-core v0.1.0`. No `replace` directive and no pseudo-version on `main`; a test greps `go.mod`. Core changes are requested, never copied (see `docs/core-change-requests.md`).
- Consequences: gaps in the core are worked around in `snow` and recorded as change requests; a core bump is an ordinary PR (DEP-6).

## ADR-002 Agent-mode daemon client is a fail-closed stub

- Status: Accepted (revisit when the real adapter is released)
- Context: agent mode takes its Okta token from the `agent-okta-d` daemon through the core's `auth.DaemonClient` / `auth.TokenSource` interfaces. The core defines the interfaces, but the real adapter (and the `agent-okta-d` `pkg/client` release it needs) does not exist yet.
- Decision: the composition root has a `newDaemonClient()` that returns a client reporting the daemon unavailable. It surfaces the core's unreachable error (exit 3, category `auth`) and the message names the configured `daemon.socket` path. `agent-okta-d` is NOT added to `go.mod`. `app.Options.DaemonClient` is the test seam; tests use the core's `authtest.Fake`. Nothing in `snow` fabricates, caches or reads a token on its own, so there is no "pretend it works" path.
- Consequences: in this release an agent-mode command that reaches ServiceNow exits 3 against a real instance until the adapter lands. Human mode and all fake-server tests are unaffected. Replacing the stub is a one-function change in the composition root plus the `go.mod` require; it is listed in `docs/deferred.md`. The agent skill must say the agent mode is not usable end to end until then (`docs/root-skill-update-needed.md`).

## ADR-003 Shipped policies use the core's strict schema, not the PRD sketch

- Status: Accepted (D-f), implemented as `policies/agent.policy.yaml` and `policies/human.policy.yaml`
- Context: PRD section 9 shows a policy sketch that is not loadable by the core (`policy.Parse` rejects unknown keys and duplicate keys).
- Decision: both files use the core schema (`version: 1`, `limits`, ordered `rules` with `id`, `effect`, `verbs`, `resources`, `mode`, `fields`, `constraints`, `rate_limit`). A verb/resource vocabulary (`internal/policymap`) is the stable contract between use cases, policy files and the generated skill. Files are embedded (`policies.Named`) so `--policy agent|human` works without a file on disk, and every file is parsed in unit tests.
- Consequences: constructs the core cannot express are handled outside policy (`incident.create.require` is validated by `snow` before the policy check, exit 9) or approximated (`max_writes_per_run` becomes `rate_limit.per_run` on each write rule because the core has no shared write counter; see core change requests). The mapping table lives in the spec (D-f).

## ADR-004 Impact and urgency use the ServiceNow scale; the PRD inversion is corrected

- Status: Accepted (D-d)
- Context: ServiceNow scale is 1 = High, 2 = Medium, 3 = Low. The PRD sketch `max_impact_urgency: 2  # nothing above High` maps to a core `Max: 2`, which allows 1 (the highest) and forbids 3, the reverse of the intent.
- Decision: the agent policy constrains `impact` and `urgency` with `min: 2, max: 3`; the human policy allows 1..3 so a human escalates. `priority` is never written (and not in any create/update allowlist). A test named for assumption A-01 asserts 1 is denied and 2 and 3 are allowed for both fields on create and update.
- Consequences: instances with a different scale need a different policy file; the config key `incident.scale` records the instance's values. The P1 block also belongs server-side in the record producer, which is out of scope here.

## ADR-005 Client policy is a guardrail; ServiceNow roles and ACLs are the boundary

- Status: Accepted
- Context: loosening ServiceNow roles because the CLI has guardrails would invert the security model (PRD section 13.1).
- Decision: the policy engine narrows what the CLI attempts and explains denials early (exit 6). A server denial is exit 4 and final. Deny rules (`sys_*` tables, `sysapproval_approver`, agent `resolve`) win over any allow. No command prints a token, there is no raw REST passthrough, and the audit log is block-on-failure for writes.
- Consequences: `snow selftest` is the acceptance test the ServiceNow platform team runs; it needs a live instance and never runs in PR CI.

## ADR-006 The agent skill is generated from the command tree; the root copy is updated by hand

- Status: Accepted (FR-051, SKILL-3)
- Decision: `snow skill generate` renders the skill through the core's `docgen` from the router's command tree (commands, usage, examples and forbidden actions per command). `make skill` writes `dist/snow-cli.md` (git-ignored). A golden file (`internal/cli/testdata/skill.golden.md`) pins the output; the unit test and a CI step fail on drift. The root `skills/snow-cli.md` in the `agentic-teams` repository is not edited from here; needed changes are recorded in `docs/root-skill-update-needed.md` and applied by a manual pull request at release time (PRD 16.1).
- Consequences: adding or changing a command changes the golden; the golden is regenerated with `go test ./internal/cli -run TestSkillGolden -update` and the diff reviewed. Hand-written guidance that `docgen` cannot derive lives only in the root copy.

## ADR-007 Output extras live in `data`; list pagination is snow-trimmed

- Status: Accepted (D-e, D-h)
- Context: core `output.Meta` is fixed; core bounds arrays, strings, but not an `items` array nested in an object (verified in the core `output` package).
- Decision: `acl_filtered_possible`, `deduplicated`, `page` detail and `dry_run` go in `data`. Lists return `{items, page, acl_filtered_possible}` and `snow` trims `items` to `--max-bytes` itself. `data.page.next_offset` is absolute; core `meta.next_offset` (when set) is relative to the page returned.
- Consequences: documented in the skill and user docs; a core request asks for an offset base option.

## ADR-008 Human token type sent to ServiceNow defaults to the access token

- Status: Open (decided by M0 spike, A-05)
- Decision for now: config `okta.token_type: access|id`, default `access`. Changing the default after the spike is a one-line change.
