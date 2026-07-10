#!/usr/bin/env bash
# Smoke script for Phase 32 — dual-lifecycle HTTP and observability.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-32 dual-lifecycle HTTP and observability"

if [ -f docs/plans/phase-32-stateless-http-observability.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
skip "dual-lifecycle HTTP implementation lands with Phase 32"

smoke_summary
