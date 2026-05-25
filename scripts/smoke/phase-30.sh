#!/usr/bin/env bash
# Smoke script for Phase 30 — V1 release engineering + cut.
#
# One assertion per acceptance criterion in docs/plans/phase-30-v1-cut.md.
# A check against an unbuilt surface SKIPs rather than FAILs (the common.sh
# convention). The release workflow itself is verified end-to-end through
# the dry-run captured under docs/release/v1.0.0/.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-30 V1 release engineering + cut"

# ---------------------------------------------------------------------------
# 1. CHANGELOG.md exists with a v1.0.0 entry framed by the four binding
#    properties (P1–P4).
# ---------------------------------------------------------------------------
if [ -f CHANGELOG.md ]; then
  ok "CHANGELOG.md exists"
else
  fail "CHANGELOG.md missing"
fi

if grep -qE '^## \[1\.0\.0\]' CHANGELOG.md 2>/dev/null \
   || grep -qE '^## \[v1\.0\.0\]' CHANGELOG.md 2>/dev/null; then
  ok "CHANGELOG.md carries a v1.0.0 release heading"
else
  fail "CHANGELOG.md has no v1.0.0 release heading"
fi

# The four binding properties are the load-bearing framing — a v1.0.0
# entry without them is the wrong release notes.
for prop in "P1" "P2" "P3" "P4"; do
  if grep -qE "\\*\\*${prop} " CHANGELOG.md 2>/dev/null; then
    ok "CHANGELOG.md frames the V1 story by ${prop}"
  else
    fail "CHANGELOG.md is missing the ${prop} framing"
  fi
done

# ---------------------------------------------------------------------------
# 2. The release workflow exists with the cross-compile matrix + the
#    workflow_dispatch dry-run trigger + the preflight gate.
# ---------------------------------------------------------------------------
if [ -f .github/workflows/release.yml ]; then
  ok ".github/workflows/release.yml exists"
else
  fail ".github/workflows/release.yml missing"
fi

if grep -qE 'tags:\s*\["v\*"\]' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml triggers on v* tag push"
else
  fail "release.yml does not trigger on v* tag push"
fi

if grep -q 'workflow_dispatch' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml carries the workflow_dispatch (dry-run) trigger"
else
  fail "release.yml is missing the workflow_dispatch trigger"
fi

if grep -q 'make preflight' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml runs make preflight as a release gate"
else
  fail "release.yml does not run make preflight as a release gate"
fi

if grep -q 'releasebuild' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml drives internal/releasebuild for the cross-compile matrix"
else
  fail "release.yml does not reference internal/releasebuild"
fi

if grep -q 'softprops/action-gh-release' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml uses softprops/action-gh-release for the GitHub Release"
else
  fail "release.yml does not use a GitHub Release action"
fi

if grep -q 'sha256sum -c checksums.txt' .github/workflows/release.yml 2>/dev/null; then
  ok "release.yml verifies the aggregate checksums before publishing"
else
  fail "release.yml does not verify the aggregate checksums"
fi

# ---------------------------------------------------------------------------
# 3. The two new internal/ packages compile + their CLIs are reachable.
# ---------------------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  if go build -o /dev/null ./internal/changelogx/... >/tmp/dockyard-changelogx-build.out 2>&1; then
    ok "internal/changelogx builds"
  else
    cat /tmp/dockyard-changelogx-build.out | sed 's/^/  /'
    fail "internal/changelogx does not build"
  fi
  rm -f /tmp/dockyard-changelogx-build.out
  if go build -o /dev/null ./internal/releasebuild/... >/tmp/dockyard-releasebuild-build.out 2>&1; then
    ok "internal/releasebuild builds"
  else
    cat /tmp/dockyard-releasebuild-build.out | sed 's/^/  /'
    fail "internal/releasebuild does not build"
  fi
  rm -f /tmp/dockyard-releasebuild-build.out
else
  skip "go not on PATH — package build checks skipped"
fi

