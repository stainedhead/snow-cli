# Human login

Human mode (`mode: human`) runs the same commands as you, with your own ServiceNow roles. It uses Okta OAuth 2.0 authorization code with PKCE. Agent profiles cannot sign in (`snow auth login` exits 6).

Status: tested against a fake Okta only. Not verified with a real Okta tenant or ServiceNow instance.

## Prerequisites

- A profile with `mode: human`, `instance.host`, `okta.issuer` and `okta.client_id` (see [configuration](configuration.md)).
- An Okta native app for the client id, with PKCE, the loopback redirect `http://127.0.0.1:<port>/callback`, refresh tokens and (for `--device`) the device authorization grant enabled. `snow` requests the scopes `openid profile email offline_access snow.user`.
- `snow` picks a free loopback port at random and has no setting to pin one. If your Okta app only accepts fixed redirect ports, browser login will be rejected (unverified); use `--device`.
- ServiceNow must trust the Okta token you send. If calls return 401 (exit 3), try `okta.token_type: id` instead of `access` (unverified which your instance wants).

## Sign in

```
snow auth login            # opens your browser; prints the URL if it cannot
snow auth login --device   # prints a verification URL and code; for headless sessions
snow auth status
snow auth logout
```

- `status` shows mode, issuer, subject and token expiry, never token values. Exit 3 when not signed in.
- `logout` revokes the tokens at Okta and deletes the stored entry even if revocation fails; both outcomes are reported.
- Tokens refresh silently, including refresh-token rotation. If refresh fails, the Okta error is shown with exit 3; sign in again.
- MFA is enforced by Okta, not by `snow`.

## Where credentials are stored

The real OS keychain backends (macOS Keychain, Linux Secret Service) and a WSL2 store are not built in this release. Without an opt-in, `auth login` and every human-mode command fail with exit 3:

```
credential store "macos-keychain" is unavailable: the macOS Keychain backend is not built into this version
```

To proceed, opt in to a plain file store explicitly, on every command that needs credentials:

```
snow auth login --insecure-store          # or: export SNOW_INSECURE_STORE=1
```

Tokens are then kept in `~/.config/snow/credentials.json` (mode 0600, unencrypted). `auth status` and `auth logout` take the same flag. Use it only on machines you trust. Windows is not supported natively; use the Linux build under WSL2.

## Writes in human mode

Writes ask for confirmation. Use `--yes` to skip, or `--dry-run` to preview. See [usage](usage.md#writing).
