# Dev-Flow Process Analysis

**Feature:** snow-cli (ServiceNow CLI for agents and humans, built on agent-cli-core v0.1.0)
**Spec directory:** specs/archive/261003-snow-cli (plus follow-up specs/archive/261003-snow-cli-auto-review)
**Report generated:** 2026-10-04 (00:42Z; git timestamps below are local -0400, status times are UTC)

---

## 1. Executive Summary

snow-cli is a Go 1.27 Clean Architecture CLI for ServiceNow with agent mode (policy, audit, idempotency) and human mode (Okta PKCE/device flows), read and write commands, catalog ordering, selftest, and a generated agent skill. It was built in five parallel workstreams (WS-A foundation, then B to E), followed by an automated review that produced a second fix spec (WS-F, G, H).

**Total runtime:** first feature commit 2026-10-03 19:20:16 -0400 (status dashboard) to last pre-report commit 20:40:02 -0400, about 80 minutes. Repo history starts earlier with the PRD (14:49 to 16:19 -0400).
**Overall assessment:** Fast and largely clean. Strict TDD (red test commits precede implementation) held across streams. The orchestrator used sub-agents that often returned no report, so progress had to be inferred from git and worktree state. One commit missed the required trailer.

---

## 2. Step-by-Step Timing

Status times are UTC (git local time = UTC minus 4h).

| Step | Name | Start | End | Runtime (min) | Key Outputs |
|---|---|---|---|---|---|
| 0 | PRD validation | 23:20 | 23:22 | 2 | PRD checked |
| 1 | Create Spec | 23:22 | 23:26 | 4 | spec, plan, tasks, architecture |
| 2 | Review Spec | 23:26 | 23:28 | 2 | ordered test-first tasks, ownership |
| 3 | Implement Product | 23:28 | 00:08 | 40 | WS-A to E code, tests, merges |
| 4 | Docs and User Docs | 00:08 | 00:12 | 4 | ADRs, README, user-docs |
| 5 | Code and Design Review | 00:12 | 00:17 | 5 | review findings |
| 6 | Prepare Review PRD | n/a | 00:17 | n/a | auto-review PRD |
| 7 | Archive Original Spec | n/a | 00:17 | n/a | specs/archive/261003-snow-cli |
| 8 | Spec Review Fixes | 00:17 | 00:39 | 22 | fix spec, S0 prelude |
| 9 | Implement Review Fixes | n/a | 00:39 | included in 8 | WS-F, G, H merged |
| 10 | Archive Fixes Spec | 00:39 | 00:42 | 3 | specs/archive/261003-snow-cli-auto-review |
| 11 | Final Quality Pass | n/a | 00:42 | n/a | build, vet, race tests, lint |
| 12 | Process Analysis Report | 00:42 | 00:42 | n/a | this report |
| 13 | Archive Spec | 00:42 | 00:42 | 0 | both specs already in specs/archive |

**Notable observations:**
- Steps 6, 7, 9, 11 have no recorded start; the dashboard is incomplete there. Git boundaries: PRD commits 20:16 to 20:17, spec creation 20:19, S0 prelude 20:21, fix merges 20:36, wrap-up 20:38 to 20:40 (-0400).
- Step 3 is 40 min and dominated by parallel workstreams; WS-A foundation (19:30 to 19:43) gated the rest, B to E landed 19:46 to 19:57.
- The review fix cycle (steps 8 to 9) ran about 22 min with three parallel streams.
- Spec archiving happened at step 7 and 10, so step 13 was a confirmation only.

---

## 3. Commit and Push Summary

**Total commits:**      113 on branch history (103 since the 19:20 dashboard commit). No PRs created yet (step 14 pending).

