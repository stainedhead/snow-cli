# Research: snow-cli Auto Review Fixes
Date: 2026-10-03 | Source PRD: specs/261003-snow-cli-auto-review/snow-cli-auto-review-PRD.md

## Research Questions
1. FR-R02: can cross-process counters be derived from the core audit log, or does a locked state file keyed by agent+run id fit better? What does core audit.Record expose?
2. FR-R06: how does core policy/profile mode pin work, and is policy.allow_override consistent with spec D-i?
3. FR-R10: does core audit.Record accept an outcome label dry_run, or must the resource suffix be used?
4. FR-R01: what does core httpx offer for redirect refusal that can be reused for the Okta client?
5. FR-R05: which encoded-query operators does the ServiceNow Table API accept that the parser must allow (ASSUMPTION candidates)?

## Industry Standards
[TBD]
## Existing Implementations
[TBD]
## API Documentation
[TBD]
## Best Practices
[TBD]
## Open Questions
See spec.md.
## References
agent-cli-core v0.1.0 (read-only).
