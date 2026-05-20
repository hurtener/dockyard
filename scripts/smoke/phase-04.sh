#!/usr/bin/env bash
# Smoke script for Phase 04 — contract-first codegen + typed tool builder.
# One assertion per acceptance criterion (docs/plans/phase-04-codegen.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-04 codegen"

# 1. The codegen package exists.
if [ -f internal/codegen/schema.go ]; then
  ok "internal/codegen package exists"
else
  skip "internal/codegen not built"
fi

# 2. The typed tool builder package exists.
if [ -f runtime/tool/builder.go ]; then
  ok "runtime/tool builder package exists"
else
  skip "runtime/tool not built"
fi

# 3. The golden schema fixtures exist and are non-empty.
if [ -d internal/codegen/testdata ] && [ -n "$(ls -A internal/codegen/testdata 2>/dev/null)" ]; then
  ok "codegen golden fixtures exist"
else
  skip "internal/codegen/testdata not built"
fi

# 4. google/jsonschema-go is a direct dependency.
if [ -f internal/codegen/schema.go ]; then
  if grep -q 'github.com/google/jsonschema-go v' go.mod \
     && ! grep -q 'github.com/google/jsonschema-go v.* // indirect' go.mod; then
    ok "google/jsonschema-go is a direct dependency"
  else
    fail "google/jsonschema-go is not a direct dependency in go.mod"
  fi
else
  skip "internal/codegen not built — dependency check deferred"
fi

# 5. The codegen + builder packages build CGo-free.
if [ -f internal/codegen/schema.go ] && [ -f runtime/tool/builder.go ]; then
  if CGO_ENABLED=0 go build ./internal/codegen/... ./runtime/tool/... >/dev/null 2>&1; then
    ok "codegen + builder build CGo-free"
  else
    fail "codegen + builder do not build with CGO_ENABLED=0"
  fi
else
  skip "phase-04 packages not built — build check deferred"
fi

# 6. The codegen + builder tests pass (covers schema generation, the builder,
#    content/structuredContent routing, and golden output).
if [ -f internal/codegen/schema_test.go ] && [ -f runtime/tool/builder_test.go ]; then
  if go test ./internal/codegen/... ./runtime/tool/... >/dev/null 2>&1; then
    ok "codegen + builder tests pass"
  else
    fail "codegen + builder tests fail"
  fi
else
  skip "phase-04 tests not built"
fi

# 7. The Phase 04 integration test passes (codegen -> builder -> server wiring).
if [ -f test/integration/phase04_codegen_test.go ]; then
  if go test ./test/integration/ -run Phase04 >/dev/null 2>&1; then
    ok "phase-04 codegen-to-server integration test passes"
  else
    fail "phase-04 integration test fails"
  fi
else
  skip "phase-04 integration test not built"
fi

smoke_summary
