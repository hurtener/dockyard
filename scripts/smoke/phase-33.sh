#!/usr/bin/env bash
# Smoke script for Phase 33 — Apps, Tasks, and multi-round-trip requests.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-33 Apps, Tasks, and MRTR"

if [ -f docs/plans/phase-33-apps-tasks-mrtr.md ]; then ok "phase plan exists"; else fail "phase plan missing"; fi
if [ -f docs/plans/phase-33-protocol-design.md ]; then ok "protocol design note exists"; else fail "protocol design note missing"; fi
if [ -f internal/protocolcodec/tasks_modern.go ]; then ok "modern Tasks codec exists"; else fail "modern Tasks codec missing"; fi
if [ -f internal/protocolcodec/tasks_modern_test.go ]; then ok "modern Tasks codec tests exist"; else fail "modern Tasks codec tests missing"; fi
if [ -f runtime/tasks/modern_test.go ]; then ok "modern Tasks lifecycle tests exist"; else fail "modern Tasks lifecycle tests missing"; fi
if [ -f runtime/server/modern_tasks_test.go ]; then ok "modern Tasks HTTP integration test exists"; else fail "modern Tasks HTTP integration test missing"; fi
if grep -q 'tasks/update' docs/site/getting-started/approval-flows.md && grep -q 'requestState' docs/site/getting-started/approval-flows.md; then ok "approval docs distinguish task update and core MRTR"; else fail "approval docs do not distinguish task update and core MRTR"; fi
if grep -q 'Modern task input always uses `tasks/update`' skills/test-with-the-inspector/SKILL.md; then ok "inspector skill documents modern task input"; else fail "inspector skill modern task input guidance missing"; fi

smoke_summary
