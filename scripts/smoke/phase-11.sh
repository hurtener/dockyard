#!/usr/bin/env bash
# Smoke script for Phase 11 — the Svelte bridge shell library (web/bridge): the
# View half of the ui/ postMessage JSON-RPC dialect — handshake, hostContext
# stores, notification fan-out, display-mode negotiation, viewUUID view-state.
# One assertion per acceptance criterion (docs/plans/phase-11-bridge-shell.md).
# A check against an unbuilt surface, or a missing npm, skips rather than fails.
# preflight: serial — exercises the shared in-repo web/ workspace (`make web`,
# vitest coverage), so the preflight gate runs it sequentially, never
# concurrently with another web gate (D-188).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-11 bridge-shell"

# 1. The web/bridge project exists.
if [ -f web/bridge/package.json ]; then
  ok "web/bridge project exists"
else
  skip "web/bridge/package.json not built"
fi

# 2. The public entry points exist (the View side of the ui/ dialect).
if [ -f web/bridge/src/index.ts ] && [ -f web/bridge/src/bridge.ts ]; then
  ok "web/bridge public entry (index.ts + bridge.ts) present"
else
  skip "web/bridge source not built"
fi

# 3. Plain Svelte, no SvelteKit (D-006).
if [ -f web/bridge/package.json ]; then
  if ! grep -q '@sveltejs/kit' web/bridge/package.json; then
    ok "web/bridge declares no SvelteKit dependency (D-006)"
  else
    fail "web/bridge depends on SvelteKit (violates D-006)"
  fi
else
  skip "web/bridge/package.json not built — SvelteKit check deferred"
fi

# 4. The ui/ dialect method names and wire types are centralised (forward-compat).
if [ -f web/bridge/src/protocol.ts ]; then
  if grep -q 'ui/initialize' web/bridge/src/protocol.ts \
     && grep -q 'ui/request-display-mode' web/bridge/src/protocol.ts; then
    ok "web/bridge centralises the ui/ dialect methods in protocol.ts"
  else
    fail "web/bridge/src/protocol.ts missing ui/ dialect method names"
  fi
else
  skip "web/bridge/src/protocol.ts not built"
fi

# 5. The vitest unit-test config is present and there is a real test suite.
if [ -f web/bridge/vitest.config.ts ] \
   && compgen -G "web/bridge/src/__tests__/*.test.ts" >/dev/null; then
  ok "web/bridge has a vitest config and a unit-test suite"
else
  skip "web/bridge test suite not built"
fi

# 6. The frontend gate: the web/bridge type-check + unit tests pass (make web).
if [ -f web/bridge/package.json ]; then
  if command -v npm >/dev/null 2>&1; then
    # Capture `make web` output to a temp log and surface it on failure so a CI
    # frontend-gate failure is diagnosable; stay quiet on success (C1, Wave 6
    # checkpoint).
    web_log="$(mktemp -t dockyard-smoke-phase11-web.XXXXXX)"
    if make web >"$web_log" 2>&1; then
      ok "web/bridge type-check + unit tests pass (make web)"
    else
      fail "web/bridge frontend gate (make web) failed"
      echo "--- make web output (last 60 lines) ---" >&2
      tail -n 60 "$web_log" >&2
      echo "--- end make web output ---" >&2
    fi
    rm -f "$web_log"
  else
    skip "npm not installed — web/bridge frontend gate deferred"
  fi
else
  skip "web/bridge not built — frontend gate deferred"
fi

# 7. The frontend CI gate exists: ci.yml has a web job and the Makefile a target.
if grep -q 'name: web' .github/workflows/ci.yml \
   && grep -qE '^web:' Makefile; then
  ok "frontend CI gate wired (ci.yml web job + Makefile web target)"
else
  fail "frontend CI gate missing (ci.yml web job / Makefile web target)"
fi

smoke_summary