# ---------------------------------------------------------------------------
# 4. The changelogx CLI extracts the v1.0.0 section from the in-repo
#    CHANGELOG.md and exits zero; errors on a missing version.
# ---------------------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  if go run ./internal/changelogx/cmd/changelogx -version v1.0.0 >/tmp/dockyard-changelogx.out 2>&1; then
    if [ -s /tmp/dockyard-changelogx.out ]; then
      ok "changelogx CLI extracts the v1.0.0 section"
    else
      fail "changelogx CLI produced an empty v1.0.0 section"
    fi
  else
    cat /tmp/dockyard-changelogx.out | sed 's/^/  /'
    fail "changelogx CLI failed against v1.0.0"
  fi
  rm -f /tmp/dockyard-changelogx.out
  # Negative test: a missing version exits non-zero.
  if ! go run ./internal/changelogx/cmd/changelogx -version v99.0.0 >/dev/null 2>&1; then
    ok "changelogx CLI rejects a missing version"
  else
    fail "changelogx CLI accepted a missing version"
  fi
else
  skip "go not on PATH — changelogx CLI checks skipped"
fi

# ---------------------------------------------------------------------------
# 5. The releasebuild CLI is reachable (-help exits zero).
# ---------------------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  # -help in Go's flag package exits status 0 (the flag.ContinueOnError
  # path returns flag.ErrHelp without a usage error). In our CLI we use
  # ContinueOnError and surface a "is required" message; both shapes
  # (status 0 with usage, status non-zero with usage) prove the binary
  # built and printed something — we accept either by ignoring exit
  # status and grepping the captured output.
  go run ./internal/releasebuild/cmd/releasebuild -help >/tmp/dockyard-releasebuild.out 2>&1 || true
  if grep -qE 'releasebuild|Usage|version' /tmp/dockyard-releasebuild.out 2>/dev/null; then
    ok "releasebuild CLI is reachable and prints its usage"
  else
    cat /tmp/dockyard-releasebuild.out | sed 's/^/  /'
    fail "releasebuild CLI did not surface its usage"
  fi
  rm -f /tmp/dockyard-releasebuild.out
else
  skip "go not on PATH — releasebuild CLI checks skipped"
fi

# ---------------------------------------------------------------------------
# 6. V2-BACKLOG.md exists and covers every named deferral.
# ---------------------------------------------------------------------------
if [ -f docs/V2-BACKLOG.md ]; then
  ok "docs/V2-BACKLOG.md exists"
else
  fail "docs/V2-BACKLOG.md missing"
fi

for token in "D-088" "D-101" "D-108" "D-136" "D-139" "analytics-widgets" "SLSA" "ChatGPT Apps SDK" "Postgres"; do
  if grep -qF "${token}" docs/V2-BACKLOG.md 2>/dev/null; then
    ok "V2-BACKLOG references ${token}"
  else
    fail "V2-BACKLOG missing reference to ${token}"
  fi
done

# ---------------------------------------------------------------------------
# 7. RELEASING.md exists with the tag-push command, the semver policy,
#    and the rollback procedure.
# ---------------------------------------------------------------------------
if [ -f docs/RELEASING.md ]; then
  ok "docs/RELEASING.md exists"
else
  fail "docs/RELEASING.md missing"
fi

if grep -qE 'git tag -a v1\.0\.0' docs/RELEASING.md 2>/dev/null; then
  ok "RELEASING.md documents the v1.0.0 tag-push command"
else
  fail "RELEASING.md does not document the tag-push command"
fi

if grep -qiE 'semver|semantic version' docs/RELEASING.md 2>/dev/null; then
  ok "RELEASING.md documents the semver policy"
else
  fail "RELEASING.md does not document the semver policy"
fi

if grep -qiE 'rollback' docs/RELEASING.md 2>/dev/null; then
  ok "RELEASING.md documents the rollback procedure"
else
  fail "RELEASING.md does not document the rollback procedure"
fi

