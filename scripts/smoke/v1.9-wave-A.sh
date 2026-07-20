#!/usr/bin/env bash
# Smoke script for v1.9 wave A — modern Tasks transport parity.
# Plan: docs/plans/v1.9-wave-A-modern-tasks-transport-parity.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.9-wave-A modern-tasks-transport-parity"

# --- Item 1: the SEP-2575 serverInfo injection is still wired ----------------
# Guards against a future refactor silently dropping D-199's injection.
if grep -q "func EncodeServerInfo" internal/protocolcodec/response.go 2>/dev/null &&
   grep -q "EncodeServerInfo" runtime/server/response_semantics.go 2>/dev/null; then
  ok "protocolcodec.EncodeServerInfo exists and is invoked by the response middleware"
else
  fail "EncodeServerInfo missing or not wired into responseSemanticsMiddleware"
fi

# --- Item 2: modern Tasks parity test asserts BOTH fields --------------------
# The parity test must name serverInfo and resultType together in one test file
# on a tasks/* path.
parity_hit=""
for f in $(grep -rlE "tasks/(get|update|cancel|list)" runtime/server test/integration --include='*_test.go' 2>/dev/null); do
  if grep -q "serverInfo" "$f" && grep -q "resultType" "$f"; then
    parity_hit="$f"
    break
  fi
done
if [ -n "$parity_hit" ]; then
  ok "a modern Tasks test asserts both serverInfo and resultType ($parity_hit)"
else
  skip "modern Tasks parity test (serverInfo + resultType) not present yet"
fi

# --- Item 3: the reachability invariant is proven + recorded ------------------
# The modern protocol is unreachable off the stateless HTTP path, so the Tasks
# mount's legacy codec is correct over stdio. Satisfied by a test + a decision,
# not a code branch (see D-200).
if grep -rqs "TestModernProtocolUnreachableViaInitialize" runtime/server 2>/dev/null &&
   grep -qs "^## D-200" docs/decisions.md 2>/dev/null; then
  ok "modern-protocol-unreachable-over-stdio invariant is proven by test and recorded (D-200)"
else
  skip "reachability invariant test/decision not present yet"
fi

# --- Item 4: CHANGELOG carries 1.9.2 and drops the over-broad 1.9.1 wording ---
# Both are wave deliverables, so they skip (not fail) until the wave lands.
if grep -q '^## \[1.9.2\]' CHANGELOG.md 2>/dev/null; then
  ok "CHANGELOG has a [1.9.2] section"
else
  skip "CHANGELOG [1.9.2] section not written yet"
fi
if grep -q 'on every modern-protocol response' CHANGELOG.md 2>/dev/null; then
  skip "CHANGELOG 1.9.1 wording not yet corrected ('on every modern-protocol response')"
else
  ok "CHANGELOG no longer claims 'on every modern-protocol response'"
fi

smoke_summary
