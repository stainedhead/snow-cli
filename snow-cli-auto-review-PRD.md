# snow-cli Auto Review PRD

Branch: feat/snow-cli. Reviewed against specs/261003-snow-cli/spec.md and snow-cli-PRD.md. Method: read the implementation (config, sn adapter, humanauth, auditx, use cases, CLI, policies, docs) and ran `go build`, `go vet`, `gofmt -l`, `golangci-lint run` (0 issues) and `go test -race -cover ./...` (all green; usecase packages 95-100%, humanauth 88%, sn 90%, cli 72%, cmd/snow 43%).

## 1. Executive Summary

The implementation is structurally sound: dependencies point inward, the core envelope, exit codes, policy engine, audit logger and httpx host allowlist are used as intended, errors map per spec D-b, free text is wrapped as untrusted, tokens are redacted in the credential type, and the assumption register is test-enforced. No true blocker (P0) was found.

The material risks are guardrail gaps that make documented controls weaker than they read: Okta calls run on a plain HTTP client with no host or redirect restriction; per-hour and per-run write limits cannot work across separate CLI processes; catalog order reads and selftest write probes run outside the policy and audit guard; the encoded-query pass-through is a small blacklist; an agent can choose `--policy human`; retried create POSTs can duplicate after a lost response; and a reported "conflict" happens after the PATCH was applied.

Counts (verified against section 2): P0 = 0, P1 = 8 (FR-R01..FR-R08), P2 = 7 (FR-R09..FR-R15), total 15.

Non-goals: no change to agent-cli-core, the root skills/snow-cli.md, the ServiceNow scoped app, native Windows support, or release workflows; no live ServiceNow/Okta testing.

Priority rule: P0 = blocks any use or release (data loss, credential leak in default config, broken build); P1 = weakens a documented security or data-integrity control; P2 = hardening, accuracy, hygiene.

## 2. Functional Requirements (one per finding)

### P1

**FR-R01 (P1) Okta transport has no host allowlist or redirect refusal (token leakage).**
Evidence: `internal/humanauth/oauth.go` `Config.client()` returns a bare `http.Client{Timeout}`; `post`/`postRaw` send `refresh_token`, `code`, `code_verifier` and revoke tokens. Go follows 307/308 redirects and re-sends the form body to the redirect target. Spec D-a requires the Okta transport to have its own AllowedHosts.
Acceptance: (a) Okta requests go through an httpx-style client whose allowed hosts are exactly the issuer host; (b) any redirect to another host, or https to http downgrade, fails with the core `ForbiddenHostError` (exit 4) and sends no body; (c) tests with an httptest Okta that answers 307/308 to a second server prove the second server receives zero requests; (d) issuer scheme rules unchanged (https, loopback http for tests).

**FR-R02 (P1) Write rate limits (`per_hour`, `per_run`) are not enforced across invocations.**
Evidence: `app.go` builds `policy.NewEngine(pol, nil)` per process; core counters are in memory (`agent-cli-core/policy/engine.go`). Each `snow` call is one process, so `incident-create per_hour: 5` and `per_run: 10` never trigger. `user-docs/configuration.md` says `per_hour` "is counted by the policy engine", which a reader takes as working.
Acceptance: (a) either enforce limits across processes (for example derive counts from the audit log or a locked state file keyed by agent and run id) with tests that run N+1 invocations and see the last denied (exit 6, with retry hint); or (b) if enforcement is deferred, record it in docs/deferred.md and docs/core-change-requests.md, and rewrite the user-docs and generated skill text to state plainly that limits apply per process only. Option (a) preferred.

**FR-R03 (P1) `catalog order` reads the catalog outside the policy and audit guard.**
Evidence: `usecase/write/order.go` calls `s.Catalog.Item` and `s.Catalog.Variables` before `Guard.Run`; `cli/cmd_write.go` passes the raw `e.Catalog` port. These requests are neither policy-checked (`get`/`vars` on `catalog:item:*`) nor audited, and run even when `order` would be denied.
Acceptance: item resolution and variable validation run through the read service's guarded paths (`CatalogGet`/`CatalogVars`), or inside the order action after policy allow; a denied order sends zero requests; audit shows the reads. Test: policy that denies `vars` makes `catalog order` exit 6 with no HTTP.

