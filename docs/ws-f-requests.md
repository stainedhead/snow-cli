# WS-F requests (human auth / Okta)

Hand-off notes for other streams; WS-H folds this into the ADR / technical-details and deletes the file (T-H8).

## For WS-H (docs)
- technical-details / ADR / user-docs/human-login.md: the Okta client now allows requests only to the issuer host (host:port, same scheme). Any redirect to another host or an https to http downgrade fails with `*httpx.ForbiddenHostError` (exit 4) and the body (`refresh_token`, `code`, `code_verifier`, revoke tokens) is never re-sent. Applies to login, refresh, device flow and logout/revoke, including an injected `Config.HTTP` client (its CheckRedirect is replaced).
- A refresh that hits a refused redirect returns the ForbiddenHostError itself (exit 4), not an exit-3 login-required error.
- PKCE login: a callback with a wrong or missing `state`, or with a Host header other than the listener address, is answered 400 and ignored; login keeps waiting until the timeout. A returned id_token must carry the matching nonce, otherwise login fails.
- State plainly: the id_token signature is not verified; the displayed `Subject` is an unverified claim and is never used for authorization.
- Removed behavior to drop from docs if mentioned: "a state mismatch aborts login".

## For WS-G (typed wiring, FR-R14)
- Replace `cli.Env.Keychain any` with `humanauth.Store` (interface: Kind, Load, Save, Delete). `humanauth.NewSource(cfg, store, profile)`, `humanauth.Logout(ctx, cfg, store, profile)` and `humanauth.StatusOf(store, profile, now)` already take the typed store; no humanauth change is needed.
- `humanauth.SelectStore(goos, wsl, insecure, path)` returns a `Store`; type the field on that.

## For the orchestrator
- Test seam added in oktafake (owned by WS-F): `QueueTokenFunc` for responses computed at request time (used to echo the PKCE nonce).
