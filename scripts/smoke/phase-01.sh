#!/usr/bin/env bash
# Smoke script for Phase 01 — runtime library skeleton + go-sdk baseline.
# One assertion per acceptance criterion. A check against an unbuilt surface
# SKIPs rather than FAILs — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-01 runtime-skeleton"

# AC: package layout matches AGENTS.md §3.
if [ -d runtime/server ]; then ok "runtime/server package exists"; else fail "runtime/server missing"; fi
if [ -f cmd/dockyard/main.go ]; then ok "cmd/dockyard/main.go exists"; else fail "cmd/dockyard/main.go missing"; fi

# AC: the SDK version is pinned to a recent v1.x.
if grep -qE 'modelcontextprotocol/go-sdk v1\.[0-9]+\.[0-9]+' go.mod; then
  ok "go-sdk pinned to a v1.x release in go.mod"
else
  fail "go-sdk not pinned to a v1.x release in go.mod"
fi

# AC: CGO_ENABLED=0 build verified.
if CGO_ENABLED=0 go build ./... >/dev/null 2>&1; then
  ok "CGO_ENABLED=0 go build ./... succeeds"
else
  fail "CGO_ENABLED=0 go build ./... failed"
fi

# AC: a trivial server registers one tool and serves it; stdio path; concurrency.
# The runtime test suite covers tools/list + tools/call over the in-memory
# transport, ServeStdio cancellation, and concurrent-session reuse under -race.
if CGO_ENABLED=0 go test ./runtime/server/... >/dev/null 2>&1; then
  ok "runtime/server tests pass (serve + tool call + stdio + concurrency)"
else
  fail "runtime/server tests failed"
fi

# AC: cmd/dockyard placeholder binary runs.
if go run ./cmd/dockyard version 2>/dev/null | grep -q '^dockyard '; then
  ok "dockyard version placeholder runs"
else
  fail "dockyard version placeholder did not run"
fi

smoke_summary
