#!/usr/bin/env bash
# Smoke script for Phase 13 — MCP Tasks extension, server-side: tasks/* method
# routing, the tasks capability advertisement, CreateTaskResult substitution
# for a task-augmented tools/call, and the enforced five-status lifecycle. One
# assertion per acceptance criterion (docs/plans/phase-13-tasks-server.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-13 tasks-server"

# 1. The runtime/tasks package exists.
if [ -f runtime/tasks/engine.go ]; then
  ok "runtime/tasks package exists"
else
  skip "runtime/tasks/engine.go not built"
fi

# 2. runtime/tasks builds CGo-free (no-CGo runtime guarantee, AGENTS.md §13).
if [ -f runtime/tasks/engine.go ]; then
  if CGO_ENABLED=0 go build ./runtime/tasks/... >/dev/null 2>&1; then
    ok "runtime/tasks builds CGo-free"
  else
    fail "runtime/tasks does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/tasks not built — build check deferred"
fi

# 3. The four tasks/* methods are routed (acceptance: tasks/get/result/cancel/list
#    behave per spec).
if [ -f runtime/tasks/engine.go ]; then
  if grep -q '"tasks/get"' runtime/tasks/engine.go \
     && grep -q '"tasks/result"' runtime/tasks/engine.go \
     && grep -q '"tasks/cancel"' runtime/tasks/engine.go \
     && grep -q '"tasks/list"' runtime/tasks/engine.go; then
    ok "all four tasks/* methods are defined and routed"
  else
    fail "runtime/tasks is missing one of the tasks/* methods"
  fi
else
  skip "runtime/tasks not built — method check deferred"
fi

# 4. CreateTaskResult substitution for a task-augmented tools/call exists
#    (acceptance: a task-augmented call returns CreateTaskResult).
if [ -f runtime/tasks/engine.go ]; then
  if grep -q 'CreateForToolCall' runtime/tasks/engine.go \
     && grep -q 'EncodeCreateTaskResult' runtime/tasks/engine.go; then
    ok "CreateTaskResult substitution for task-augmented tools/call is present"
  else
    fail "runtime/tasks is missing the CreateTaskResult substitution"
  fi
else
  skip "runtime/tasks not built — CreateTaskResult check deferred"
fi

# 5. The five-status lifecycle is enforced through a typed error, never a panic
#    (acceptance: lifecycle transitions enforced).
if [ -f runtime/tasks/errors.go ]; then
  if grep -q 'ErrIllegalTransition' runtime/tasks/errors.go \
     && grep -q 'CanTransitionTo' runtime/tasks/store.go; then
    ok "lifecycle transitions are enforced via a typed error"
  else
    fail "runtime/tasks lifecycle enforcement is missing"
  fi
else
  skip "runtime/tasks/errors.go not built — lifecycle check deferred"
fi

# 6. The tasks capability is advertised, capability-driven (acceptance: the
#    tasks capability is advertised; no host matrix).
if [ -f runtime/tasks/capability.go ]; then
  if grep -q 'CapabilityJSON' runtime/tasks/capability.go \
     && grep -q 'EncodeTasksServerCapability' runtime/tasks/capability.go; then
    ok "tasks capability advertisement is present and capability-driven"
  else
    fail "runtime/tasks tasks-capability advertisement is missing"
  fi
else
  skip "runtime/tasks/capability.go not built — capability check deferred"
fi

# 7. Every tasks/* wire shape goes through internal/protocolcodec (P3):
#    runtime/tasks imports protocolcodec and hand-builds no raw envelope keys.
#    A struct-tagged `json:"..."` for a Tasks envelope key outside the seam is
#    the drift this guards; a slog field name is not a wire shape, so the check
#    matches only JSON struct tags. The mechanical, module-wide P3 guard is
#    TestNoRawWireTypeImportsOutsideSeam in internal/protocolcodec.
if [ -f runtime/tasks/dispatch.go ]; then
  if grep -rq 'internal/protocolcodec' runtime/tasks/*.go \
     && ! grep -rEq 'json:"(taskId|nextCursor|statusMessage|pollInterval|createdAt|lastUpdatedAt)"' \
          runtime/tasks/*.go; then
    ok "runtime/tasks routes wire shapes through protocolcodec — no raw keys (P3)"
  else
    fail "runtime/tasks constructs a raw Tasks envelope shape (P3 violation)"
  fi
else
  skip "runtime/tasks not built — P3 check deferred"
fi

# 8. The runtime/tasks and protocolcodec tests pass.
if [ -f runtime/tasks/engine_test.go ]; then
  if go test ./runtime/tasks/... ./internal/protocolcodec/... >/dev/null 2>&1; then
    ok "runtime/tasks and protocolcodec tests pass"
  else
    fail "runtime/tasks / protocolcodec tests fail"
  fi
else
  skip "phase-13 tests not built"
fi

# 9. The Phase 13 integration test exists (real engine + real protocolcodec).
if [ -f test/integration/phase13_tasks_test.go ]; then
  ok "phase-13 integration test present"
else
  skip "test/integration/phase13_tasks_test.go not built"
fi

smoke_summary
