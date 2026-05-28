# Releasing Dockyard

This document is the operational manual for cutting a Dockyard release.
It is consumed by maintainers; a developer building *with* Dockyard does
not need to read it.

The companion artifacts are:

- [`CHANGELOG.md`](../CHANGELOG.md) — the canonical release-notes source.
  Each version's section is the body of the matching GitHub Release.
- [`.github/workflows/release.yml`](../.github/workflows/release.yml) — the
  workflow that runs when a `v*` tag is pushed.
- [`internal/releasebuild`](../internal/releasebuild/) — the in-repo
  cross-compile driver the workflow consumes.
- [`internal/changelogx`](../internal/changelogx/) — the in-repo
  CHANGELOG section extractor the workflow consumes.
- [`docs/V2-BACKLOG.md`](V2-BACKLOG.md) — the consolidated post-V1
  deferral list.

---

## Semver policy

Dockyard follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Concretely, going forward from v1.0.0:

- **Major (`vX.0.0`)** — a breaking change. Either: (a) a backwards-
  incompatible change to a public runtime API or to a Go-shaped contract
  (`runtime/server`, `runtime/tool`, `runtime/apps`, `runtime/tasks`,
  `runtime/obs`, `runtime/store`); (b) a breaking change to the
  `dockyard.app.yaml` manifest schema (field removed / renamed / a new
  required field added); (c) a breaking change to the `obs/v1` event
  shape (a structural change requires bumping the schema version, per
  AGENTS.md §8 — that bump rides with a Dockyard major); (d) a CLI verb
  removed or fundamentally re-shaped; (e) a change to one of the four
  binding properties (P1–P4 — RFC §1; this is governed by an RFC PR,
  not just a major release).
- **Minor (`v1.Y.0`)** — an additive feature: a new CLI verb, a new
  runtime API, a new manifest field with a sensible default, a new
  template, a new agent skill. Existing users see no behaviour change.
- **Patch (`v1.0.Z`)** — a bug fix, a documentation fix, a security
  fix, a dependency bump that does not change Dockyard's behaviour.
  Existing users see no API or wire change.

Pre-releases follow semver's pre-release form: `v1.1.0-rc.1`,
`v2.0.0-beta.1`. The release pipeline accepts them; the GitHub
Release for a pre-release is marked as pre-release (not "Latest").

> When in doubt, lean breaking. A surprise breakage in a minor release
> is worse than a major version bump that a user accepts cleanly. If
> the change touches an `obs/v1` event shape, default to major and
> bump `obs/v1`'s `schema_version` in the same PR.

---

## Cutting a release

Releases are tag-driven. Pushing a tag matching `v*` to `main` triggers
`.github/workflows/release.yml`, which runs the preflight gate, builds
the cross-compile matrix, computes the checksums, extracts the matching
CHANGELOG section, and creates (or updates) the GitHub Release.

### Pre-flight (do this before tagging)

