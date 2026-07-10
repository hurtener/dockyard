#!/usr/bin/env bash
# Smoke script for Phase 36 — stateless OAuth resource server.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-36 stateless OAuth resource server"

if [ -f docs/plans/phase-36-oauth-resource-server.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
if [ -f docs/research/08-oauth-resource-server.md ]; then ok "OAuth resource-server brief exists"; else fail "OAuth resource-server brief missing"; fi
skip "OAuth resource-server implementation lands with Phase 36"

smoke_summary
