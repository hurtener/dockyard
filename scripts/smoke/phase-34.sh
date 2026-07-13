#!/usr/bin/env bash
# Smoke script for Phase 34 — contracts and server response semantics.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-34 contracts and server semantics"

if [ -f docs/plans/phase-34-contracts-server-semantics.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi

run_focused_test() {
  label=$1
  package=$2
  pattern=$3
  if output=$(go test -race "$package" -run "$pattern" -count=1 2>&1); then
    ok "$label"
  else
    fail "$label"
    printf '%s\n' "$output"
  fi
}

schema_goldens="
internal/codegen/testdata/scalars_input.golden
internal/codegen/testdata/nested_output.golden
internal/codegen/testdata/show_revenue_input.golden
internal/codegen/testdata/show_revenue_output.golden
internal/codegen/testdata/shapes_contract.golden
internal/codegen/testdata/embedded_event.golden
internal/codegen/testdata/recursive_node.golden
"
goldens_2020_12=true
for golden in $schema_goldens; do
  if [ ! -f "$golden" ] || ! grep -q 'https://json-schema.org/draft/2020-12/schema' "$golden"; then
    goldens_2020_12=false
    break
  fi
done
if $goldens_2020_12; then
  run_focused_test "2020-12 composition, enum, embedded, and recursive schema goldens" ./internal/codegen '^TestGoldenSchemas$'
else
  fail "2020-12 composition, enum, embedded, and recursive schema goldens"
fi

run_focused_test "recursive generation and local refs resolve safely" ./internal/codegen '^(TestSchemaFor_RecursiveReferences|TestRecursiveSchemaPreservesSpecialPointersEmbeddingAndInstances|TestValidateSchemaAcceptsLocalComposition|TestValidateSchemaAcceptsFragmentAndAnchorRefs)$'
run_focused_test "schema validation is bounded and rejects external refs" ./internal/codegen '^(TestValidateSchemaRejectsWrongDialectAndExternalRefs|TestValidateSchemaBoundsDepth)$'
run_focused_test "structured output carries arbitrary JSON values and explicit null" ./runtime/server '^(TestStructuredOutputSupportsPrimitiveAndExplicitNull|TestStructuredPresentTypedNilKinds)$'
run_focused_test "modern resource errors and list/templates/read cache metadata conform" ./runtime/server '^(TestCachePolicyValidation|TestModernResourceSemanticsRealHTTP)$'
run_focused_test "modern discovery and result discriminators conform on the production wire" ./runtime/server '^(TestModernDiscoveryRawWireConforms|TestModernResultTypePreservesInputRequired|TestModernToolsCallReturnsFlatCreateTaskResultOverSDKHTTP)$'
run_focused_test "legacy resource errors remain versioned and omit cache metadata" ./runtime/server '^TestLegacyResourceResponseOmitsCacheAndUsesLegacyMissingCode$'
run_focused_test "generate emits recursive, enum, and scalar 2020-12 contracts" ./internal/generate '^TestPlan_EnumsRecursiveAndScalarOutput$'
run_focused_test "validate blocks stale and externally referenced generated schemas" ./internal/validate '^(TestRun_StaleCodegenIsBlocker|TestCheckSchemas_RejectsExternalReference)$'
run_focused_test "test gate rejects stale and nonconformant generated output" ./internal/testgate '^(TestRun_ContractRegressionFailsTheGate|TestRunGolden_RejectsNonconformantSchema)$'

if [ "$SMOKE_OK" -lt 6 ]; then
  fail "acceptance criteria require at least 6 OK checks (got $SMOKE_OK)"
fi

smoke_summary