| Commit | Timestamp | Message |
|---|---|---|
| 3442e72 | 2026-10-03T20:40:02-04:00 | chore: archive auto-review spec |
| ce30ed4 | 2026-10-03T20:39:42-04:00 | chore: status steps 8-9 |
| 124cdca | 2026-10-03T20:38:43-04:00 | feat(cli,docs): wire --expected-mod-count, e2e audit outcome tests, fold ws-*-requests into docs, tick review FRs |
| 6753a5f | 2026-10-03T20:36:03-04:00 | merge: WS-g fixes into feat/snow-cli |
| df3ab7b | 2026-10-03T20:36:03-04:00 | merge: WS-h fixes into feat/snow-cli |
| f626022 | 2026-10-03T20:36:03-04:00 | merge: WS-f fixes into feat/snow-cli |
| fc73b44 | 2026-10-03T20:34:42-04:00 | feat(sn,read): FR-R13 server-error mapping, oversize body error, rel-table policy checks; sn coverage; WS-H notes |
| 38eae81 | 2026-10-03T20:32:48-04:00 | feat(read): FR-R12 inverted untrusted marking with shape-checked structured set, golden injection tests |
| 4a8cf47 | 2026-10-03T20:30:03-04:00 | refactor(cli): typed Env fields and policy-error port; docs hand-off for WS-H (FR-R14) |
| dbafd06 | 2026-10-03T20:30:00-04:00 | feat(idempotency,write,sn): FR-R11 key validation before guard, previous-hour dedupe, no active filter |
| c54fed9 | 2026-10-03T20:28:51-04:00 | feat(read,write): FR-R10 audit target refs and dry_run outcome from use cases |
| a120ef8 | 2026-10-03T20:28:47-04:00 | test(humanauth): raise coverage over rotation, store and callback paths; WS-F requests (FR-R14) |
| e49397d | 2026-10-03T20:28:32-04:00 | test(app): default order is a dry_run outcome in the audit |
| 4e28159 | 2026-10-03T20:28:25-04:00 | feat(auditx): audit target ref suffix and dry_run/applied_conflict outcomes (FR-R10) |
| 37c7998 | 2026-10-03T20:28:16-04:00 | test(app): catalog order reads go through the guarded read service (FR-R03) |
| 4f69521 | 2026-10-03T20:27:50-04:00 | fix(humanauth): harden PKCE loopback login (FR-R09) |
| a3760c2 | 2026-10-03T20:27:46-04:00 | feat(sn,write): FR-R08 pre-write mod-count precondition, applied-conflict message and audit outcome |
| 5c54b4d | 2026-10-03T20:27:44-04:00 | fix(auditx): enforce per_hour and per_run limits across invocations from a locked state file (FR-R02) |
| d39bab1 | 2026-10-03T20:26:38-04:00 | feat(sn): FR-R07 create retry re-runs dedupe before every re-send |
| f7f3463 | 2026-10-03T20:25:47-04:00 | fix(selftest): audit every server probe through a policy-skipping guard (FR-R04) |
| b087d43 | 2026-10-03T20:25:22-04:00 | feat(read,sn): FR-R05 encoded-query parser, field submission to policy, ORDERBY by clause position |
| 7672248 | 2026-10-03T20:24:10-04:00 | fix(app): pin --policy to the profile unless policy.allow_override (FR-R06) |
| f5c38b7 | 2026-10-03T20:23:29-04:00 | test+fix(humanauth): restrict Okta client to issuer host, refuse redirects (FR-R01) |
| 280949a | 2026-10-03T20:23:06-04:00 | test(write): FR-R03 order catalog reads guarded, denied vars sends zero HTTP |
| 424c02d | 2026-10-03T20:21:41-04:00 | feat(usecase): S0 ports prelude for auto-review fixes |
| b8f976b | 2026-10-03T20:19:06-04:00 | docs(spec): create snow-cli auto-review fix spec with 3-stream split |
| d80f1b5 | 2026-10-03T20:17:54-04:00 | chore: status steps 5-7 |
| 9c7ea1f | 2026-10-03T20:17:16-04:00 | docs: archive spec 261003-snow-cli to specs/archive and fix references |
| ef7a3f5 | 2026-10-03T20:17:03-04:00 | docs: revise auto review PRD after review-prd (counts, NFRs, open questions, acceptance) |
| 64284ce | 2026-10-03T20:16:00-04:00 | docs: auto review PRD for snow-cli (dev-flow step 5) |
| e839bd5 | 2026-10-03T20:11:52-04:00 | docs: product docs, ADRs, README and user-docs for implemented snow CLI |
| e4e48f6 | 2026-10-03T20:07:24-04:00 | feat(E3,I2,E7): selftest, integration and security tests, apply WS requests |
| 47bfa6c | 2026-10-03T19:57:16-04:00 | fix: integration merge (test const clash, skill golden) |
| 797eab8 | 2026-10-03T19:56:45-04:00 | merge: WS-e into feat/snow-cli |
| f35cacb | 2026-10-03T19:56:45-04:00 | merge: WS-d into feat/snow-cli |
| ca2bcfb | 2026-10-03T19:56:45-04:00 | merge: WS-c into feat/snow-cli |
| 90b422f | 2026-10-03T19:56:45-04:00 | merge: WS-b into feat/snow-cli |
| a1f1962 | 2026-10-03T19:55:32-04:00 | test(B3): assumption tests, lint fixes |
| dc247b7 | 2026-10-03T19:55:21-04:00 | feat(B1-B6): read commands, wiring and WS-B requests doc |
| 7c0f21d | 2026-10-03T19:54:18-04:00 | test(B1-B6): end-to-end read command tests against snfake (red) |
| 0fd47e0 | 2026-10-03T19:53:49-04:00 | feat(B5): catalog search/get/vars use cases |
| 404e949 | 2026-10-03T19:53:21-04:00 | feat(B5): sn Catalog reader (search/item/variables) |
| 366789f | 2026-10-03T19:52:50-04:00 | feat(B3): cmdb ci get/search/related and app use cases |
| b4cca3f | 2026-10-03T19:52:26-04:00 | feat(C3-C7): CLI write commands, wiring, e2e tests, notes |
| 2d3ad50 | 2026-10-03T19:51:44-04:00 | test(B3): cmdb ci get/search/related and app tests (red) |
| 4addb93 | 2026-10-03T19:51:44-04:00 | feat(B4): work item and my work use cases |
| daa91e2 | 2026-10-03T19:51:21-04:00 | test(C3-C7): CLI write command tests (red) |
| ac95f65 | 2026-10-03T19:50:38-04:00 | test(B4): work item and my work tests (red) |
| a2259ae | 2026-10-03T19:50:31-04:00 | feat(C3-C6): sn write adapters (incident, task, order) |
| e5a1816 | 2026-10-03T19:50:11-04:00 | feat(B1,B2,B6): read table use cases, pagination, Fit trimming, untrusted marking |
| def4ceb | 2026-10-03T19:50:10-04:00 | docs(E8): ADR, deferred, core change requests, root skill updates, M0 checklist, assumptions register, WS-E requests; CI static builds |
| 289263f | 2026-10-03T19:50:07-04:00 | feat(B1,B2,B6): read table use cases, pagination, Fit trimming, untrusted marking |
| 9b733f0 | 2026-10-03T19:50:03-04:00 | feat(D1-D5): human auth (keychain iface, PKCE, device flow, refresh/rotation, auth commands) |
| 5dca700 | 2026-10-03T19:50:03-04:00 | test(D1-D5): human auth store, PKCE, device, refresh, CLI and wiring tests |
| 3f139be | 2026-10-03T19:49:41-04:00 | test(C3-C6): sn write adapter tests (red) |
| 1c1ab82 | 2026-10-03T19:48:57-04:00 | feat(C3-C7): write use cases (incident create/update/resolve, task update, catalog order) |
| 3422e50 | 2026-10-03T19:48:55-04:00 | test(B1,B2): read use case table tests and helpers (red) |
| ac2c810 | 2026-10-03T19:48:17-04:00 | feat(B1): sn Tables reader (get/list/count) |
| 10ab648 | 2026-10-03T19:48:10-04:00 | feat(E5): CI workflow with skill drift check, no hashFiles gating, workflow shape test |
| 44e5ca6 | 2026-10-03T19:48:05-04:00 | feat(E5): CI workflow with skill drift check, no hashFiles gating, workflow shape test |
| 5497cb5 | 2026-10-03T19:47:44-04:00 | test(C3-C7): write use case tests (red) |
| 589f60e | 2026-10-03T19:47:38-04:00 | feat(E4): snow skill generate via docgen with golden and --check drift |
| b276210 | 2026-10-03T19:47:30-04:00 | test(B1): sn Tables reader tests (red) |
| 6ab4ee6 | 2026-10-03T19:47:29-04:00 | test(E4): red tests for skill generate and drift check |
| 2ea0a53 | 2026-10-03T19:46:52-04:00 | feat(E2): policymap request builders |
| 33986a2 | 2026-10-03T19:46:52-04:00 | test(E2): red tests for policymap request builders |
| fec6757 | 2026-10-03T19:46:52-04:00 | feat(E1): agent and human policy files, embedded Named lookup |
| 67f90fc | 2026-10-03T19:46:32-04:00 | test(E1): red tests for shipped agent and human policies |
| c14201b | 2026-10-03T19:46:14-04:00 | feat(C1,C2): idempotency key/dedupe and provenance |
| 01f7b32 | 2026-10-03T19:46:13-04:00 | test(C1,C2): idempotency key/dedupe and provenance tests (red) |
| b68b1cd | 2026-10-03T19:43:30-04:00 | docs: record A-GATE freeze SHA and progress |
| 8bfc277 | 2026-10-03T19:43:20-04:00 | fix(A-GATE): usecase free of net/http; Assumption-named tests for A-01/02/03/05/11 |
| 82b24fd | 2026-10-03T19:43:06-04:00 | test(A-GATE): red layer guard for domain/usecase imports |
| f94a1d9 | 2026-10-03T19:41:50-04:00 | docs: WS-A implementation notes and frozen contracts |
| bff36a4 | 2026-10-03T19:41:23-04:00 | feat(A9,A10): composition root, daemon stub, keychain placeholder, version and whoami |
| 50c8a0b | 2026-10-03T19:41:04-04:00 | test(A9): red end-to-end tests for composition root (version, whoami, exit 3 stub) |
| 5b27125 | 2026-10-03T19:40:36-04:00 | test(A10): red tests for whoami identity adapter, use case and core commands (app wiring WIP) |
| ec38c87 | 2026-10-03T19:40:07-04:00 | test(A9): red tests for agentauth stub and composition root |
| 1c501af | 2026-10-03T19:38:41-04:00 | feat(A8): CLI router, global flags, render helper, exit mapping, area stubs; progress |
| dfded32 | 2026-10-03T19:37:47-04:00 | test(A8): red tests for CLI router, flags, render, exit mapping |
| 674b618 | 2026-10-03T19:37:09-04:00 | fix: lint |
| ef1aa41 | 2026-10-03T19:37:01-04:00 | feat(A5): sn client with httpx wiring, D-b status map, query builders |
| c6a990a | 2026-10-03T19:36:04-04:00 | test(A5): red tests for sn client, status map and query builders |
| 4370cc4 | 2026-10-03T19:36:04-04:00 | feat(A2): snfake and oktafake test support |
| 258d0ae | 2026-10-03T19:35:13-04:00 | test(A2): red self-tests for snfake and oktafake |
| 06fadc0 | 2026-10-03T19:34:29-04:00 | fix(A7): lint |
| 334cb25 | 2026-10-03T19:34:16-04:00 | feat(A5,A7): policy denial adapter; auditx guard with Block/Warn, pending/outcome records |
| 38dbc8a | 2026-10-03T19:33:53-04:00 | test(A5): red tests for policy denial adapter |
| f62c9d1 | 2026-10-03T19:33:42-04:00 | test(A7): red tests for auditx guard |
| 54625c0 | 2026-10-03T19:33:41-04:00 | feat(A6): policymap vocabulary and builder skeleton; lint fixes |
| 62e03b5 | 2026-10-03T19:33:41-04:00 | test(A6): red tests for policymap vocabulary |
| de870cf | 2026-10-03T19:33:05-04:00 | feat(A4): strict config loader with host validation |
| f1537cd | 2026-10-03T19:32:52-04:00 | test(A4): red tests for config loader and host validation |
| 8521632 | 2026-10-03T19:32:13-04:00 | feat(A3): domain types and usecase ports |
| 5d71423 | 2026-10-03T19:31:58-04:00 | test(A3): red tests for usecase ports contract |
| 482d4e2 | 2026-10-03T19:31:21-04:00 | test(A3): red tests for domain types |
| 74067c0 | 2026-10-03T19:30:44-04:00 | feat(A1): go.mod with agent-cli-core v0.1.0, Makefile targets |
| 330c965 | 2026-10-03T19:30:39-04:00 | test(A1): red tests for go.mod constraints |
| a81436c | 2026-10-03T19:28:39-04:00 | chore: status step 2 complete |
| 80ecc4c | 2026-10-03T19:28:03-04:00 | spec: mark review done, fix task count |
| 4d7b119 | 2026-10-03T19:27:56-04:00 | spec: review fixes - ordered test-first tasks, ownership, verified core API |
| ebd39a4 | 2026-10-03T19:25:51-04:00 | Set spec path in DEV-FLOW-STATUS |
| 51ae88c | 2026-10-03T19:25:46-04:00 | Add snow-cli feature spec (dev-flow step 1) |
| 98804d9 | 2026-10-03T19:20:16-04:00 | chore: dev-flow status dashboard |
| 0477725 | 2026-10-03T16:19:41-04:00 | PRD: skill updates reach the root by manual pull request for now |
| 84cbafe | 2026-10-03T16:16:42-04:00 | PRD: require an agent skill document, published in the root repo's skills/ folder |
| 0880667 | 2026-10-03T16:10:57-04:00 | PRD: native Windows not required (WSL2 uses the Linux build); daemon-unreachable is exit 3 with a clear message |
| 9c11220 | 2026-10-03T15:31:17-04:00 | CI: add tidy and cross-compile checks, pin tool versions, skip Go steps until code exists |
| 5bddac4 | 2026-10-03T15:28:36-04:00 | Depend on agent-cli-core as its own repository |
| c37708f | 2026-10-03T15:07:37-04:00 | PRD: add CI/CD and release requirements |
| 9bced4e | 2026-10-03T14:59:10-04:00 | INTENT: clarify Okta's reach and align claims with the PRD |
| 1ad74b8 | 2026-10-03T14:56:10-04:00 | Add INTENT.md describing purpose and wider context |
| 67d7c59 | 2026-10-03T14:49:48-04:00 | Initial commit: scaffold snow-cli repository |

