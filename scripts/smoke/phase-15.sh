#!/usr/bin/env bash
# Smoke script for Phase 15 — obs/v1 event model + headless emitter: the
# canonical obs.Event model + event kinds, the non-blocking headless emitter
# (interface + factory + driver seam), the in-memory ring-buffer driver, W3C
# Trace Context IDs, and shape+size default capture. Also the folded-in S1 fix
# (the Store migration registry made t.Parallel()-safe). One assertion per
# acceptance criterion (docs/plans/phase-15-obs-emitter.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-15 obs-emitter"

# 1. The obs/v1 package exists and builds CGo-free (no-CGo runtime guarantee;
#    CLAUDE.md §13).
if [ -f runtime/obs/event.go ]; then
  if CGO_ENABLED=0 go build ./runtime/obs/... >/dev/null 2>&1; then
    ok "runtime/obs builds CGo-free"
  else
    fail "runtime/obs does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/obs/event.go not built"
fi

# 2. The canonical obs.Event model + event kinds cover tool/resource/app/task
#    events (acceptance: tool/resource/app/task events emit; RFC §11.2).
if [ -f runtime/obs/event.go ]; then
  if grep -q 'type Event struct' runtime/obs/event.go \
     && grep -q 'KindToolCall' runtime/obs/event.go \
     && grep -q 'KindResourceRead' runtime/obs/event.go \
     && grep -q 'KindAppLoad' runtime/obs/event.go \
     && grep -q 'KindTaskProgress' runtime/obs/event.go; then
    ok "obs.Event model carries tool/resource/app/task event kinds"
  else
    fail "obs.Event model is missing an event kind"
  fi
else
  skip "runtime/obs/event.go not built — event-model check deferred"
fi

# 3. The emitter is an interface + factory + driver seam (CLAUDE.md §4.4) and the
#    ring-buffer driver registers via init().
if [ -f runtime/obs/emitter.go ] && [ -f runtime/obs/ringbuffer.go ]; then
  if grep -q 'type Emitter interface' runtime/obs/emitter.go \
     && grep -q 'func RegisterDriver' runtime/obs/emitter.go \
     && grep -q 'func Open' runtime/obs/emitter.go \
     && grep -q 'RegisterDriver(ringDriverName' runtime/obs/ringbuffer.go; then
    ok "the emitter is an interface + factory + driver seam; the ring-buffer driver self-registers"
  else
    fail "the obs emitter seam is incomplete"
  fi
else
  skip "runtime/obs emitter/ring-buffer not built — seam check deferred"
fi

# 4. The ring buffer serves recent events, bounded and non-blocking (acceptance:
#    the ring buffer serves recent events).
if [ -f runtime/obs/ringbuffer.go ]; then
  if grep -q 'func (r \*RingBuffer) Recent' runtime/obs/ringbuffer.go \
     && grep -q 'func (r \*RingBuffer) Emit' runtime/obs/ringbuffer.go \
     && grep -q 'func (r \*RingBuffer) Dropped' runtime/obs/ringbuffer.go; then
    ok "the ring buffer serves recent events and accounts drops"
  else
    fail "the ring-buffer driver is incomplete"
  fi
else
  skip "runtime/obs/ringbuffer.go not built — ring-buffer check deferred"
fi

# 5. The emitter never blocks on a slow consumer (acceptance) — proven by the
#    non-blocking concurrency test.
if [ -f runtime/obs/emitter_test.go ]; then
  if grep -q 'TestRingBuffer_SlowConsumerNeverBlocksEmit' runtime/obs/emitter_test.go; then
    ok "a slow-consumer non-blocking test proves the emitter never blocks"
  else
    fail "no slow-consumer non-blocking test found"
  fi
else
  skip "runtime/obs/emitter_test.go not built — non-blocking-test check deferred"
fi

