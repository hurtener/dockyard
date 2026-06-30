#!/usr/bin/env bash
# Smoke script for v1.8 wave A — thread inbound request _meta to tool handlers.
# Plan: docs/plans/thread-request-meta.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.8-wave-A request-meta"

TOOL=runtime/server/tool.go

# 1. The public RequestMeta / WithRequestMeta accessor pair exists.
if [ -f "$TOOL" ] && \
   grep -q 'func RequestMeta(ctx context.Context)' "$TOOL" && \
   grep -q 'func WithRequestMeta(ctx context.Context' "$TOOL"; then
  ok "RequestMeta / WithRequestMeta seam present"
else
  skip "request-meta accessor pair not built yet"
fi

# 2. The accessor exposes the stdlib map[string]any, not the raw SDK mcpsdk.Meta
#    (P3 / §13 — handler-facing APIs expose no raw protocol type).
if [ -f "$TOOL" ] && \
   grep -q 'func RequestMeta(ctx context.Context) map\[string\]any' "$TOOL"; then
  ok "RequestMeta returns map[string]any, not mcpsdk.Meta (P3)"
else
  skip "RequestMeta return type not yet the stdlib map (P3 check deferred)"
fi

# 3. Both registration wrappers thread params._meta onto the handler context.
if [ -f "$TOOL" ]; then
  meta_threads=$(grep -c 'WithRequestMeta(ctx, req.Params.Meta)' "$TOOL" || true)
  if [ "$meta_threads" -ge 2 ]; then
    ok "both AddTool and AddToolWithSchemas thread params._meta ($meta_threads sites)"
  else
    fail "expected params._meta threaded in both wrappers, found $meta_threads site(s)"
  fi
else
  skip "runtime/server/tool.go not built — wrapper-threading check deferred"
fi

# 4. The behavioural test exists and the runtime/server tests pass under -race
#    (round-trip, no-op, defensive copy, and end-to-end across both wrappers).
if [ -f runtime/server/requestmeta_test.go ]; then
  if go test -race -run RequestMeta ./runtime/server/... >/dev/null 2>&1; then
    ok "request-meta behavioural tests pass under -race"
  else
    fail "runtime/server request-meta tests fail"
  fi
else
  skip "runtime/server/requestmeta_test.go not built"
fi

# 5. runtime/server still builds CGo-free.
if CGO_ENABLED=0 go build ./runtime/server/... >/dev/null 2>&1; then
  ok "runtime/server builds CGo-free"
else
  fail "runtime/server does not build with CGO_ENABLED=0"
fi

# 6. The decision is recorded (D-189).
if grep -q "D-189" docs/decisions.md 2>/dev/null; then
  ok "docs/decisions.md records D-189 (inbound request _meta seam)"
else
  skip "D-189 not recorded yet"
fi

smoke_summary
