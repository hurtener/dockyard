#!/usr/bin/env bash
# Smoke script for Phase 07 — MCP server core: transports + security.
# One assertion per acceptance criterion (docs/plans/phase-07-server-core.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-07 server-core"

# 1. The streamable-HTTP transport file exists.
if [ -f runtime/server/http.go ]; then
  ok "runtime/server streamable-HTTP transport exists"
else
  skip "runtime/server/http.go not built"
fi

# 2. The resource registration file exists.
if [ -f runtime/server/resource.go ]; then
  ok "runtime/server resource registration exists"
else
  skip "runtime/server/resource.go not built"
fi

# 3. The server package builds CGo-free.
if [ -f runtime/server/http.go ]; then
  if CGO_ENABLED=0 go build ./runtime/server/... >/dev/null 2>&1; then
    ok "runtime/server builds CGo-free"
  else
    fail "runtime/server does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/server transports not built — build check deferred"
fi

# 4. The server package tests pass (transports, resources, security, seam).
if [ -f runtime/server/http_test.go ]; then
  if go test ./runtime/server/... >/dev/null 2>&1; then
    ok "runtime/server tests pass"
  else
    fail "runtime/server tests fail"
  fi
else
  skip "phase-07 tests not built"
fi

# 5. HTTP security options are explicitly settable (DefaultHTTPSecurity exists).
if [ -f runtime/server/http.go ]; then
  if grep -q 'func DefaultHTTPSecurity()' runtime/server/http.go \
     && grep -q 'DNSRebindingProtection' runtime/server/http.go \
     && grep -q 'CrossOriginProtection' runtime/server/http.go; then
    ok "explicit HTTP security options present"
  else
    fail "explicit HTTP security options missing from http.go"
  fi
else
  skip "runtime/server/http.go not built — security check deferred"
fi

# 6. The getServer per-request seam is exposed (ServerForRequest option).
if [ -f runtime/server/http.go ]; then
  if grep -q 'ServerForRequest func(\*http.Request) \*Server' runtime/server/http.go; then
    ok "getServer per-request seam (ServerForRequest) exposed"
  else
    fail "ServerForRequest per-request seam missing from http.go"
  fi
else
  skip "runtime/server/http.go not built — seam check deferred"
fi

# 7. The in-memory transport entrypoint is wired (ServeInMemory).
if [ -f runtime/server/server.go ]; then
  if grep -q 'func (s \*Server) ServeInMemory(' runtime/server/server.go; then
    ok "InMemoryTransport entrypoint (ServeInMemory) wired"
  else
    fail "ServeInMemory entrypoint missing from server.go"
  fi
else
  skip "runtime/server/server.go not built"
fi

# 8. D-021 resolved: the temporary exported MCP() seam is retired.
if [ -f runtime/server/server.go ]; then
  if grep -qE 'func \(s \*Server\) MCP\(\)' runtime/server/server.go; then
    fail "Server.MCP() still exported — D-021 not resolved"
  else
    ok "Server.MCP() retired — D-021 resolved"
  fi
else
  skip "runtime/server/server.go not built — D-021 check deferred"
fi

smoke_summary