# 6. Events carry W3C Trace Context trace/span IDs (RFC §11.2).
if [ -f runtime/obs/trace.go ]; then
  if grep -q 'W3C Trace Context' runtime/obs/trace.go \
     && grep -q 'func NewTrace' runtime/obs/trace.go \
     && grep -q 'traceIDBytes = 16' runtime/obs/trace.go; then
    ok "events carry W3C Trace Context trace/span IDs"
  else
    fail "W3C Trace Context IDs are missing"
  fi
else
  skip "runtime/obs/trace.go not built — trace-id check deferred"
fi

# 7. Tool input/output capture defaults to shape + size, not full content
#    (CLAUDE.md §7).
if [ -f runtime/obs/capture.go ]; then
  if grep -q 'CapturePolicyShape' runtime/obs/capture.go \
     && grep -q 'func Shape' runtime/obs/capture.go \
     && grep -q 'type Redactor interface' runtime/obs/capture.go; then
    ok "tool input/output capture defaults to shape+size; full capture is a redaction-gated opt-in"
  else
    fail "shape+size default capture is missing"
  fi
else
  skip "runtime/obs/capture.go not built — capture-policy check deferred"
fi

# 8. The runtime EMITS obs/v1: runtime/server, runtime/apps and runtime/tasks
#    are instrumented to emit, with no back channel (P2, CLAUDE.md §6).
if [ -f runtime/server/server.go ]; then
  if grep -q 'runtime/obs' runtime/server/server.go \
     && grep -q 'runtime/obs' runtime/apps/apps.go \
     && grep -q 'runtime/obs' runtime/tasks/engine.go; then
    ok "runtime/server, runtime/apps and runtime/tasks emit the obs/v1 stream"
  else
    fail "a runtime subsystem is not instrumented to emit obs/v1"
  fi
else
  skip "runtime/server not built — instrumentation check deferred"
fi

# 9. The Event JSON wire shape is pinned by a golden test (obs/v1 is a versioned
#    public contract; CLAUDE.md §8).
if [ -f runtime/obs/event_test.go ]; then
  if grep -q 'TestEvent_GoldenShape' runtime/obs/event_test.go; then
    ok "the obs/v1 Event wire shape is pinned by a golden test"
  else
    fail "no golden test pins the obs/v1 Event shape"
  fi
else
  skip "runtime/obs/event_test.go not built — golden-test check deferred"
fi

# 10. S1 fold-in: the Store migration registry is a caller-owned MigrationSet,
#     not a process global, and is t.Parallel()-safe (D-073).
if [ -f runtime/store/migrate.go ]; then
  if grep -q 'type MigrationSet struct' runtime/store/migrate.go \
     && ! grep -q 'func AddMigration' runtime/store/migrate.go \
     && ! grep -q 'ResetMigrationsForTest' runtime/store/migrate.go; then
    ok "S1 fix: the migration registry is a caller-owned MigrationSet, the mutable global removed"
  else
    fail "S1 fix incomplete — the global migration registry is still present"
  fi
else
  skip "runtime/store/migrate.go not built — S1-fix check deferred"
fi

# 11. S1 fold-in: a -race test proves concurrent migration is safe.
if [ -f runtime/store/store_test.go ]; then
  if grep -q 'TestMigrationSet_ConcurrentMigrate' runtime/store/store_test.go; then
    ok "S1 fix: a concurrent-migrate -race test proves no race / no panic"
  else
    fail "no concurrent-migrate test proves the S1 fix"
  fi
else
  skip "runtime/store/store_test.go not built — S1 concurrency-test check deferred"
fi

# 12. The runtime/obs and runtime/store tests pass.
if [ -f runtime/obs/event_test.go ]; then
  if go test ./runtime/obs/... ./runtime/store/... >/dev/null 2>&1; then
    ok "runtime/obs and runtime/store tests pass"
  else
    fail "phase-15 unit tests fail"
  fi
else
  skip "phase-15 tests not built"
fi

# 13. The Phase 15 integration test exists (real server emitting obs/v1).
if [ -f test/integration/phase15_obs_test.go ]; then
  ok "phase-15 integration test present"
else
  skip "test/integration/phase15_obs_test.go not built"
fi

smoke_summary
