#!/usr/bin/env bash
# Smoke script for v1.11 wave A — opt-in unauthenticated MCP handshake.
# Plan: docs/plans/v1.11-wave-A-authz-unauthenticated-handshake.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.11-wave-A authz-unauthenticated-handshake"

# --- Item 1: the opt-in config field exists ----------------------------------
if grep -q "UnauthenticatedHandshake bool" runtime/authz/config.go 2>/dev/null; then
  ok "authz.Config.UnauthenticatedHandshake field exists"
else
  fail "UnauthenticatedHandshake field missing"
fi

# --- Item 2: the middleware defines a deny-by-default exempt allowlist --------
if grep -q "exemptHandshakeMethods" runtime/server/http.go 2>/dev/null &&
   grep -q "cfg.UnauthenticatedHandshake" runtime/server/http.go 2>/dev/null &&
   grep -q '"server/discover"' runtime/server/http.go 2>/dev/null; then
  ok "auth middleware gates on the flag with a Dockyard-owned exempt allowlist"
else
  fail "exempt allowlist or flag gating missing from the auth middleware"
fi

# --- Item 3: no invocation method is in the exempt allowlist ------------------
# The allowlist block runs from its declaration to the closing brace; assert no
# invocation method appears inside the middleware source's allowlist literal.
if ! grep -qE '"(tools/call|resources/read|prompts/get|completion/complete)"' \
     <(sed -n '/exemptHandshakeMethods = map/,/^}/p' runtime/server/http.go) 2>/dev/null; then
  ok "no invocation method is in the exempt allowlist (deny-by-default)"
else
  fail "an invocation method leaked into the exempt allowlist"
fi

# --- Item 4: the invariants + fail-closed fuzz target are tested --------------
if grep -rqs "TestUnauthHandshakeDefaultOffProtectsEveryMethod" runtime/server 2>/dev/null &&
   grep -rqs "TestUnauthHandshakeInvocationsStillRequireToken" runtime/server 2>/dev/null &&
   grep -rqs "TestUnauthHandshakeBatchRequiresTokenIfAnyInvocation" runtime/server 2>/dev/null &&
   grep -rqs "func FuzzAllMethodsExempt" runtime/server 2>/dev/null; then
  ok "default-off, deny-by-default, batch, and fail-closed fuzz invariants are tested"
else
  fail "UnauthenticatedHandshake invariant tests or fuzz target missing"
fi

# --- Item 5: the RFC carve-out and the superseding decision exist -------------
if grep -q "Unauthenticated handshake (opt-in)" RFC-001-Dockyard.md 2>/dev/null &&
   grep -qs "^## D-202" docs/decisions.md 2>/dev/null; then
  ok "RFC §19.2 handshake carve-out + D-202 recorded"
else
  fail "RFC carve-out or D-202 missing"
fi

# --- Item 6: the published guide documents the opt-in ------------------------
if [ -f docs/site/guides/oauth-protected-resource.md ] &&
   grep -q "Unauthenticated handshake" docs/site/guides/oauth-protected-resource.md 2>/dev/null &&
   grep -q "Discovery becomes public\|discovery public" docs/site/guides/oauth-protected-resource.md 2>/dev/null; then
  ok "OAuth guide documents the unauthenticated handshake with the discovery-public warning"
else
  skip "OAuth guide unauthenticated-handshake section not present yet"
fi

smoke_summary
