#!/usr/bin/env bash
# Smoke script for Phase 02 — protocolcodec seam + vendored specs.
# A check against an unbuilt surface skips (see common.sh).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-02 protocolcodec-seam"

SEAM="internal/protocolcodec"

# 1. The seam package exists.
if [ -d "$SEAM" ] && compgen -G "$SEAM/*.go" >/dev/null; then
  ok "internal/protocolcodec package exists"
else
  skip "internal/protocolcodec not built yet"
fi

# 2. Vendored MCP Apps spec exists and is pinned by commit SHA.
APPS="docs/specifications/mcp-apps-2026-01-26.mdx"
if [ -f "$APPS" ]; then
  if grep -qE 'Pinned commit: [0-9a-f]{40}' "$APPS"; then
    ok "vendored MCP Apps spec present and pinned by SHA"
  else
    fail "vendored MCP Apps spec missing a pinned 40-char commit SHA"
  fi
else
  skip "vendored MCP Apps spec not present yet"
fi

# 3. Vendored MCP Tasks schema exists and is pinned by commit SHA.
TASKS="docs/specifications/mcp-tasks-experimental.schema.ts"
if [ -f "$TASKS" ]; then
  if grep -qE 'Pinned commit: [0-9a-f]{40}' "$TASKS"; then
    ok "vendored MCP Tasks schema present and pinned by SHA"
  else
    fail "vendored MCP Tasks schema missing a pinned 40-char commit SHA"
  fi
else
  skip "vendored MCP Tasks schema not present yet"
fi

# 4. Seam isolation: the deprecated flat _meta key literal appears in Go
#    sources only inside internal/protocolcodec (P3, AGENTS.md §10).
if compgen -G "**/*.go" >/dev/null 2>&1 || find . -name '*.go' -print -quit | grep -q .; then
  STRAY=$(grep -rl --include='*.go' 'ui/resourceUri' . 2>/dev/null \
            | grep -v "^\./$SEAM/" || true)
  if [ -z "$STRAY" ]; then
    ok "extension _meta key literals confined to internal/protocolcodec"
  else
    fail "extension _meta key literal found outside the seam: $STRAY"
  fi
else
  skip "no Go sources to scan for seam isolation"
fi

# 5. The seam's own test suite passes (covers round-trip, tolerance, boundary).
if command -v go >/dev/null 2>&1 && [ -d "$SEAM" ]; then
  if CGO_ENABLED=0 go test "./$SEAM/" >/dev/null 2>&1; then
    ok "internal/protocolcodec test suite passes"
  else
    fail "internal/protocolcodec test suite failed"
  fi
else
  skip "go toolchain or seam package unavailable — skipping codec tests"
fi

smoke_summary