1. **`main` is green.** Confirm the latest `main` push on
   [`actions/workflows/ci.yml`](https://github.com/hurtener/dockyard/actions)
   is ✅. A red `main` must be fixed before tagging — the release
   workflow will re-run `make preflight` and fail anyway, but knowing
   up front saves a wasted release run.

2. **CHANGELOG carries the entry.** The version you're about to tag
   must have a matching `## [<version>] - <YYYY-MM-DD>` section in
   [`CHANGELOG.md`](../CHANGELOG.md). The `internal/changelogx`
   extractor exits non-zero on a missing section; the workflow's
   "extract release notes" step will fail and skip the release
   creation. To verify locally:

   ```bash
   go run ./internal/changelogx/cmd/changelogx -version v1.0.0
   ```

   The command prints the section body to stdout. An empty output
   or a non-zero exit means CHANGELOG is not ready.

3. **Optional: a `workflow_dispatch` dry-run.** Run the release
   workflow against `main` via the Actions UI, with the dispatch
   `dry-run-version` set to something like `v0.0.0-dryrun`. The
   workflow narrows the matrix to the runner's host target and
   stops at artifact upload (no GitHub Release is created). A
   maintainer can download the artifact from the workflow run page
   and audit the binary + checksums before tagging for real.

### The tag-push sequence

From a clean local `main`:

```sh
git checkout main
git pull --ff-only origin main

# Sanity: CHANGELOG has the matching section.
go run ./internal/changelogx/cmd/changelogx -version v1.0.0 >/dev/null

# Tag and push. The tag message mirrors the CHANGELOG section's
# headline — a short, informative line, not a release-notes dump.
git tag -a v1.0.0 -m "Dockyard v1.0.0"
git push origin v1.0.0
```

The push triggers `.github/workflows/release.yml`. The workflow:

1. **preflight** — runs `make preflight` (build, every per-phase smoke,
   drift-audit). A failed preflight stops the release before any
   artifact is built.
2. **build** — drives `internal/releasebuild` against the
   `cmd/dockyard` main package over the RFC §14 cross-compile matrix
   (darwin / linux / windows × amd64 / arm64). Each artifact is a
   CGo-free, statically-linked binary; the workflow verifies the
   per-artifact checksums (`sha256sum -c checksums.txt`) before
   moving on.
3. **release** — downloads the build artifacts, re-verifies the
   checksums, extracts the matching CHANGELOG section as the release
   body, and creates / updates the GitHub Release via
   `softprops/action-gh-release`. The action is idempotent on
   re-run: a re-run uploads any artifacts not already present and
   updates the body; it never creates a duplicate release.

The release body is the hand-authored CHANGELOG section followed by an
auto-generated **commit supplement** (D-167): the build job runs
`changelogx -supplement` over the `previous-tag..this-tag` commit range
and appends a Conventional-Commits-derived list (feat → Added, fix →
Fixed, everything else → Changed; `docs`/`chore`/`test`/`ci`/`build`/`style`
are dropped as noise) below the prose. The hand-authored section stays
the canonical narrative — the supplement is the "what landed in detail"
companion. It is appended only on a real tag push and only when a
previous tag exists; a dry-run keeps the bare extracted section. Preview
it locally before tagging:

```bash
# What the supplement will append for the next release:
go run ./internal/changelogx/cmd/changelogx \
  -supplement -from v1.1.0 -to HEAD
```

The first run for a tag typically takes 5–10 minutes (the
cross-compile is the slowest step). A `workflow_dispatch` dry-run is
2–3 minutes.

### Post-release verification

Once the workflow is ✅, verify the release:

1. **The Releases page shows v<X.Y.Z>.** Visit
   https://github.com/hurtener/dockyard/releases. The latest release
   is the tag you just pushed, marked "Latest" (for a stable
   release; pre-releases are marked accordingly). The body is the
   CHANGELOG section's content (no headings before P1; the
   acknowledgements at the end).

2. **The artifacts are attached.** Six binaries
   (`dockyard-v<X.Y.Z>-{darwin,linux,windows}-{amd64,arm64}[.exe]`)
   plus six `.sha256` sidecars plus the aggregate `checksums.txt`.

3. **Checksums verify.** Download `checksums.txt` and at least one
   binary; verify:

   ```sh
   sha256sum -c checksums.txt
   ```

   `OK` on every artifact line.

4. **`go install` works.** From any directory:

   ```sh
   go install github.com/hurtener/dockyard/cmd/dockyard@v1.0.0
   dockyard --version    # if a version flag exists
   dockyard --help
   ```

   `go install` resolves against the just-pushed tag; the resulting
   binary lives at `"$(go env GOPATH)/bin/dockyard"`. A failure here
   almost always means the tag did not push or the `go.mod` module
   path is wrong (it should be `github.com/hurtener/dockyard`).

5. **One artifact boots.** Download e.g.
   `dockyard-v<X.Y.Z>-darwin-arm64`, `chmod +x`, run `--help`. The
   binary should print the cobra command tree.

If any verification step fails, follow the rollback procedure below.

---

## Rollback procedure

A release that goes out broken is rare but real. The right rollback
depends on the failure mode.

### Failure mode A — bad release body, good artifacts

Symptom: the GitHub Release body is malformed (a CHANGELOG section
extraction issue, or the wrong section was attached because
CHANGELOG was edited mid-release).

