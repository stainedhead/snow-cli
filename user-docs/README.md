# user-docs

Guides for adopting, configuring and using `snow`.

- [Getting started](getting-started.md): build, configure, first commands
- [Configuration reference](configuration.md): `config.yaml` keys, policy files, environment variables
- [Usage examples](usage.md): every command with examples and output notes
- [Human login](human-login.md): Okta sign-in, device flow, credential storage
- [Troubleshooting](troubleshooting.md): exit codes, common failures
- [Verifying releases](verifying-releases.md): what can and cannot be verified today

Status: implemented and tested against fakes and `httptest` servers only. It has not been run against a real ServiceNow instance or Okta tenant. Behaviour that depends on your instance is marked "unverified" in these guides.