**FR-R04 (P1) Selftest write probes and server-ACL probes bypass policy and audit.**
Evidence: `usecase/selftest/selftest.go` `opResolveServer`, `opUpdateServer`, `opServerList` call ports directly. With `--include-writes` a real resolve and update are sent with no pending audit record (D-c requires Block-mode audit for writes). If the server over-grants, the probe actually resolves the fixture incident, and reads of `sys_user` and `sys_properties` are unaudited.
Acceptance: every probe request is wrapped in an audited guard action with a distinct verb/resource (for example `selftest:probe-resolve`) while still skipping the client policy on purpose; write probes write the pending record first and abort if audit fails; tests assert audit records exist for each probe and that a failed audit sink sends no request. Document the deliberate policy bypass in the ADR.

**FR-R05 (P1) Encoded-query pass-through is a blacklist and escapes scoping and field allowlists.**
Evidence: `usecase/read/query.go` `ValidateQuery` rejects only `javascript:` and `<script`. A caller can add `^NQ` (new query = OR of whole sets) to escape built-in conditions such as `--mine`, `--state`, the `my work` assignee filter and class scoping (conditions are joined with `^`). Query and `--order-by` can reference fields outside the policy field allowlist (existence oracle on fields the policy hides, including dot-walks), and `table count --query` is never field-checked. `DYNAMIC` filter operators are not blocked. `TableParams.Values` also skips the default order when the text "ORDERBY" appears anywhere, even inside a value.
Acceptance: a parser accepts only `field OP value` clauses joined by `^` (and `^OR` inside a group), rejects `NQ`, `DYNAMIC`, `javascript`/`gs.` in any case or encoding, and rejects control characters; field names in clauses and in `--order-by` are submitted to the policy as requested fields (so an allowlist denial, exit 6, applies); the ORDERBY detection looks at clause position only; table-driven tests cover each rejected form and `count`.

**FR-R06 (P1) An agent can select a different policy with `--policy`.**
Evidence: `app.loadPolicy` lets the flag override `policy.path`; `--policy human` loads the permissive human policy (resolve allowed, impact 1, `table:*` reads) under an agent-mode profile. Spec D-i fixes mode from config only; policy is not given the same pin.
Acceptance: in agent mode `--policy` may not select a policy broader than the profile's (simplest: refused with exit 6 unless config sets an explicit `policy.allow_override: true`); the named policy must match the profile mode by default; tests show `snow --policy human incident resolve` on an agent profile is denied with no HTTP request. Document the rule in user-docs/configuration.md.

**FR-R07 (P1) Retried create POSTs can create duplicates after a lost response.**
Evidence: `sn/writes_incident.go` marks the create POST `SafeToRetry` after a single dedupe miss; the core transport then re-sends on 429/5xx/transport errors with no new dedupe lookup. If the first POST succeeded but the response was lost or a gateway returned 502/503 after commit, the retry creates a second incident. The dedupe key is not server-unique, so the retry is not truly safe.
Acceptance: before every re-send of a create POST the adapter re-runs `FindByCorrelation` (own bounded retry loop instead of blind transport retry); a hit returns `deduplicated: true`; tests with the fake server: first POST commits then returns 503, second attempt must find the record and send no second POST (POST count asserted).

**FR-R08 (P1) Update conflict is reported after the write was applied; the precondition is never used.**
Evidence: `sn/writes_common.go` `wGuardedPatch` PATCHes first, then returns `ConflictError` (exit 7) if `sys_mod_count` jumped by more than one. The use cases always pass `ExpectedModCount` 0, so the pre-write check never runs. The audit outcome is "error" and the hint says "re-read and retry", yet the PATCH (including journal entries) was applied; a retry duplicates work notes.
Acceptance: (a) the use case reads `sys_mod_count` and passes it as `ExpectedModCount` for the actual write when the caller supplied a previously read value, and otherwise the adapter compares immediately before PATCH; (b) the post-write conflict message states that the change was applied and names the record number; (c) the audit outcome distinguishes applied-with-conflict; (d) tests for pre-write mismatch (no PATCH sent) and post-write advance (message says applied).