Fix: edit the release body directly in the GitHub UI, or re-run the
release workflow (Re-run failed jobs) — the `softprops/action-gh-release`
action updates an existing release in place.

### Failure mode B — bad artifact in an otherwise-good release

Symptom: one of the cross-compile binaries fails to launch (a Go
toolchain issue, a transient runner fault).

Fix: re-run the release workflow. The build step re-runs the
cross-compile; the release step uploads any artifacts not already
present and replaces existing ones. Verify with `sha256sum -c` after
the re-run.

### Failure mode C — a broken release across the board (recall a tag)

Symptom: the released code itself is broken (the wrong commit was
tagged, a regression slipped through preflight, a security issue
needs a recall).

Fix:

1. **Delete the GitHub Release** (preserves the tag).

   In the GitHub UI: Releases → the release → Delete. This removes
   the artifacts from GitHub and prevents new downloads through the
   Releases page. The tag itself is preserved, so any user who
   already ran `go install …@v<X.Y.Z>` keeps their cached copy in
   `go.sum`.

2. **Cut a follow-up patch release immediately** (recommended).

   Even if you delete the release, the tag is in the public history
   and `go install …@v<X.Y.Z>` will still resolve to it for users
   who haven't pinned a newer version. The right rollback is
   forward: cut `v<X.Y.Z+1>` with the fix and announce it.

3. **Force-delete the tag — only if absolutely necessary.**

   A force-deleted tag breaks `go.sum` for any user who already
   installed it; it is a hostile operation. Only do this if the
   release shipped a real security risk (e.g. a hardcoded secret
   slipped in). Procedure:

   ```sh
   git push --delete origin v<X.Y.Z>
   git tag -d v<X.Y.Z>
   ```

   Announce loudly and immediately. Then cut `v<X.Y.Z+1>` per
   step 2.

   This path is governed by the project's security policy; do not
   take it without checking with a second maintainer.

### Failure mode D — the workflow itself is broken

Symptom: the release workflow fails because of an issue in
`release.yml` or in `internal/releasebuild` / `internal/changelogx`
(e.g. a regression introduced by a workflow update).

Fix: revert the workflow change on `main` (PR + merge), then re-run
the release workflow (Re-run all jobs). Do **not** push a different
tag — the same `v<X.Y.Z>` tag should produce the released artifacts
once the workflow is fixed.

---

## Pre-publish checklist (for a maintainer authoring a release PR)

When the work for a release lands (a feature PR or a security PR), the
release-prep PR usually:

- [ ] adds the version's `## [<version>] - <YYYY-MM-DD>` section to
  `CHANGELOG.md` (move every entry from `## [Unreleased]` into the
  new section);
- [ ] updates the `[<version>]` reference-link footer block at the
  bottom of `CHANGELOG.md`;
- [ ] bumps any version constants (e.g. an `internal/version` package
  if one lands post-V1);
- [ ] updates the docs site's release callout if the release is a
  notable milestone;
- [ ] confirms `make preflight` is green locally on the release PR.

The release-prep PR is then merged into `main`; the maintainer pulls
`main` and runs the tag-push sequence above.

---

## What the release workflow does NOT do

For transparency:

- **It does NOT push the tag.** The tag-push is the maintainer's act —
  the workflow only responds to a push. This is intentional: a tag
  push is a deliberate "release this now" signal, not a side-effect
  of merging a PR.
- **It does NOT bump version constants.** Dockyard does not currently
  embed a release version in the binary (the source-of-truth is the
  git tag). A future post-V1 phase may add an embedded version (an
  `internal/version` package + `-ldflags` injection); when that
  lands, the release workflow will set it via the build step.
- **It does NOT sign the artifacts.** Cosign / SLSA signing is a
  recorded V2 follow-up (`docs/V2-BACKLOG.md` — "Signed releases +
  SLSA provenance"). V1 releases ship SHA-256 checksums and the
  source URL pin via the tag.
- **It does NOT announce the release.** The Releases page is the
  source of truth; announcing is the maintainer's act (a blog post,
  a Mastodon / Bluesky post, a Discord ping — outside the
  repository).