# ---------------------------------------------------------------------------
# 8. The post-v1.0.0 install path is documented in the README, the
#    scaffold-a-server skill, and the docs-site getting-started page.
# ---------------------------------------------------------------------------
# Match either the pre-tag (@main) or post-tag (@v1.0.0) form. The README
# carries @main with a "swap to @v1.0.0 after the tag is published" note;
# the skills + docs site lock to @v1.0.0. Both shapes are honest at their
# respective surfaces; the smoke just enforces that the go-install path is
# documented somewhere on each.
INSTALL_TOKEN='go install github.com/hurtener/dockyard/cmd/dockyard@'
if grep -qF "${INSTALL_TOKEN}" README.md 2>/dev/null; then
  ok "README.md documents the go-install recommended path"
else
  fail "README.md is missing the go-install recommended path"
fi
if grep -qF "${INSTALL_TOKEN}" skills/scaffold-a-server/SKILL.md 2>/dev/null; then
  ok "skills/scaffold-a-server/SKILL.md documents the go-install recommended path"
else
  fail "skills/scaffold-a-server/SKILL.md is missing the go-install recommended path"
fi
if grep -qF "${INSTALL_TOKEN}" docs/site/getting-started/index.md 2>/dev/null; then
  ok "docs/site/getting-started/index.md documents the go-install recommended path"
else
  fail "docs/site/getting-started/index.md is missing the go-install recommended path"
fi
if grep -qF "${INSTALL_TOKEN}" docs/site/index.md 2>/dev/null; then
  ok "docs/site/index.md carries the v1.0.0 release callout"
else
  fail "docs/site/index.md is missing the v1.0.0 release callout"
fi

# ---------------------------------------------------------------------------
# 9. The phase index marks Phase 30 as Shipped.
# ---------------------------------------------------------------------------
if grep -qE '^\|\s*30\s*\|.*Shipped' docs/plans/README.md 2>/dev/null; then
  ok "docs/plans/README.md marks Phase 30 as Shipped"
else
  fail "docs/plans/README.md does not mark Phase 30 as Shipped"
fi

# ---------------------------------------------------------------------------
# 10. The release dry-run transcripts exist under docs/release/v1.0.0/.
# ---------------------------------------------------------------------------
if [ -d docs/release/v1.0.0 ]; then
  ok "docs/release/v1.0.0/ exists (dry-run transcripts)"
else
  fail "docs/release/v1.0.0/ missing"
fi
for f in cross-compile-matrix.txt preflight.txt binary-help.txt README.md; do
  if [ -s "docs/release/v1.0.0/${f}" ]; then
    ok "docs/release/v1.0.0/${f} is present and non-empty"
  else
    fail "docs/release/v1.0.0/${f} is missing or empty"
  fi
done

# ---------------------------------------------------------------------------
# 11. The §19 hook is still wired in scripts/drift-audit.sh (the V1 cut
#     must not regress drift hygiene).
# ---------------------------------------------------------------------------
if grep -q "AGENTS.md §19" scripts/drift-audit.sh; then
  ok "scripts/drift-audit.sh still carries the §19 hook"
else
  fail "scripts/drift-audit.sh has lost the §19 hook"
fi

# §19 user-facing-vocabulary hook (added in the v1.0.0 polish pass):
# every user-facing surface is mechanically free of "Phase N" prose.
if grep -q "user-facing vocabulary" scripts/drift-audit.sh \
   && grep -q "template-README D-NNN" scripts/drift-audit.sh; then
  ok "scripts/drift-audit.sh carries the §19 user-facing-vocabulary + template-D-NNN hooks"
else
  fail "scripts/drift-audit.sh is missing the §19 user-facing-vocabulary hook"
fi

# ---------------------------------------------------------------------------
# 12. The new D-NNN entries land in the decisions log.
# ---------------------------------------------------------------------------
for d in D-154 D-155 D-156 D-157 D-158 D-159 D-160; do
  if grep -qE "^## ${d} " docs/decisions.md 2>/dev/null; then
    ok "decisions log carries ${d}"
  else
    fail "decisions log missing ${d}"
  fi
done

smoke_summary
