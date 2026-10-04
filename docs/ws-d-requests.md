# WS-D requests and notes (human auth)

## Requests for frozen files
- `internal/app/app.go` calls `newKeychain()` / `newHumanTokenSource(prof)` without `Getenv`/home. WS-D workaround: `app/human.go` reads `SNOW_INSECURE_STORE` and the home directory itself (`storeFactory` test seam). No change needed unless the integration owner prefers passing `Options.Getenv`/`HomeDir`.
- `Env.Keychain` is `any`; the value is a `humanauth.Store`. Commands pass test seams through `Env.Extra["humanauth"]` (`*cli.HumanAuthDeps`); production uses defaults.

## Decisions
- `--insecure-store` is a flag on `auth login|status|logout` and affects only that command (the Env is built before command flags are parsed). Other commands read the same 0600 file only when `SNOW_INSECURE_STORE=1`. Default file: `~/.config/snow/credentials.json`.
- Real OS keychain backends (macOS, Linux Secret Service, WSL2) are fail-closed stubs returning `StoreUnavailableError` (exit 3) naming the escape hatch.
- `auth status` and `auth logout` are also refused in agent mode (exit 6), like `auth login`.
- Refresh failure from Okta -> `LoginRequiredError` (exit 3) showing the Okta error code/description; credentials are kept. Rotated refresh tokens are persisted atomically (file store: temp file + rename); a failed persist is an error.
- id_token claims (sub, nonce) are read without signature verification because the token comes directly from the token endpoint over TLS (OIDC Core 3.1.3.7).

## Assumptions (for docs/assumptions)
- ASSUMPTION(unverified against a real instance): the Okta app allows the `http://127.0.0.1:<port>/callback` loopback redirect (a fixed port may be required) and issues the `snow.user` scope (A-05). Tests: `TestPKCESuccess` (see `TestAssumptionLoopbackRedirect`).
- ASSUMPTION(unverified against a real instance): Okta endpoints are `<issuer>/v1/authorize|token|revoke|device/authorize`; token lifetime defaults to 3600s when `expires_in` is absent.

## Integration disposition
- `Options.Getenv`/`HomeDir` are not passed to `newHumanTokenSource`/`newKeychain`: `app/human.go` keeps its own `SNOW_INSECURE_STORE` and home lookup behind the `storeFactory` seam, which the integration tests use. Accepted, no change.
- `Env.Keychain` stays `any` (the value is a `humanauth.Store`); accepted.
- A-06 is documented as an optional fixed `Port` (0 = free port), not the 8765-8769 range; see docs/assumptions.md and `TestAssumptionA06FixedLoopbackPortWhenConfigured`.
