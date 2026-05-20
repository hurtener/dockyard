#!/usr/bin/env bash
# Smoke script for Phase 03 — Store seam + sqlite/inmem drivers + conformance.
# One assertion per acceptance criterion (docs/plans/phase-03-store-seam.md).
# A check against an unbuilt surface skipsrather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-03 store-seam"

# 1. The Store seam package exists.
if [ -f runtime/store/store.go ]; then
  ok "runtime/store seam package exists"
else
  skip "runtime/store not built"
fi

# 2. Both V1 drivers exist.
if [ -f runtime/store/inmem/inmem.go ]; then
  ok "in-memory driver exists"
else
  skip "runtime/store/inmem not built"
fi
if [ -f runtime/store/sqlitestore/sqlitestore.go ]; then
  ok "modernc.org/sqlite driver exists"
else
  skip "runtime/store/sqlitestore not built"
fi

# 3. The shared conformance suite exists.
if [ -f runtime/store/storetest/conformance.go ]; then
  ok "shared conformance suite exists"
else
  skip "runtime/store/storetest not built"
fi

# 4. The build is CGo-free.
if [ -f runtime/store/store.go ]; then
  if CGO_ENABLED=0 go build ./runtime/store/... >/dev/null 2>&1; then
    ok "CGO_ENABLED=0 go build ./runtime/store/... succeeds"
  else
    fail "CGo-free build of runtime/store/... failed"
  fi
else
  skip "CGo-free build: runtime/store not built"
fi

# 5. Conformance + concurrency tests pass (both drivers, under the seam).
if [ -f runtime/store/storetest/conformance.go ]; then
  if CGO_ENABLED=0 go test ./runtime/store/... >/dev/null 2>&1; then
    ok "CGO_ENABLED=0 go test ./runtime/store/... passes"
  else
    fail "store conformance/concurrency tests failed"
  fi
else
  skip "store tests: runtime/store not built"
fi

# 6. modernc.org/sqlite is a declared dependency.
if [ -f go.mod ] && grep -q 'modernc.org/sqlite' go.mod; then
  ok "modernc.org/sqlite declared in go.mod"
else
  skip "modernc.org/sqlite not yet a dependency"
fi

smoke_summary
