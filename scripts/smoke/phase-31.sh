#!/usr/bin/env bash
# Smoke script for Phase 31 — MCP 2026-07-28 SDK foundation.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-31 MCP 2026-07-28 SDK foundation"

if [ -f docs/plans/phase-31-mcp-2026-sdk-foundation.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
if [ -f docs/research/07-mcp-2026-07-28-migration.md ]; then ok "migration brief exists"; else fail "migration brief missing"; fi
skip "SDK/spec implementation lands with Phase 31"

smoke_summary
