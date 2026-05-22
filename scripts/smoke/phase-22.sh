#!/usr/bin/env bash
# Smoke script for Phase 22 — Inspector core: bridge host-half + obs view.
# One assertion per acceptance criterion (master plan / RFC §12).
# A check against an unbuilt surface skips(), never fails() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-22 inspector core"

# 1. The internal/inspector backend exists and builds.
if [ -d internal/inspector ] && [ -f internal/inspector/inspector.go ]; then
  if go build ./internal/inspector/ >/dev/null 2>&1; then
    ok "internal/inspector exists and builds"
  else
    fail "internal/inspector does not build"
  fi
else
  skip "internal/inspector not built"
fi

# 2. The inspector refuses a non-localhost bind (the typed rejection — RFC §12,
#    the CVE-2025-49596 lesson). Asserted via the package's loopback-gate test.
if [ -f internal/inspector/inspector_test.go ]; then
  if go test -run 'TestNew_RejectsNonLoopback' ./internal/inspector/ \
       >/dev/null 2>&1; then
    ok "inspector refuses a non-localhost bind (typed ErrNonLoopbackBind)"
  else
    fail "inspector non-loopback rejection test failed"
  fi
else
  skip "internal/inspector loopback-gate test not built"
fi

# 3. The host-half bridge exists and reuses the web/bridge ui/ dialect — it does
#    not fork the protocol constants.
HOST=web/inspector/src/host/host-bridge.ts
if [ -f "$HOST" ]; then
  if grep -q "@dockyard/bridge" "$HOST" \
     && grep -q "ui/initialize" "$HOST" 2>/dev/null \
     || grep -q "ViewMethod" "$HOST"; then
    ok "host-half bridge exists and reuses the web/bridge ui/ dialect"
  else
    fail "host-half bridge does not reuse the web/bridge protocol contract"
  fi
else
  skip "web/inspector host-half bridge not built"
fi

# 4. The obs/v1 stream relay exists in the inspector backend.
if [ -f internal/inspector/relay.go ]; then
  if grep -q 'obs/v1' internal/inspector/relay.go \
     && grep -q 'streamHandler' internal/inspector/relay.go; then
    ok "inspector obs/v1 stream relay exists"
  else
    fail "internal/inspector/relay.go missing the obs/v1 stream relay"
  fi
else
  skip "internal/inspector obs relay not built"
fi

# 5. The web/inspector frontend project exists and is in the make web set.
if [ -f web/inspector/package.json ]; then
  if grep -q '"name": "@dockyard/inspector"' web/inspector/package.json \
     && grep -q '"gate"' web/inspector/package.json; then
    ok "web/inspector is @dockyard/inspector with a gate script"
  else
    fail "web/inspector/package.json missing name or gate script"
  fi
  if grep -q 'web/inspector' Makefile; then
    ok "web/inspector is in the make web WEB_PROJECTS set"
  else
    fail "web/inspector is not wired into the Makefile WEB_PROJECTS set"
  fi
else
  skip "web/inspector frontend project not built"
fi

# 6. The web/inspector frontend composes @dockyard/ui — it does not re-implement
#    a shared component (CLAUDE.md §20).
if [ -f web/inspector/src/App.svelte ]; then
  if grep -rq "@dockyard/ui" web/inspector/src/; then
    ok "web/inspector composes the @dockyard/ui design system"
  else
    fail "web/inspector does not compose @dockyard/ui (violates §20)"
  fi
else
  skip "web/inspector App not built"
fi

# 6b. The App-preview surface has a production entry point (remediation R1): the
#     backend serves the server's ui:// App HTML over `/api/apps`, and the
#     frontend loads a real App from it (RFC §12 line 711 — the inspector
#     renders the server's Apps). A depth audit found the AppFrame chain was
#     unreachable in the shipped inspector.
if [ -f internal/inspector/appsource.go ]; then
  if grep -q "AppsFromServer" internal/inspector/appsource.go \
     && grep -q "ReadResource" internal/inspector/appsource.go \
     && grep -q '/api/apps' internal/inspector/assets.go; then
    ok "the inspector backend serves the server's Apps over /api/apps (R1)"
  else
    fail "internal/inspector App-preview backend incomplete (R1 Blocker 2)"
  fi
else
  skip "internal/inspector App-preview backend not built"
fi
if [ -f web/inspector/src/App.svelte ]; then
  if grep -q "fetchApps" web/inspector/src/App.svelte; then
    ok "web/inspector App.svelte loads a real App from /api/apps (R1)"
  else
    fail "web/inspector App.svelte has no production App-load path (R1 Blocker 2)"
  fi
else
  skip "web/inspector App not built"
fi

# 7. The web/inspector frontend gate passes (type-check + unit tests + coverage).
if [ -f web/inspector/package.json ]; then
  if ! command -v npm >/dev/null 2>&1; then
    skip "npm not installed — web/inspector frontend gate deferred"
  else
    if ( cd web/inspector && \
         { [ -d node_modules ] || npm ci --no-audit --no-fund >/dev/null 2>&1; } && \
         npm run gate >/dev/null 2>&1 ); then
      ok "web/inspector type-check + unit tests + coverage pass"
    else
      fail "web/inspector frontend gate failed"
    fi
  fi
else
  skip "web/inspector not built — frontend gate deferred"
fi

smoke_summary
