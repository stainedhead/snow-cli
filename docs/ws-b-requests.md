# WS-B (read path) requests and notes

Requests to frozen or other-stream files (applied by the integration owner) and decisions the read path made.

## Requests
1. **Guard field allowlist hook (internal/auditx, WS-A).** `usecase/read` substitutes the policy field allowlist when `--fields` is omitted, so a read never goes outside it (spec D-f). It looks for an optional interface on `Env.Guard`: `AllowedFields(verb, resource string) []string` (type `read.FieldAllowlister`). `auditx.Guard` does not implement it yet; add it by exposing the matching allow rule's `fields` from the `policy.Policy` the engine holds. Until then the read path falls back to built-in default field lists, which are still checked against the policy (a default outside the allowlist is a policy denial: the caller passes `--fields`).
2. **Core: truncation of object data (R-04, core-change-requests.md).** Verified in core `output.fit`: only top-level arrays and strings are cut; an object holding an `items` array that exceeds `--max-bytes` returns `ErrBoundTooSmall` (exit 2), and `Write` resets `meta.truncated`/`meta.next_offset` for object data. snow therefore trims `items` itself (`read.Fit`) and reports truncation inside `data` (`data.truncated: true`, `data.page.next_offset` = absolute `sysparm_offset` of the first dropped item) and sets `meta.count`. Request to core: honour a caller-set `Meta.Truncated`/`NextOffset` for object data (or add an `items`-aware cut) so M1 acceptance "truncation sets meta.truncated and meta.next_offset" can be met literally.
3. **Policy rules E1 must ship for the read verbs** (resources are the D-f vocabulary): `get`/`list` on `incident`, `request`, `ritm`, `task`, `problem`, `change`; `get`/`search`/`related` on `cmdb:ci`; `get` on `cmdb:app`; `search` on `catalog:search`; `get`/`vars` on `catalog:item:*`; `get`/`list`/`count` on `table:<name>`; **`list` on `table:task` for `snow my work`**; `cmdb ci related` is one policy check (`related`, `cmdb:ci`) that also covers its `cmdb_rel_ci` reads (no separate `table:cmdb_rel_ci` check). A catalog item given by name performs a `search catalog:search` check first, then the `get`/`vars` check on the resolved `catalog:item:<sys_id>`.
4. **Human mode read test** (M1 "agent and human profile both read") needs WS-D's human TokenSource; the read path is mode-agnostic (only the token source differs) and is tested here with the agent profile and `authtest.Fake`. Integration (I2) should add the human-profile run.

## Decisions
- Free-text fields marked `output.Untrusted`: description, short_description, work_notes, comments, comments_and_work_notes, additional_comments, close_notes, resolution_notes, justification; author = record `sys_updated_by`, timestamp = `sys_updated_on` (UTC) when those fields were read. Catalog item `short_description` is marked without author.
- Default page size 25 (`read.DefaultLimit`), clamped by `limits.max_results`.
- `--order-by f` / `-f` always gets `^ORDERBYsys_id` as tiebreak; otherwise `sn.DefaultOrder`.
- `--mine`: `assigned_to.user_name=<whoami user>` (`requested_for` for `request`). `my work` resolves the user through the Identity port inside the audited read (not a separate `whoami` policy check).
- `cmdb ci related`: `down` = relationships where the CI is the parent, `up` = where it is the child; default depth 2, max 5; node cap 200; level-by-level batched queries; cycle-safe.
- `cmdb app` reads display values so owner and support group are names.
- `cmdb ci search --class` accepts names matching `cmdb_ci*` only (name-prefix check, no class hierarchy cache).
- Encoded query validation refuses `javascript:` and `<script` (case-insensitive); filter values containing `^` or line breaks are refused (clause injection).
- Command helper files: `cmdb ci get` etc. live in `internal/cli/cmd_read.go` only.

## Assumptions introduced (for the register; each has an Assumption-named test)
- B-A1 (relates to A-11): Aggregate API count response `{"result":{"stats":{"count":"N"}}}` (`sn.Tables.Count`).
- B-A2: Service Catalog API response shapes for `items`, `items/{id}`, `items/{id}/variables` and tolerant parsing of category/type/mandatory/choices (`sn.Catalog`).
- B-A3 (relates to A-12): the Table API resolves dot-walked fields in `sysparm_fields` (`parent.name`, `child.sys_class_name`, `type.name`) for relationship names.
- B-A4 (relates to A-03): CI classes recognised by the `cmdb_ci` name prefix.

## Integration disposition
1. Applied: `auditx.Guard.AllowedFields(verb, resource)` returns the allowlist of the first matching allow rule (a matching deny returns none); `TestAllowedFields`, and `TestIntegrationM1ReadAllowlistedDeniedAndServerStatuses` asserts the shipped policy allowlist reaches `sysparm_fields`.
2. Deferred (core change, R-04 in core-change-requests.md): truncation of object data is still reported inside `data` (`data.truncated`, `data.page.next_offset`); `meta.truncated` and `meta.next_offset` stay false/null for object data. See docs/deferred.md. Test: `TestIntegrationM1TruncationAndACLFilteredPage`.
3. Applied/verified: `TestIntegrationShippedPoliciesAllowEveryReadCommand` runs every read command under the shipped agent and human policies (no default deny).
4. Applied: human-profile read run (`TestIntegrationM1HumanProfileReadsThroughTheSameUseCases`, `...HumanExpiredTokenRefreshesAgainstOkta`).
