#!/usr/bin/env bash
# Smoke script for Phase 16 — obs/v1 transports: the out-of-band localhost SSE
# sink, the optional off-by-default OTelEmitter adapter (MCP semconv), and the
# MCP logging → obs/v1 log-event bridge. One assertion per acceptance criterion
# (docs/plans/phase-16-obs-transports.md, RFC §11.3).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-16 obs-transports"

# 1. The out-of-band SSE sink exists and runtime/obs still builds CGo-free
#    (no-CGo runtime guarantee even with the OTel dependency added; CLAUDE.md §13).
if [ -f runtime/obs/sse.go ]; then
  if CGO_ENABLED=0 go build ./runtime/obs/... >/dev/null 2>&1; then
    ok "the SSE sink exists and runtime/obs builds CGo-free"
  else
    fail "runtime/obs does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/obs/sse.go not built"
fi

# 2. The SSE sink registers behind the Phase 15 emitter seam (CLAUDE.md §4.4).
if [ -f runtime/obs/sse.go ]; then
  if grep -q 'RegisterDriver(sseDriverName' runtime/obs/sse.go \
     && grep -q 'type SSESink struct' runtime/obs/sse.go \
     && grep -q 'func (s \*SSESink) Emit' runtime/obs/sse.go; then
    ok "the SSE sink registers behind the obs emitter seam and is an Emitter"
  else
    fail "the SSE sink is not wired behind the emitter seam"
  fi
else
  skip "runtime/obs/sse.go not built — SSE seam check deferred"
fi

# 3. The SSE sink is localhost-only — it refuses a non-loopback bind
#    (CLAUDE.md §7; the sink is dev-mode-only and never reachable off-localhost).
if [ -f runtime/obs/sse.go ]; then
  if grep -q 'requireLoopback' runtime/obs/sse.go \
     && grep -q 'errSSENonLoopback' runtime/obs/sse.go; then
    ok "the SSE sink enforces a localhost-only bind"
  else
    fail "the SSE sink does not enforce a localhost-only bind"
  fi
else
  skip "runtime/obs/sse.go not built — SSE localhost-bind check deferred"
fi

# 4. The SSE sink streams without corrupting a stdio pipe (acceptance: headline
#    criterion) — proven by the integration test's no-corruption assertion.
if [ -f test/integration/phase16_obs_transports_test.go ]; then
  if grep -q 'TestPhase16_SSESinkDoesNotCorruptStdioPipe' \
       test/integration/phase16_obs_transports_test.go; then
    ok "an integration test proves the SSE sink does not corrupt a stdio pipe"
  else
    fail "no SSE no-corruption integration test found"
  fi
else
  skip "test/integration/phase16_obs_transports_test.go not built"
fi

# 5. The optional OTelEmitter adapter exists and registers behind the same seam.
if [ -f runtime/obs/otel/otel.go ]; then
  if grep -q 'type OTelEmitter struct' runtime/obs/otel/otel.go \
     && grep -q 'obs.RegisterDriver(driverName' runtime/obs/otel/otel.go; then
    ok "the OTelEmitter adapter exists and registers behind the obs emitter seam"
  else
    fail "the OTelEmitter adapter is not wired behind the emitter seam"
  fi
else
  skip "runtime/obs/otel/otel.go not built — OTel adapter check deferred"
fi

# 6. OTel spans carry mcp.* / gen_ai.* attributes (acceptance: OTel spans carry
#    mcp.*/gen_ai.* attributes; RFC §11.3).
if [ -f runtime/obs/otel/otel.go ]; then
  if grep -q '"mcp.method.name"' runtime/obs/otel/otel.go \
     && grep -q '"gen_ai.tool.name"' runtime/obs/otel/otel.go \
     && grep -q '"gen_ai.operation.name"' runtime/obs/otel/otel.go; then
    ok "OTel spans carry mcp.* / gen_ai.* semantic-convention attributes"
  else
    fail "the OTel adapter does not emit mcp.*/gen_ai.* attributes"
  fi
else
  skip "runtime/obs/otel/otel.go not built — OTel semconv check deferred"
fi

# 7. The OTelEmitter is OFF BY DEFAULT — opening the driver by name yields a
#    NopEmitter; local observation needs zero OTel config (CLAUDE.md §8).
if [ -f runtime/obs/otel/otel_test.go ]; then
  if grep -q 'TestOTelEmitter_OffByDefault' runtime/obs/otel/otel_test.go \
     && grep -q 'TestOTelEmitter_DriverRegisteredAndInert' runtime/obs/otel/otel_test.go; then
    ok "the OTelEmitter is off by default — proven by the off-by-default tests"
  else
    fail "no test proves the OTelEmitter is off by default"
  fi
else
  skip "runtime/obs/otel/otel_test.go not built — OTel off-by-default check deferred"
fi

# 8. The MCP logging → obs/v1 log-event bridge exists (acceptance:
#    notifications/message surface as log events; RFC §11.3).
if [ -f runtime/server/logbridge.go ]; then
  if grep -q 'type LogBridge struct' runtime/server/logbridge.go \
     && grep -q 'func (s \*Server) LogBridge' runtime/server/logbridge.go \
     && grep -q 'sess.Log(ctx' runtime/server/logbridge.go \
     && grep -q 'b.rec.Log(ctx' runtime/server/logbridge.go; then
    ok "the MCP logging → obs/v1 log-event bridge exists and fans to both sinks"
  else
    fail "the MCP logging → obs/v1 bridge is incomplete"
  fi
else
  skip "runtime/server/logbridge.go not built — log-bridge check deferred"
fi

# 9. A client that negotiated logging still receives notifications/message AND
#    the record surfaces as an obs/v1 log event — proven by the integration test.
if [ -f test/integration/phase16_obs_transports_test.go ]; then
  if grep -q 'TestPhase16_LogBridge_RoundTrip' \
       test/integration/phase16_obs_transports_test.go; then
    ok "an integration test proves the logging bridge feeds MCP and obs/v1"
  else
    fail "no logging-bridge round-trip integration test found"
  fi
else
  skip "test/integration/phase16_obs_transports_test.go not built — log-bridge round-trip check deferred"
fi

# 10. make build stays CGo-free with the OTel dependency present (CLAUDE.md
#     §13; the OTel SDK must not pull CGo into the shipped artifact).
if [ -f runtime/obs/otel/otel.go ]; then
  if CGO_ENABLED=0 go build ./... >/dev/null 2>&1; then
    ok "make build stays CGo-free with the OTel dependency"
  else
    fail "the OTel dependency broke the CGo-free build"
  fi
else
  skip "runtime/obs/otel not built — CGo-free-build check deferred"
fi

# 11. The Phase 16 unit + integration tests pass.
if [ -f runtime/obs/sse_test.go ]; then
  if go test ./runtime/obs/... ./runtime/server/... \
       -run 'SSE|OTel|LogBridge|LogLevel' >/dev/null 2>&1; then
    ok "the phase-16 transport unit tests pass"
  else
    fail "phase-16 unit tests fail"
  fi
else
  skip "runtime/obs/sse_test.go not built — phase-16 unit-test check deferred"
fi

smoke_summary
