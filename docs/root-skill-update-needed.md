# Root skill updates needed

Compared `skills/snow-cli.md` in the root `agentic-teams` repository (read only, not edited) with the build specification. Apply these by manual pull request when a release exists (PRD 16.1). The generated skill (`make skill`, `dist/snow-cli.md`) supplies the command list, usage, examples, forbidden actions, envelope and exit-code sections; the items below are hand-written guidance or corrections that belong in the root copy.

## Corrections to the current text

1. Pagination. The root says to fetch the next page "using `next_offset`". Per D-h: `data.page.next_offset` is the absolute offset for the next page (null when exhausted). Core `meta.next_offset` is set only when output was truncated for `--max-bytes` and is an index into the items of the page returned, so the next call is `--offset <previous offset + meta.next_offset>`.
2. `acl_filtered_possible` lives in `data`, not `meta` (D-e). Write `data.acl_filtered_possible: true`.
3. `deduplicated` is returned in `data` (`data.deduplicated`), not at envelope level.
4. Impact and urgency. The root says they are "capped by policy". State the scale and the agent rule: ServiceNow 1 = High, 2 = Medium, 3 = Low; the agent policy allows 2 and 3 only, so value 1 is denied with exit 6; a human raises a P1 (D-d, assumption A-01).
5. Global flags. Add `--offset`, `--config`, `--yes` (human mode confirmation skip) and `--display` on reads where supported. `--trace` outside human mode is a policy denial (exit 6), not just unavailable.
6. `auth login` on an agent profile is refused with exit 6 (the root says "disabled"; add the exit code).
7. `selftest` has `--include-writes` (default read only). Keep "do not run unprompted".
8. `incident create`: `--ci` or `--app` is required, together with the other four content flags. A missing flag is exit 9, raised before the policy check.
9. Catalog order: unless the item is opted in by the policy it is `dry_run_only` (the command previews and does not order). A transient failure on an order is not retried automatically (exit 8); check `snow request list` before retrying (A-07).
10. Exit 8 also covers 5xx responses after bounded retries (D-b). Exit 5 also covers records the identity cannot see (ServiceNow returns 404 for ACL-hidden records, A-03); the errors table should say so.
11. Links. The "Tool repo" line points at `snow-cli-PRD.md` at the repository root; the PRD now lives under the spec directory in the tool repository, so the link should point at `INTENT.md` and `user-docs/` only.

## Content to add

1. Availability banner. The tool is built from this repository, but agent mode cannot reach ServiceNow end to end until the `agent-okta-d` daemon adapter is released (ADR-002): agent-mode commands that need a token exit 3 and the message names the daemon socket. The banner may be removed only after a release exists and the skill's examples have run against it (SKILL-5). State the version the skill applies to.
2. Rate limits in the shipped agent policy: `incident create` is limited to 5 per hour and every write rule to 10 per run. An exceeded limit is exit 6 with a `retry-after` style reason; do not retry in a loop.
3. Forbidden fields. Agents cannot write `priority`, `state` on incidents, or any `sys_*` table; reads are limited to the policy field allowlist. Name them so agents do not probe.
4. Resolve. `incident resolve` is denied for agents (deny rule, exit 6); humans resolve with confirmation.
5. Untrusted free text is `untrusted: true` for `description`, `short_description`, `work_notes`, `comments` and CI descriptions (spec section 5).
6. Provenance. Writes add the work-note prefix `[snow-cli agent=<id> run=<run_id>]` and `correlation_display=agent:<id>` (unverified, A-04); mention that the agent id comes from config or `SNOW_AGENT_ID`, and the run id from `SNOW_RUN_ID` when set.
7. Idempotency. The default key is a hash of agent id, CI, short description and hour bucket; passing an explicit stable `--idempotency-key` is still recommended.
8. Pagination guidance for lists: use `--limit` and the `data.page` object; empty or short pages may hide ACL-filtered records.
9. Section order. The generated skill orders commands alphabetically with one entry per leaf command (`incident get`, `incident list`); the root groups them (`incident get|list`). Keep the grouped table in the root copy and add the generated per-command forbidden actions beneath it.
10. `snow skill generate` is a maintainer command and should not appear in the agent-facing table.

## Process

Per SKILL-4, a change to commands, flags, exit codes, policy verbs or write modes is not complete until the root skill is updated and names its version. The release checklist includes opening that pull request against `agentic-teams`.
