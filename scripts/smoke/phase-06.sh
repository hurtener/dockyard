#!/usr/bin/env bash
# Smoke script for Phase 06 — manifest (dockyard.app.yaml).
# One assertion per acceptance criterion (docs/plans/phase-06-manifest.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-06 manifest"

# 1. The manifest package exists.
if [ -f internal/manifest/manifest.go ]; then
  ok "internal/manifest package exists"
else
  skip "internal/manifest not built"
fi

# 2. The example manifest exists and is non-empty.
if [ -s examples/customer-health/dockyard.app.yaml ]; then
  ok "example manifest exists and is non-empty"
else
  skip "example manifest not built"
fi

# 3. gopkg.in/yaml.v3 is a direct dependency (not // indirect).
if [ -f internal/manifest/load.go ]; then
  if grep -q 'gopkg.in/yaml.v3 v' go.mod \
     && ! grep -q 'gopkg.in/yaml.v3 v.* // indirect' go.mod; then
    ok "gopkg.in/yaml.v3 is a direct dependency"
  else
    fail "gopkg.in/yaml.v3 is not a direct dependency in go.mod"
  fi
else
  skip "internal/manifest not built — dependency check deferred"
fi

# 4. The manifest package builds CGo-free.
if [ -f internal/manifest/manifest.go ]; then
  if CGO_ENABLED=0 go build ./internal/manifest/... >/dev/null 2>&1; then
    ok "internal/manifest builds CGo-free"
  else
    fail "internal/manifest does not build with CGO_ENABLED=0"
  fi
else
  skip "internal/manifest not built — build check deferred"
fi

# 5. The manifest package tests pass (loader, validation, resolver seam).
if [ -f internal/manifest/load_test.go ]; then
  if go test ./internal/manifest/... >/dev/null 2>&1; then
    ok "manifest loader + validation + resolver tests pass"
  else
    fail "manifest package tests fail"
  fi
else
  skip "internal/manifest tests not built"
fi

# 6. A valid manifest loads + validates and an invalid one fails source-located.
#    Exercised by the table tests; assert the named tests pass.
if [ -f internal/manifest/load_test.go ]; then
  if go test ./internal/manifest/ \
       -run 'TestLoadFile_ExampleManifest|TestValidate_InvalidFixtures' \
       >/dev/null 2>&1; then
    ok "valid manifest round-trips; invalid manifests fail source-located"
  else
    fail "manifest valid/invalid fixture tests fail"
  fi
else
  skip "internal/manifest fixture tests not built"
fi

# 7. The Go type references in tools[].input/output resolve via the codegen seam.
if [ -f internal/manifest/resolve_test.go ]; then
  if go test ./internal/manifest/ -run 'TestResolveContracts_RealResolver' \
       >/dev/null 2>&1; then
    ok "tool input/output contract references resolve through the codegen seam"
  else
    fail "contract-reference resolution test fails"
  fi
else
  skip "internal/manifest resolver tests not built"
fi

# 8. Depth-remediation validations: CSP origins, single-file/CSP coherence,
#    orphan apps, task_support coherence (D-055).
if [ -f internal/manifest/validate.go ]; then
  if grep -q 'func validateOrigin(' internal/manifest/validate.go \
     && grep -q 'validateTaskSupportCoherence' internal/manifest/validate.go \
     && grep -q 'referenced by no tool' internal/manifest/validate.go; then
    ok "manifest CSP/orphan/task_support coherence checks present (D-055)"
  else
    fail "depth-remediation manifest validations missing from validate.go"
  fi
else
  skip "internal/manifest/validate.go not built — coherence check deferred"
fi

smoke_summary