### P2

**FR-R09 (P2) Human login hardening gaps.**
Evidence: `humanauth/pkce.go`: any local request to `/callback` with a wrong or missing state aborts the login (local denial of service); no Host header check on the loopback listener (DNS rebinding); the nonce is compared only when the id_token carries one, so a token with no nonce passes; the id_token signature is not verified (documented, but unverified claims feed `Subject`).
Acceptance: a bad-state request is ignored (login keeps waiting until timeout) and answered 400; requests whose Host is not the listener address are refused; when an id_token is present a missing nonce fails; tests for each; subject display documented as unverified.

**FR-R10 (P2) Audit records cannot identify the record, fields, or dry-run.**
Evidence: core `audit.Record` carries verb and resource only; snow passes resource "incident" without the number or sys_id, and a `--dry-run` shows as pending then "ok" with HTTP status 0, like a send. A reviewer cannot tell which record changed or that nothing was sent.
Acceptance: use a resource form that includes the target reference (for example `incident:INC0010001`, task and catalog item ids) while keeping policy matching on the base resource; add an outcome label such as `dry_run` for previews; if core must change, add to docs/core-change-requests.md and use the resource suffix meanwhile; tests assert the audit lines.

**FR-R11 (P2) Idempotency edge cases and input validation.**
Evidence: `idempotency.Key` buckets by hour (a retry across the hour boundary creates a new incident); the CI is the raw argument (name versus sys_id gives different keys); dedupe looks only at `active=true` (a record resolved within the hour is duplicated); check-then-create is not atomic across concurrent runs. A bad `--idempotency-key` (for example containing a space) fails in `wSafeRef` with a plain error (exit 1) after the pending audit record, instead of validation exit 9 before the guard.
Acceptance: validate the explicit key's character set in `CreateInput` validation (exit 9, no audit, no request); normalise CI to the resolved sys_id or document that names and ids differ; widen the dedupe window (previous bucket) or drop `active=true` for keys younger than the window; document the concurrency limit; tests for each.

**FR-R12 (P2) Untrusted marking is a fixed field list.**
Evidence: `usecase/read/present.go` wraps only nine named fields. Catalog variable labels and choices, CI `name`/`short_description` from other CMDB tables, and any custom `u_*` text are emitted as plain strings; `presentItem` wraps `short_description` but not `name`; approval and other journal-like fields are not listed.
Acceptance: invert the rule: treat every string field as untrusted except a documented set of structured fields (sys_id, number, state codes, timestamps, class names, references); mark catalog variable text; golden tests with injection text in each place.

**FR-R13 (P2) Read paths fetch fields never submitted to policy; assorted error-mapping nits.**
Evidence: `cmdb ci related` and `cmdb app` fetch `parent.name`, `child.name`, `type.name`, `*.sys_class_name` from `cmdb_rel_ci`, but the policy request carries only `sys_id,name,sys_class_name`; traversal reads on `cmdb_rel_ci` are not checked as `table:cmdb_rel_ci`. `sn.mapStatus` turns every unclassified 5xx into `RateLimitedError{Attempts:1}` (misleading "rate limited" text for 500/501); `Client.Do` silently truncates bodies at 8 MB, which surfaces as a JSON parse error.
Acceptance: policy requests list every field actually fetched (including dot-walked) and the related-table read is checked; a 5xx that is not rate limiting maps to exit 8 with an accurate message; oversize bodies return a clear "response too large" error (exit 1); tests for each.

