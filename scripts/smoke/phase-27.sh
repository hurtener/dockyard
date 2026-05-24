#!/usr/bin/env bash
# Smoke script for Phase 27 — Security pass + spec-compliance conformance.
# Asserts that the audit surfaces, the conformance suite, and the CI hygiene
# fix all landed. A check against an unbuilt surface SKIPs rather than FAILs
# so the script tolerates earlier-phase fallbacks; Phase 27 itself is
# expected to be fully shipped, so missing assertions WILL fail.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-27 security + conformance"

# 1. Hostile-input fuzz coverage at every wire-decoding surface.
for surface in \
    internal/protocolcodec/fuzz_test.go \
    internal/manifest/fuzz_test.go \
    internal/codegen/fuzz_test.go \
    runtime/tool/fuzz_test.go \
    runtime/tasks/fuzz_test.go \
    internal/inspector/fuzz_test.go ; do
  if [ -f "$surface" ] && grep -q '^func Fuzz' "$surface"; then
    ok "fuzz target present: $surface"
  else
    fail "fuzz target missing: $surface"
  fi
done

# 2. HTTPSecurity stress under concurrent + adversarial load.
if [ -f test/integration/phase27_httpsecurity_stress_test.go ] && \
    grep -q 'TestPhase27_HTTPSecurity_StressUnderAdversarialLoad' \
    test/integration/phase27_httpsecurity_stress_test.go; then
  ok "HTTPSecurity stress integration test present"
else
  fail "HTTPSecurity stress integration test missing"
fi

# 3. Tasks security adversarial sweep.
if [ -f test/integration/phase27_tasks_security_test.go ] && \
    grep -q 'TestPhase27_TasksSecurity' \
    test/integration/phase27_tasks_security_test.go; then
  ok "Tasks security adversarial integration test present"
else
  fail "Tasks security adversarial integration test missing"
fi

# 4. Inspector security re-audit.
if [ -f test/integration/phase27_inspector_security_test.go ] && \
    grep -q 'TestPhase27_InspectorSecurity' \
    test/integration/phase27_inspector_security_test.go; then
  ok "Inspector security re-audit integration test present"
else
  fail "Inspector security re-audit integration test missing"
fi

# 5. MCP spec-compliance conformance suite.
if [ -d test/conformance ] && compgen -G "test/conformance/*_test.go" >/dev/null; then
  ok "conformance suite directory + at least one test file"
else
  fail "conformance suite missing under test/conformance/"
fi

# 6. The Makefile coverage diagnostic-hygiene fix is in place: the recipe
#    must no longer redirect go test output unconditionally to /dev/null.
if grep -E 'go test .* >/dev/null' Makefile >/dev/null 2>&1; then
  fail "Makefile coverage recipe still suppresses go test output unconditionally"
else
  ok "Makefile coverage recipe no longer suppresses go test output unconditionally"
fi

# 7. The D-143..D-N range for Phase 27 is filed.
if grep -qE '^## D-14[3-9]\b' docs/decisions.md; then
  ok "Phase 27 decisions D-143..D-149 filed"
else
  fail "Phase 27 reserved decisions D-143..D-149 not filed in docs/decisions.md"
fi

# 8. Compile every test target — no skipped builds.
if go vet ./test/integration/... ./test/conformance/... ./internal/inspector/... \
    ./internal/protocolcodec/... ./internal/manifest/... ./internal/codegen/... \
    ./runtime/tasks/... ./runtime/tool/... >/dev/null 2>&1; then
  ok "Phase 27 packages compile under go vet"
else
  fail "Phase 27 packages fail to compile under go vet"
fi

smoke_summary
