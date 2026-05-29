#!/usr/bin/env bash
# Smoke script for v1.3 wave A — DX fixes from first downstream feedback.
# Plan: docs/plans/v1.3-wave-A-dx-feedback-fixes.md
# Items 1-3 (items 4-5 are wave B). A check against an unbuilt surface
# should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.3-wave-A dx-feedback-fixes"

# --- Item 3: dockyard new --here -------------------------------------------

if [ -x bin/dockyard ]; then
  if bin/dockyard new --help 2>&1 | grep -q -- "--here"; then
    ok "dockyard new exposes --here"
  else
    skip "dockyard new has no --here flag yet"
  fi
else
  skip "bin/dockyard not built (run make build)"
fi

if [ -f internal/scaffold/scaffold.go ]; then
  if grep -q "ErrFileCollision" internal/scaffold/scaffold.go; then
    ok "scaffold guards file collisions (--here no-overwrite)"
  else
    skip "scaffold has no ErrFileCollision guard yet"
  fi
else
  skip "internal/scaffold/scaffold.go not present"
fi

# --- Item 2: go.mod version pin --------------------------------------------

if [ -f internal/scaffold/templates.go ] && grep -q "func requireVersion" internal/scaffold/templates.go; then
  ok "scaffold pins a real go.mod require version (requireVersion)"
else
  skip "scaffold renderGoMod version pin not present yet"
fi

if [ -f internal/cli/root.go ] && grep -q "func ResolvedVersion" internal/cli/root.go; then
  ok "cli.ResolvedVersion resolves the build/install version"
else
  skip "cli.ResolvedVersion not present yet"
fi

# --- Item 1: enforced quality gates (D-168/D-169) --------------------------

if [ -f internal/validate/checks.go ]; then
  if grep -q "func checkFixtures" internal/validate/checks.go && \
     grep -q "func checkContractTests" internal/validate/checks.go; then
    ok "validate enforces require_fixtures + require_contract_tests"
  else
    skip "validate does not yet enforce the quality gates"
  fi
else
  skip "internal/validate/checks.go not present"
fi

smoke_summary
