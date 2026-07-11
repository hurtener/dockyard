#!/usr/bin/env bash
# Smoke script for Phase 32 — dual-lifecycle HTTP and observability.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-32 dual-lifecycle HTTP and observability"

if [ -f docs/plans/phase-32-stateless-http-observability.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
if grep -q 'type ProtocolMode' runtime/server/http.go; then ok "explicit protocol modes exist"; else fail "protocol modes missing"; fi
if grep -q 'ProtocolMode ProtocolMode' runtime/server/http.go; then ok "HTTP options select a protocol mode"; else fail "HTTP protocol mode option missing"; fi
if grep -q 'statelessRequestMiddleware' runtime/server/http.go; then ok "stateless request context is marked"; else fail "stateless request context marker missing"; fi
if [ -f runtime/server/http_dual_test.go ]; then ok "dual lifecycle tests exist"; else fail "dual lifecycle tests missing"; fi
if grep -q 'ProtocolMode: server.Dual' internal/scaffold/templates.go; then ok "scaffolds opt into dual HTTP"; else fail "scaffold dual HTTP wiring missing"; fi

smoke_summary
