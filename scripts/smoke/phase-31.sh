#!/usr/bin/env bash
# Smoke script for Phase 31 — MCP 2026-07-28 SDK foundation.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-31 MCP 2026-07-28 SDK foundation"

if [ -f docs/plans/phase-31-mcp-2026-sdk-foundation.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
if [ -f docs/research/07-mcp-2026-07-28-migration.md ]; then ok "migration brief exists"; else fail "migration brief missing"; fi
if [ "$(go list -m -f '{{.Version}}' github.com/modelcontextprotocol/go-sdk 2>/dev/null)" = "v1.7.0-pre.2" ]; then ok "RC SDK pin exists"; else fail "RC SDK pin missing"; fi
if [ -f docs/specifications/mcp-core-2026-07-28.mdx ]; then ok "core RC snapshot exists"; else fail "core RC snapshot missing"; fi
if [ -f docs/specifications/mcp-authorization-2026-07-28.mdx ]; then ok "authorization RC snapshot exists"; else fail "authorization RC snapshot missing"; fi
if [ -f docs/specifications/mcp-tasks-2026-07-28.schema.ts ]; then ok "revised Tasks snapshot exists"; else fail "revised Tasks snapshot missing"; fi
if [ -f docs/specifications/mcp-apps-2026-07-28-audit.md ]; then ok "Apps revision audit exists"; else fail "Apps revision audit missing"; fi

smoke_summary
