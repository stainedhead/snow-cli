# Deferred and not built

Everything here is intentionally absent from this release. Nothing is stubbed to look like it works unless the entry says it fails closed.

| Item | State in this release | Why / what is needed |
|---|---|---|
| Real `agent-okta-d` daemon adapter | Fail-closed stub: agent-mode commands that need a token exit 3 and the message names the socket path (ADR-002). `agent-okta-d` is not in `go.mod`. | The adapter and the `agent-okta-d` `pkg/client` release do not exist yet. Replace `newDaemonClient()` in the composition root and add the require. |
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
| Truncation of object data in the core envelope (R-04) | `read.Fit` trims `items` and reports `data.truncated` / `data.page.next_offset`; `meta.truncated` and `meta.next_offset` are not set for object data | Needs a core change (`output.fit` honouring a caller-set `Meta.Truncated`/`NextOffset` or cutting `items`); see docs/ws-b-requests.md. |
