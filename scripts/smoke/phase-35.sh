#!/usr/bin/env bash
# Smoke script for Phase 35 — inspector, CLI, and quality-gate compatibility.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-35 inspector, CLI, and quality-gate compatibility"

if [ -f docs/plans/phase-35-inspector-cli-2026-compat.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi

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

run_focused_test "install boot check negotiates modern and explicit legacy fallback" ./internal/installpkg '^TestBootCheckNegotiation$'
run_focused_test "inspector modern invoke and MRTR retry" ./internal/inspector '^TestToolsFromServer$'
run_focused_test "scaffold serves modern and legacy HTTP lifecycles" ./test/integration '^TestPhase35ScaffoldDualHTTPLifecycle$'
run_focused_test "test gate reports both offline core revisions" ./internal/testgate '^TestCoreConformance(Golden|PartialFailure)$'

if grep -q 'ProtocolMode: server.Dual' templates/analytics-widgets/main.go.tmpl &&
   grep -q 'ProtocolMode: server.Dual' templates/approval-flows/main.go.tmpl; then
  ok "named templates default to dual HTTP lifecycle"
else
  fail "named templates default to dual HTTP lifecycle"
fi

if grep -q 'server/discover' docs/site/guides/inspector.md &&
   grep -q 'ui/initialize' docs/site/guides/inspector.md &&
   grep -q 'server/discover' skills/test-with-the-inspector/SKILL.md &&
   grep -q 'ui/initialize' skills/test-with-the-inspector/SKILL.md; then
  ok "docs and inspector skill distinguish base discovery from Apps initialization"
else
  fail "docs and inspector skill distinguish base discovery from Apps initialization"
fi

if [ "$SMOKE_OK" -lt 6 ]; then
  fail "acceptance criteria require at least 6 OK checks (got $SMOKE_OK)"
fi

smoke_summary
