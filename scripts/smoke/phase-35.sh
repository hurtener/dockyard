#!/usr/bin/env bash
# Smoke script for Phase 35 — inspector, CLI, and quality-gate compatibility.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-35 inspector, CLI, and quality-gate compatibility"

if [ -f docs/plans/phase-35-inspector-cli-2026-compat.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
skip "client-shaped tooling migration lands with Phase 35"

smoke_summary
