# Dockyard v1.0.0 — release dry-run transcripts

This directory holds the captured proofs the Phase 30 release pipeline
works end to end before the actual `v1.0.0` tag is pushed. They are the
maintainer-side artifact backing acceptance criterion sub-goal F of
`docs/plans/phase-30-v1-cut.md` and are required-present by
`scripts/smoke/phase-30.sh`.

Captures (run on 2026-05-25, host `darwin/arm64`):

- **`cross-compile-matrix.txt`** — the output of one full run of
  `go run ./internal/releasebuild/cmd/releasebuild -version
  v1.0.0-dryrun -output …` against the v1 codebase. Every target in
  the RFC §14 matrix (darwin / linux / windows × amd64 / arm64)
  produced a CGo-free, statically-linked binary and a SHA-256
  sidecar; the aggregate `checksums.txt` round-trips through
  `shasum -a 256 -c` cleanly; the Linux artifact's `file(1)` output
  confirms "statically linked" and the Windows artifact's confirms
  PE32+ for x86-64 (i.e. the cross-compile is producing the right
  shape per target).
- **`binary-help.txt`** — the `--help` output of one cross-compiled
  artifact (the host's `darwin/arm64` binary, the only target the
  host kernel will load directly). Confirms the cross-compiler
  produces a binary that boots, recognises the full cobra command
  tree (the nine V1 verbs — `build` / `completion` / `dev` /
  `generate` / `help` / `inspect` / `install` / `new` / `run` /
  `test` / `validate`), and prints the framework's mission
  paragraph.
- **`preflight.txt`** — the output of `make preflight` against the
  v1 codebase. Captures the same release gate the
  `.github/workflows/release.yml` workflow runs as its first step.

When the actual `v1.0.0` tag is pushed, the GitHub Actions run page
becomes the canonical record of what the release workflow did; these
in-tree captures stay as the pre-tag proof + a regression baseline. A
future release-engineering pass that supersedes Phase 30's dry-run
posture may overwrite these transcripts with the actual run's
transcripts (same shape, real source).

## How to reproduce

From a clean checkout of the Dockyard repo:

```sh
mkdir -p /tmp/dockyard-release-fullmatrix
go run ./internal/releasebuild/cmd/releasebuild \
  -version v1.0.0-dryrun \
  -output /tmp/dockyard-release-fullmatrix \
  2>&1 | tee docs/release/v1.0.0/cross-compile-matrix.txt
(cd /tmp/dockyard-release-fullmatrix && shasum -a 256 -c checksums.txt) \
  | tee -a docs/release/v1.0.0/cross-compile-matrix.txt

# pick the artifact your host kernel can load (here: darwin/arm64)
/tmp/dockyard-release-fullmatrix/dockyard-v1.0.0-dryrun-darwin-arm64 --help \
  > docs/release/v1.0.0/binary-help.txt 2>&1

make preflight 2>&1 | tee docs/release/v1.0.0/preflight.txt
```

The whole sequence takes ~5 minutes on a developer laptop. CI runs the
equivalent on every tag push via the release workflow.
