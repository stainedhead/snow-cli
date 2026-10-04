# Verifying releases

There are no signed or published releases yet. No release workflow, checksums, signatures, SBOM or provenance are produced, so there is nothing to verify today. Apple signing and notarization and cosign signing are planned but not built.

What you can do now:

- Build from a source checkout you trust: `make build` (Go 1.27), which writes `bin/snow`. Pin the exact commit you built.
- Confirm what you built: `bin/snow version` prints the version, commit and build date stamped at build time.
- Confirm the command surface matches the agent skill document: `make skill-check` compares the generated document against the committed reference copy and fails on drift.
- Inspect the dependency set in `go.mod` and `go.sum`.

This page will describe checksum and signature checks once releases exist.