**FR-R14 (P2) Type safety and test gaps on security-sensitive adapters.**
Evidence: `cli.Env.Keychain any` and `Env.Extra map[string]any` carry typed services and are asserted at run time (a missing entry becomes "not wired"); `FieldAllowlister` is an optional interface asserted on the Guard, so a guard without it silently drops allowlist substitution; `cli` imports the `sn` adapter only to adapt policy errors. Coverage: humanauth 88%, cli 72%, cmd/snow 43%, app 87%, sn 90% (NFR-003 only mandates domain and use cases, but these hold auth, redirect and token code).
Acceptance: replace `Keychain any` with `humanauth.Store` (or a port interface in usecase), give the task fetcher, selftest service and human deps typed fields on `Env`, make `AllowedFields` part of the `Guard` port, move policy-error adaptation behind a port; raise humanauth and sn to at least 90% with tests for refresh rotation failure, store errors and the new FR-R01/R09 paths.

**FR-R15 (P2) Documentation accuracy and hygiene.**
Evidence: `user-docs/configuration.md` line about `per_hour` implies working enforcement (see FR-R02); `docs/ws-b-requests.md` .. `ws-e-requests.md` are internal stream hand-off notes left in docs/; README links into `specs/261003-snow-cli/` which will move on archive; docs describe retry-safety of create without the lost-response caveat (FR-R07) and the conflict wording without "already applied" (FR-R08). user-docs themselves contain no links into specs and no design material (rule satisfied).
Acceptance: (a) every docs/technical-details.md, ADR and user-docs statement touched by FR-R01..R14 matches the final code (checked by a docs review pass listing each changed statement); (b) docs/ws-*-requests.md are folded into the ADR or deleted, none remain; (c) README has no link to the pre-archive `specs/261003-snow-cli/` path (grep returns none; links use specs/archive/); (d) docs/assumptions.md lists every new ASSUMPTION and the assumption test still passes; (e) user-docs contain no links into specs/ and no design material (grep check).

## 3. Guidance for the fix phase

- Use TDD: for each requirement write the failing test first (httptest Okta with redirects, fake server POST counters, audit sink failure injection, policy files in tests), see it red, then implement; keep usecase coverage at or above 90%.
- Review each fix: one reviewer pass per fix branch before merge (diff against the acceptance criteria above), including a security read of FR-R01, R03, R04, R05, R06; run `go build ./... && go vet ./... && go test -race ./...`, `gofmt -l .`, `golangci-lint run`.
- Use agent teammates: assign one agent per workstream below and a separate reviewer agent; agents report back with the exact commits.
- Use git worktrees for parallel workstreams, each on its own branch from feat/snow-cli, merged back after review. Suggested streams: (A) transport and auth: R01, R09; (B) guard, policy and audit: R02, R03, R04, R06, R10; (C) query and read paths: R05, R12, R13; (D) writes and idempotency: R07, R08, R11; (E) types, tests and docs: R14, R15 (last, after A-D merge). Streams A, C and D are independent; B touches `auditx` and `app` and should merge before E.
- Do not edit agent-cli-core (record needs in docs/core-change-requests.md); do not touch the root skills/snow-cli.md.

## 4. Non-functional requirements

- Security: FR-R01, R03-R06, R09, R12 close token-leak, bypass and injection paths; no new secret appears in logs or audit.
- Reliability: FR-R07, R08, R11 make retries and conflicts safe; no change may increase duplicate-write risk.
- Performance: no added round trips beyond one pre-write read (FR-R08) and one dedupe lookup per create retry (FR-R07).
- Observability: FR-R04, R10 make every request auditable; dry-run is distinguishable.
- Quality gates: build, vet, gofmt, golangci-lint, `go test -race ./...` green; usecase coverage >= 90%.

## 5. Dependencies

agent-cli-core v0.1.0 (read-only; changes go to docs/core-change-requests.md); Go 1.27; golangci-lint v2; httptest fakes only.

## 6. Open questions

1. FR-R02: file-based or audit-log-derived cross-process counters, or defer with docs (option b)? Decide before stream B starts.
2. FR-R06: is an explicit `policy.allow_override` config key acceptable, or should `--policy` be removed in agent mode?
3. FR-R10: does core accept an outcome label `dry_run`, or is the resource-suffix workaround permanent?
