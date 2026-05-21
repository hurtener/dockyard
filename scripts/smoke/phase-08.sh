#!/usr/bin/env bash
# Smoke script for Phase 08 — tool handler runtime: Result, content split,
# edge validation. One assertion per acceptance criterion
# (docs/plans/phase-08-handler-runtime.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-08 handler-runtime"

# 1. The handler runtime file exists.
if [ -f runtime/tool/runtime.go ]; then
  ok "runtime/tool handler runtime exists"
else
  skip "runtime/tool/runtime.go not built"
fi

# 2. The routing-flag surface exists and defines both flag kinds.
if [ -f runtime/tool/flag.go ]; then
  if grep -q 'FlagOversizeOutput' runtime/tool/flag.go \
     && grep -q 'FlagMisroutedContent' runtime/tool/flag.go; then
    ok "routing flags (oversize + misroute) defined"
  else
    fail "runtime/tool/flag.go missing FlagOversizeOutput / FlagMisroutedContent"
  fi
else
  skip "runtime/tool/flag.go not built"
fi

# 3. The empty-TextContent quirk is fixed: AddToolWithSchemas no longer emits an
#    unconditional TextContent block (D-043).
if [ -f runtime/server/tool.go ]; then
  if grep -q 'if out.Text != ""' runtime/server/tool.go; then
    ok "empty-TextContent quirk fixed — content block is conditional (D-043)"
  else
    fail "runtime/server/tool.go still emits an unconditional TextContent block"
  fi
else
  skip "runtime/server/tool.go not built — D-043 check deferred"
fi

# 4. Typed edge-validation error surface is present (D-044).
if [ -f runtime/tool/runtime.go ]; then
  if grep -q 'ErrInvalidArguments' runtime/tool/runtime.go \
     && grep -q 'type ArgumentError struct' runtime/tool/runtime.go; then
    ok "typed edge-validation error (ErrInvalidArguments / ArgumentError) present"
  else
    fail "runtime/tool/runtime.go missing the typed ArgumentError surface"
  fi
else
  skip "runtime/tool/runtime.go not built — edge-validation check deferred"
fi

# 5. The Builder.Flags() accessor is present.
if [ -f runtime/tool/builder.go ]; then
  if grep -q 'func (b \*Builder\[In, Out\]) Flags()' runtime/tool/builder.go; then
    ok "Builder.Flags() accessor present"
  else
    fail "Builder.Flags() accessor missing from builder.go"
  fi
else
  skip "runtime/tool/builder.go not built — Flags check deferred"
fi

# 6. runtime/tool builds CGo-free.
if [ -f runtime/tool/runtime.go ]; then
  if CGO_ENABLED=0 go build ./runtime/tool/... >/dev/null 2>&1; then
    ok "runtime/tool builds CGo-free"
  else
    fail "runtime/tool does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/tool not built — build check deferred"
fi

# 7. The handler-runtime and server tests pass.
if [ -f runtime/tool/runtime_test.go ]; then
  if go test ./runtime/tool/... ./runtime/server/... >/dev/null 2>&1; then
    ok "runtime/tool and runtime/server tests pass"
  else
    fail "runtime/tool / runtime/server tests fail"
  fi
else
  skip "phase-08 tests not built"
fi

# 8. The Phase 08 integration test exists (handler runtime over a real transport).
if [ -f test/integration/phase08_handler_runtime_test.go ]; then
  ok "phase-08 integration test present"
else
  skip "test/integration/phase08_handler_runtime_test.go not built"
fi

smoke_summary
