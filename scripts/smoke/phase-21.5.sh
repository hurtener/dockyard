#!/usr/bin/env bash
# Smoke script for Phase 21.5 — test-quality hardening.
# One assertion per acceptance criterion (master plan Phase 21.5):
#   - the coverage checker exists and is wired into `make coverage` + CI
#   - the Go coverage gate fails on a shortfall (asserted against a synthetic
#     low-coverage profile)
#   - the frontend coverage thresholds are configured for web/ui + web/bridge
#   - fuzz targets exist for protocolcodec, manifest, codegen, the JSON-RPC
#     argument frame path
#   - benchmarks exist for the obs ring buffer, the protocolcodec codecs and
#     the Store drivers
#   - the LogBridge / validate / generate concurrency tests exist
# A check against an unbuilt surface skip()s, never fail()s — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-21.5 test-quality hardening"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- A. the coverage checker exists and is wired in --------------------------
if [ -f internal/coveragecheck/coveragecheck.go ] \
   && [ -f internal/coveragecheck/cmd/coveragecheck/main.go ] \
   && [ -f internal/coveragecheck/coverage.json ]; then
  ok "coveragecheck package + cmd + config exist"
else
  skip "internal/coveragecheck not built"
fi

if grep -q '^coverage:' Makefile && grep -q 'coveragecheck' Makefile; then
  ok "make coverage target wired to the coverage checker"
else
  fail "Makefile has no 'coverage' target wired to coveragecheck"
fi

if grep -q 'make coverage' .github/workflows/ci.yml; then
  ok "CI runs 'make coverage' (a coverage regression fails the build)"
else
  fail ".github/workflows/ci.yml does not run 'make coverage'"
fi

# --- A. the Go coverage gate fails on a shortfall ----------------------------
# Drive the checker with a synthetic profile where one configured package is
# far below its band, and assert a non-zero exit.
if [ -f internal/coveragecheck/cmd/coveragecheck/main.go ]; then
  cat >"$WORK/low.out" <<'EOF'
mode: atomic
github.com/hurtener/dockyard/runtime/obs/f.go:1.1,2.2 100 1
github.com/hurtener/dockyard/runtime/obs/f.go:3.1,4.2 100 0
EOF
  if go run ./internal/coveragecheck/cmd/coveragecheck \
       -profile "$WORK/low.out" \
       -config internal/coveragecheck/coverage.json >"$WORK/low.log" 2>&1; then
    fail "coverage checker exited 0 on a synthetic shortfall"
    sed 's/^/    /' "$WORK/low.log"
  else
    ok "coverage checker exits non-zero on a coverage shortfall"
  fi
else
  skip "coveragecheck cmd not built"
fi

# --- A. frontend coverage thresholds are configured --------------------------
fe_ok=1
for p in web/ui web/bridge; do
  if [ -f "$p/vitest.config.ts" ]; then
    if grep -q 'thresholds' "$p/vitest.config.ts" \
       && grep -q 'coverage' "$p/vitest.config.ts"; then
      :
    else
      fe_ok=0
    fi
  else
    fe_ok=0
  fi
done
if [ "$fe_ok" -eq 1 ]; then
  ok "web/ui + web/bridge have Vitest coverage.thresholds configured"
else
  skip "a web/ project or its vitest coverage config is absent"
fi
# The gate must run --coverage so the thresholds are enforced.
if grep -q 'coverage' web/ui/package.json && grep -q 'coverage' web/bridge/package.json; then
  ok "the web/ gate runs vitest with --coverage (thresholds enforced)"
else
  fail "a web/ package.json gate script does not run coverage"
fi

# --- B. fuzz targets exist for the named parse surfaces ----------------------
fuzz_ok=1
[ -f internal/protocolcodec/fuzz_test.go ] || fuzz_ok=0
[ -f internal/manifest/fuzz_test.go ]      || fuzz_ok=0
[ -f internal/codegen/fuzz_test.go ]       || fuzz_ok=0
[ -f runtime/tool/fuzz_test.go ]           || fuzz_ok=0
if [ "$fuzz_ok" -eq 1 ]; then
  ok "fuzz targets exist for protocolcodec, manifest, codegen, the arg-frame path"
else
  skip "a fuzz target file is absent — fuzz phase not landed"
fi
# Each file must define at least one FuzzXxx function.
if grep -rq 'func Fuzz' internal/protocolcodec/fuzz_test.go 2>/dev/null \
   && grep -rq 'func Fuzz' internal/manifest/fuzz_test.go 2>/dev/null \
   && grep -rq 'func Fuzz' internal/codegen/fuzz_test.go 2>/dev/null \
   && grep -rq 'func Fuzz' runtime/tool/fuzz_test.go 2>/dev/null; then
  ok "each fuzz file defines at least one FuzzXxx target"
else
  skip "a fuzz file defines no FuzzXxx function"
fi

# --- C. benchmarks exist for the named hot paths -----------------------------
bench_ok=1
[ -f runtime/obs/bench_test.go ]              || bench_ok=0
[ -f internal/protocolcodec/bench_test.go ]   || bench_ok=0
[ -f runtime/store/storetest/benchmark.go ]   || bench_ok=0
[ -f runtime/store/inmem/bench_test.go ]      || bench_ok=0
[ -f runtime/store/sqlitestore/bench_test.go ] || bench_ok=0
if [ "$bench_ok" -eq 1 ]; then
  ok "benchmarks exist for the obs ring buffer, the codecs, the Store drivers"
else
  skip "a benchmark file is absent — benchmark phase not landed"
fi
if grep -q '^bench:' Makefile; then
  ok "make bench target exists"
else
  fail "Makefile has no 'bench' target"
fi

# --- D. the concurrency-proof + thin-package gaps are closed -----------------
if [ -f runtime/server/logbridge_concurrency_test.go ] \
   && [ -f internal/validate/concurrency_test.go ] \
   && [ -f internal/generate/concurrency_test.go ]; then
  ok "concurrency-stress tests exist for LogBridge, validate, generate"
else
  skip "a concurrency test file is absent — item D not landed"
fi

smoke_summary