---

## 4. Spec vs. Implementation Comparison

| Phase | Planned (spec) | Actual (git log) | Difference | Notes |
|---|---|---|---|---|
| Research / PRD | N/A | 14:49 to 16:19 -0400 (PRD authored manually) | n/a | before the flow started |
| Spec and review | N/A | 19:25 to 19:28 | n/a | 3 min |
| Foundation (WS-A) | per plan.md | 19:30 to 19:43 | n/a | gated by A-GATE freeze |
| Parallel streams B to E | per plan.md | 19:46 to 19:57 | n/a | merged at 19:56 |
| Integration, selftest, security | per plan.md | 19:57 to 20:07 | n/a | |
| Docs | per plan.md | 20:07 to 20:11 | n/a | |
| Review fixes (FR-R01..R14) | separate fix spec | 20:19 to 20:38 | n/a | |

The spec time estimates were not compared numerically; the specs record no usable wall-clock estimates, and spec dates (YYMMDD 261003) match git dates, so no date discrepancy was found.

**Phases skipped:** M0 spikes, ServiceNow scoped app, native Windows, release workflows (explicitly out of scope).
**Phases added:** the auto-review fix cycle (second spec, three streams).

---

## 5. Token / Message Usage

Exact token counts unavailable. Qualitatively: one orchestrator plus roughly 5 worker sub-agents for the build and 3 for the fixes, plus helper agents for reviews and docs. Many sub-agent calls ended without returning a report, which cost extra orchestrator turns to verify state from git.

