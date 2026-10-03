# Research: snow-cli (2026-10-03)

Source PRD: `specs/261003-snow-cli/snow-cli-PRD.md`

## Research Questions
1. RQ-1: How does core output truncation treat object data containing an `items` array (spec D-e, R-04)? Verify by reading core `output` tests; fall back to snow-side trimming.
2. RQ-2: Does catalog `order_now` expose a dedupe-capable key on `sc_request`/`sc_req_item` (A-07)? Needs a real instance (M0 checklist).
3. RQ-3: Is the Okta loopback redirect a port range or fixed set (A-06)? Needs Okta (M0).
4. RQ-4: Human token to ServiceNow: access vs id token (A-05)? M0.
5. RQ-5: Exact `submit_producer` response shape and ACL-denied read status codes (A-03, A-08)? M0.

## Industry Standards
RFC 8252 (native app OAuth), RFC 7636 (PKCE), RFC 8628 (device grant). Details [TBD].
## Existing Implementations
Core packages in agent-cli-core v0.1.0 (policy, output, audit, httpx, selftest, docgen, auth). Sibling CLI outlook-cli (not touched).
## API Documentation
ServiceNow Table, Aggregate, Service Catalog APIs (see PRD appendix). [TBD]
## Best Practices
Fail closed; no secrets in output; fakes over live systems.
## Open Questions
PRD section 14 items 1-7 remain open and are out of the build's control.
## References
PRD appendix; core docs.
