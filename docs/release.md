# Release checklist

Local verification:

1. Run `gofmt`, `go vet ./...`, `go test -race ./...`, and `./scripts/release-smoke.sh`.
2. Build all GoReleaser targets and inspect `dist/` archives, checksums, SBOMs,
   and provenance.
3. Confirm `CHANGELOG.md` has the release date and run `dt-task version` from
   the built artifact.

Current local evidence: `BenchmarkListTasks1000` ~54 ms on Apple M1; stripped
native binary ~3.95 MB. Race, vet, cross-build, skill validation, and smoke
checks pass.

Remote publication requires an authenticated GitHub CLI account for
`deepaktiwari09`, a public `deepaktiwari09/dt-task-cli` repository, and
`HOMEBREW_TAP_TOKEN` access to `deepaktiwari09/homebrew-tap`. The release
workflow publishes GitHub archives and the Homebrew formula; the CLI itself
never makes runtime network requests.

Use `.env.example` as the non-secret variable template. Keep real `.env` files
ignored and outside commits.

Post-release smoke checks: install the archive, run `init`, `capture`, `day start`,
`doctor`, `skill`, and `skill --status`; verify both skill destinations have
matching checksums.
