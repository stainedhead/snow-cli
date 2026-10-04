# Deferred and not built

Everything here is intentionally absent from this release. Nothing is stubbed to look like it works unless the entry says it fails closed.

| Item | State in this release | Why / what is needed |
|---|---|---|
| Other agent-cli-core v0.2 features (optional follow-ups) | Not adopted: page token, clock, and the rest of the v0.2 additions. Only the `auth/oktad` adapter is wired (ADR-017). | Adopt one at a time in separate PRs when a need appears. |
| Agent mode against a real daemon | The adapter is tested against the daemon's fake (`clienttest`) on a real unix socket, not a running `agent-okta-d` and ServiceNow instance. | Needs the sub-production environment used for live selftest. |
| Redaction hook (PRD 11, regex/field masks before output) | Not built | Field allowlists in policy are the only data-classification control for now. |
| Signed or detached-signature policy | Not built; the config rejects any `policy.signature` key so nothing pretends to verify | Core CORE-POL-7 is unbuilt. |
| Release workflows (tarball, OCI image, SBOM, provenance, cosign, Apple signing and notarization) | Not built; only PR CI exists | Release signing depends on PRD 15.8 items 1 to 3. Unsigned builds would be pre-release. |
| `change create` (P2) | Command absent; `change get\|list` only | Open PRD question 1. |
| CMDB Instance API (P2) | Not used; `cmdb ci related` uses the Table API on `cmdb_rel_ci` | Role requirements vary by ServiceNow release (A-12). |
| Attachments | Not built | Out of scope for v1. |
| Real OS keychain backends (macOS Keychain, Linux Secret Service) and WSL2 storage | Fail-closed stubs with a clear error unless `--insecure-store` is passed; fakes in tests | Backend selection and WSL2 behaviour are spike items (A-13). |
| Native Windows | Not a target; Windows users run the Linux build under WSL2 | PRD 11. |
| The ServiceNow scoped application (roles, ACLs, `whoami` endpoint, record producer) | Not in this repository | Owner undecided (PRD 14 question 3); the CLI assumes its shape (A-02, A-08). |
| Automated update of the root skill | Manual pull request per release | `GITHUB_TOKEN` cannot write to another repository (PRD 16.1). |
| Shared write counter across write rules (`max_writes_per_run`) | Approximated with `rate_limit.per_run` on each write rule | Core change request. |
| Live `selftest` in CI | On demand only, never in PR CI (BLD-4) | Needs a sub-production instance with short-lived credentials. |
| Branch protection / required status check (BLD-5) | Not configured | Separate step after CI is green. |
| Truncation of object data in the core envelope (R-04) | `read.Fit` trims `items` and reports `data.truncated` / `data.page.next_offset`; `meta.truncated` and `meta.next_offset` are not set for object data | Needs a core change (`output.fit` honouring a caller-set `Meta.Truncated`/`NextOffset` or cutting `items`); see CR-04. |
| Fixed loopback port for the Okta redirect | The PKCE code accepts a `Port` option (0 = random free port) but no config key sets it: browser login binds a random free port; `--device` is the fallback | The Okta redirect rules are unverified (A-06); a config key is a small addition once known. |
| Cross-rule write counter and core-level cross-process limits | FR-R02 is met by snow's own state file (`auditx.StateLimiter`, unix only; fails closed elsewhere); no counter is shared across rules | CR-01 and CR-11. |
| First-class audit fields for target ref and `dry_run`/`applied_conflict` outcomes | Met with the `<base>:<ref>` resource suffix and outcome label strings | CR-10. |
| Atomic create across concurrent runs (FR-R07/R11) | Not atomic: two simultaneous runs can both pass the dedupe lookup | Needs a server-side unique key (A-07 family); documented in user-docs/usage.md. |
| id_token signature verification (FR-R09) | Not verified; the displayed subject is an unverified claim never used for authorization | Needs JWKS handling; low value while ServiceNow is the authority. |
