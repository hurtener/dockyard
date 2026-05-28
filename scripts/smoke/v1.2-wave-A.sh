#!/usr/bin/env bash
# Smoke script for v1.2 wave A — scaffold autogen + changelog supplement.
# Plan: docs/plans/v1.2-wave-A-scaffold-and-changelog.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.2-wave-A scaffold-autogen + changelog-supplement"

# --- Item 1: scaffold auto-generate (D-166) ---------------------------------

# The --no-postgen opt-out flag exists on `dockyard new`.
# Skips until the flag is implemented (skeleton convention: an unbuilt
# surface skips, never fails).
if [ -x bin/dockyard ]; then
  if bin/dockyard new --help 2>&1 | grep -q -- "--no-postgen"; then
    ok "dockyard new exposes --no-postgen"
  else
    skip "dockyard new has no --no-postgen flag yet (D-166 not implemented)"
  fi
else
  skip "bin/dockyard not built (run make build)"
fi

# The post-scaffold steps are wired at the CLI boundary, not in scaffold/.
if [ -f internal/cli/new.go ]; then
  if grep -q "no-postgen\|noPostgen\|NoPostgen" internal/cli/new.go; then
    ok "internal/cli/new.go carries the post-step opt-out"
  else
    skip "internal/cli/new.go has no --no-postgen wiring yet"
  fi
else
  skip "internal/cli/new.go not present"
fi

# The §19 sync proof: the scaffold skill documents --no-postgen (the D-166
# auto-step + its opt-out). It still legitimately mentions `go mod tidy` for
# the opt-out fallback, so the meaningful signal is the flag, not its absence.
if [ -f skills/scaffold-a-server/SKILL.md ]; then
  if grep -q -- "--no-postgen" skills/scaffold-a-server/SKILL.md; then
    ok "scaffold-a-server skill documents the post-step opt-out (D-166)"
  else
    skip "scaffold-a-server skill not yet synced for D-166"
  fi
else
  skip "skills/scaffold-a-server/SKILL.md not present"
fi

# --- Item 2: changelog supplement (D-167) -----------------------------------

# The pure Supplement classifier exists.
if [ -f internal/changelogx/supplement.go ]; then
  if grep -q "func Supplement" internal/changelogx/supplement.go; then
    ok "internal/changelogx declares Supplement(...)"
  else
    fail "internal/changelogx/supplement.go lacks a Supplement function"
  fi
else
  skip "internal/changelogx/supplement.go not built yet"
fi

# The cmd driver gained a -supplement mode.
if [ -f internal/changelogx/cmd/changelogx/main.go ]; then
  if grep -q "supplement" internal/changelogx/cmd/changelogx/main.go; then
    ok "cmd/changelogx references a supplement mode"
  else
    skip "cmd/changelogx has no supplement mode yet"
  fi
else
  skip "cmd/changelogx/main.go not present"
fi

# The release workflow appends the supplement.
if [ -f .github/workflows/release.yml ]; then
  if grep -q "supplement" .github/workflows/release.yml; then
    ok "release.yml wires the changelog supplement step"
  else
    skip "release.yml has no supplement step yet"
  fi
else
  skip ".github/workflows/release.yml not present"
fi

smoke_summary
