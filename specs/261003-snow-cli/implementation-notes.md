# Implementation Notes: snow-cli (2026-10-03)

Purpose: record decisions, edge cases and deviations as work proceeds; update after each task.

## Technical Decisions
## Edge Cases & Solutions
## Deviations from Plan
## Lessons Learned

## Spec review (step 2) findings
- Verified against agent-cli-core v0.1.0 source: spec cites are accurate. Clarified: `policy.DeniedError` does not implement `output.CategoryError`, so snow adapts it (A5). `audit.Record` has fixed fields; "pending" is an `Outcome` label. `httpx.Config.Redactor` uses a core-internal type; leave nil.
- tasks.md/plan.md rewritten: test-first ordering, exclusive package ownership per stream, branch/freeze rules, shared test fakes (snfake/oktafake) in WS-A, selftest moved to integration.
