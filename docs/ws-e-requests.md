# WS-E requests for frozen files

Files owned by another stream or frozen after A-GATE; WS-E worked around each.

1. `cmd/snow/main.go` (frozen): set `NamedPolicy: policies.Named` in the `app.Options` literal in `main()` (import `github.com/stainedhead/snow-cli/policies`). `app.loadPolicy` already calls `Options.NamedPolicy`; until main passes it, `--policy agent|human` exits 2 with "built-in policy is not available in this build". `policies.Named(name)` returns the embedded bytes and an error for any other name. Workaround in WS-E: none needed for tests (they call `policies.Named` directly); the integration owner makes the one-line change at I1.
2. `Makefile` (frozen): add a `skill-check` target if desired: `go run ./cmd/snow skill generate --out dist/snow-cli.md && diff -u internal/cli/testdata/skill.golden.md dist/snow-cli.md`. CI already runs those two commands inline, so this is optional.
3. Skill golden at integration: `internal/cli/testdata/skill.golden.md` is generated from the commands registered on this branch (core plus `skill generate`). After B, C, D, E are merged, run `go test ./internal/cli -run TestSkillGolden -update`, review the diff and commit it, otherwise `TestSkillGolden` and the CI drift step fail by design.
4. CI cross-build: the existing workflow set `CGO_ENABLED=1` for darwin; WS-E changed it to 0 for every target to match NFR-005 (`CGO_ENABLED=0`). If a real keychain backend later needs cgo on darwin, revisit.
