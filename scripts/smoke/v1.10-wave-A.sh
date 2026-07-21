#!/usr/bin/env bash
# Smoke script for v1.10 wave A — opt-in validated-token exposure (RFC 8693).
# Plan: docs/plans/v1.10-wave-A-authz-expose-raw-token.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.10-wave-A authz-expose-raw-token"

# --- Item 1: the opt-in config field + accessors exist -----------------------
if grep -q "ExposeRawToken bool" runtime/authz/config.go 2>/dev/null &&
   grep -q "func WithRawToken" runtime/authz/principal.go 2>/dev/null &&
   grep -q "func RawTokenFromContext" runtime/authz/principal.go 2>/dev/null; then
  ok "ExposeRawToken field + WithRawToken/RawTokenFromContext accessors exist"
else
  fail "ExposeRawToken field or accessors missing"
fi

# --- Item 2: the middleware gates exposure on the opt-in ---------------------
if grep -q "cfg.ExposeRawToken" runtime/server/http.go 2>/dev/null &&
   grep -q "authz.WithRawToken" runtime/server/http.go 2>/dev/null; then
  ok "auth middleware threads the token only when ExposeRawToken is set"
else
  fail "auth middleware does not gate raw-token exposure on ExposeRawToken"
fi

# --- Item 3: the invariants are asserted by tests ----------------------------
if grep -rqs "TestExposeRawTokenDefaultOffHidesToken" runtime/server 2>/dev/null &&
   grep -rqs "TestExposeRawTokenNotExposedForRejectedRequest" runtime/server 2>/dev/null &&
   grep -rqs "TestExposeRawTokenNeverEntersDurableTaskState" runtime/server 2>/dev/null; then
  ok "default-off, rejected-request, and token-free-state invariants are tested"
else
  fail "ExposeRawToken invariant tests missing"
fi

# --- Item 4: the RFC carve-out and the superseding decision exist ------------
if grep -q "Delegated token exchange (opt-in)" RFC-001-Dockyard.md 2>/dev/null &&
   grep -qs "^## D-201" docs/decisions.md 2>/dev/null; then
  ok "RFC §19.2 delegation carve-out + D-201 recorded"
else
  fail "RFC carve-out or D-201 missing"
fi

# --- Item 5: the published guide documents the RFC 8693 opt-in ---------------
if [ -f docs/site/guides/oauth-protected-resource.md ] &&
   grep -q "Delegated token exchange (RFC 8693)" docs/site/guides/oauth-protected-resource.md 2>/dev/null &&
   grep -q "not token passthrough\|not Token Passthrough\|delegation, not token passthrough" docs/site/guides/oauth-protected-resource.md 2>/dev/null; then
  ok "OAuth guide documents the RFC 8693 opt-in with the not-passthrough warning"
else
  skip "OAuth guide RFC 8693 subsection not present yet"
fi

smoke_summary