---

## 6. Process Observations

### What worked well
- Test-first commit pairs (red then green) are visible throughout the log.
- Freezing the foundation (A-GATE) allowed clean parallel streams and conflict-free merges, with only one integration fix (47bfa6c area, test const clash).
- Per-stream request files kept frozen-file changes controlled and were folded into docs at the end.

### What caused delays or rework
- Sub-agents often returned no report, so the orchestrator re-derived status from git and the worktrees.
- Process slip: commit 1c501af (A8) was missing the required Co-Authored-By trailer; history was not rewritten.
- Some duplicated-looking commits (for example 10ab648 and 44e5ca6, E5 CI workflow; 289263f and e5a1816, B1/B2/B6) show overlapping work between parallel streams or a retried merge.
- A lint fix was needed in two streams (674b618, 06fadc0).
- Dashboard timing fields for steps 6, 7, 9, 11 were left blank.

### Recommendations for future runs
- Require sub-agents to commit and push a status line (or write to a shared notes file) so that missing replies do not matter.
- Add a commit-msg check or pre-push check for the trailer.
- Record start times for every step in the dashboard.
- Run golangci-lint before each stream's commit to avoid separate lint-fix commits.

---

## 7. Manual vs. Automated Comparison

**Estimated manual duration:** about 3 to 4 weeks for one senior developer (about 15 to 20 working days) to build and test this surface, including the Okta flows, policy and audit integration, docs, and a review-fix pass. Assumes familiarity with Go and agent-cli-core; excludes meetings and external review latency.
**Actual automated runtime:** about 80 minutes (19:20 to 20:40 -0400).
**Efficiency gain:** on the order of 30x to 50x in wall-clock time, rough estimate. Human review time on the output is not included and is still needed (live ServiceNow/Okta assumptions are unverified).
