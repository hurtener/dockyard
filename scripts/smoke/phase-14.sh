#!/usr/bin/env bash
# Smoke script for Phase 14 — TaskStore + TaskHandle + task security: the
# durable TaskStore on the Store seam, the TaskHandle handler API, the
# manifest-tunable lifecycle controls (max TTL, per-requestor concurrency cap,
# background TTL purge sweep), the task security model, and the folded-in
# tasks/* transport mount. One assertion per acceptance criterion
# (docs/plans/phase-14-taskstore.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-14 taskstore"

# 1. The durable TaskStore driver exists and builds CGo-free (no-CGo runtime
#    guarantee — modernc.org/sqlite is pure-Go; AGENTS.md §13).
if [ -f runtime/tasks/storedriver.go ]; then
  if CGO_ENABLED=0 go build ./runtime/tasks/... >/dev/null 2>&1; then
    ok "durable TaskStore builds CGo-free"
  else
    fail "runtime/tasks does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/tasks/storedriver.go not built"
fi

# 2. The durable TaskStore is a facade over the Store seam (RFC §13, D-070):
#    storedriver.go imports runtime/store and registers a forward-only migration.
if [ -f runtime/tasks/storedriver.go ]; then
  if grep -q 'runtime/store' runtime/tasks/storedriver.go \
     && grep -q 'RegisterMigrations' runtime/tasks/storedriver.go \
     && grep -q 'AddMigration' runtime/tasks/storedriver.go; then
    ok "durable TaskStore is a Store-seam facade with a forward-only migration"
  else
    fail "durable TaskStore is not built on the Store seam"
  fi
else
  skip "runtime/tasks/storedriver.go not built — Store-seam check deferred"
fi

# 3. The TaskHandle handler API is present — progress + cooperative
#    cancellation + input_required elicitation (acceptance: a long handler
#    reports progress and is cancellable; RFC §8.4).
if [ -f runtime/tasks/handle.go ]; then
  if grep -q 'TaskHandle' runtime/tasks/handle.go \
     && grep -q 'Progress' runtime/tasks/handle.go \
     && grep -q 'Cancelled' runtime/tasks/handle.go \
     && grep -q 'RequireInput' runtime/tasks/handle.go; then
    ok "TaskHandle API (progress, cooperative cancellation, input_required) is present"
  else
    fail "runtime/tasks TaskHandle API is incomplete"
  fi
else
  skip "runtime/tasks/handle.go not built — TaskHandle check deferred"
fi

# 4. The background TTL purge sweep is present (acceptance: TTL purge works;
#    RFC §8.5).
if [ -f runtime/tasks/lifecycle.go ]; then
  if grep -q 'purgeSweep' runtime/tasks/lifecycle.go \
     && grep -q 'PurgeExpired' runtime/tasks/store.go; then
    ok "background TTL purge sweep is present"
  else
    fail "runtime/tasks TTL purge sweep is missing"
  fi
else
  skip "runtime/tasks/lifecycle.go not built — purge-sweep check deferred"
fi

# 5. Auth-context binding rejects cross-context access (acceptance: cross-context
#    task access rejected; RFC §8.5, brief 02 §4.5).
if [ -f runtime/tasks/security.go ]; then
  if grep -q 'DispatchAs' runtime/tasks/security.go \
     && grep -q 'bindTaskAccess' runtime/tasks/security.go \
     && grep -q 'ErrCrossContext' runtime/tasks/errors.go; then
    ok "auth-context binding rejects cross-context tasks/get|result|cancel"
  else
    fail "runtime/tasks auth-context binding is missing"
  fi
else
  skip "runtime/tasks/security.go not built — auth-binding check deferred"
fi

# 6. tasks/list is withheld when requestors are not identifiable (acceptance:
#    tasks/list withheld when unauthed; RFC §8.5).
if [ -f runtime/tasks/engine.go ]; then
  if grep -q 'RequestorIdentifiable' runtime/tasks/engine.go \
     && grep -q 'listOn:.*&&.*identifiable' runtime/tasks/engine.go; then
    ok "tasks/list is withheld when requestors are not identifiable"
  else
    fail "runtime/tasks does not gate tasks/list on requestor identifiability"
  fi
else
  skip "runtime/tasks/engine.go not built — tasks/list-withholding check deferred"
fi

# 7. The transport mount routes tasks/* into Engine.Dispatch (folded-in
#    acceptance: a real MCP client drives tasks/* over a transport; RFC §8.2).
if [ -f runtime/tasks/transport.go ]; then
  if grep -q 'func NewMount' runtime/tasks/transport.go \
     && grep -q 'HTTPMiddleware' runtime/tasks/transport.go \
     && grep -q 'DispatchAs' runtime/tasks/transport.go \
     && grep -q 'capabilities.tasks\|CapabilityFrame' runtime/tasks/transport.go; then
    ok "tasks/* transport mount routes frames into the engine and injects the capability"
  else
    fail "runtime/tasks transport mount is incomplete"
  fi
else
  skip "runtime/tasks/transport.go not built — transport-mount check deferred"
fi

# 8. The four tasks lifecycle manifest keys are defined and load (acceptance:
#    each new manifest key documented; AGENTS.md §4.2).
if [ -f internal/manifest/manifest.go ]; then
  if grep -q 'max_ttl_millis' internal/manifest/manifest.go \
     && grep -q 'default_ttl_millis' internal/manifest/manifest.go \
     && grep -q 'purge_interval_millis' internal/manifest/manifest.go \
     && grep -q 'max_concurrent_per_requestor' internal/manifest/manifest.go; then
    ok "the four tasks lifecycle manifest keys are defined"
  else
    fail "internal/manifest is missing a tasks lifecycle key"
  fi
else
  skip "internal/manifest/manifest.go not built — manifest-key check deferred"
fi

# 9. The example manifest carries the tasks block.
if [ -f examples/customer-health/dockyard.app.yaml ]; then
  if grep -q '^tasks:' examples/customer-health/dockyard.app.yaml; then
    ok "the example manifest documents the tasks block"
  else
    fail "the example manifest is missing the tasks block"
  fi
else
  skip "example manifest not present"
fi

# 10. The shared TaskStore conformance suite exists and runs against every
#     backing (CLAUDE.md §9 — proven by the shared conformance suite).
if [ -f runtime/tasks/taskstoretest/conformance.go ]; then
  if grep -q 'RunConformance' runtime/tasks/taskstoretest/conformance.go \
     && grep -q 'TestDurableTaskStore_OverSQLiteStore' runtime/tasks/storedriver_test.go; then
    ok "the shared TaskStore conformance suite covers the durable driver"
  else
    fail "the TaskStore conformance suite does not cover the durable driver"
  fi
else
  skip "runtime/tasks/taskstoretest not built — conformance-suite check deferred"
fi

# 11. The runtime/tasks, taskstoretest and manifest tests pass.
if [ -f runtime/tasks/storedriver_test.go ]; then
  if go test ./runtime/tasks/... ./internal/manifest/... >/dev/null 2>&1; then
    ok "runtime/tasks, taskstoretest and manifest tests pass"
  else
    fail "phase-14 unit tests fail"
  fi
else
  skip "phase-14 tests not built"
fi

# 12. The Phase 14 integration test exists (real durable TaskStore over sqlite).
if [ -f test/integration/phase14_taskstore_test.go ]; then
  ok "phase-14 integration test present"
else
  skip "test/integration/phase14_taskstore_test.go not built"
fi

smoke_summary
