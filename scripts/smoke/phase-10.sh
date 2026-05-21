#!/usr/bin/env bash
# Smoke script for Phase 10 — UI auto-discovery + embed pipeline: convention
# discovery of web/src/apps/*.svelte into ui:// resources, the discovered
# wiring written into dockyard.app.yaml, and the //go:embed all:dist bundle
# backing the ui:// MCP resource handler. One assertion per acceptance
# criterion (docs/plans/phase-10-ui-discovery.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-10 ui-discovery"

# 1. The discovery + embed files exist in runtime/apps (new files, not a
#    rewrite of apps.go — Phase 12 edits runtime/apps in parallel).
if [ -f runtime/apps/discovery.go ] && [ -f runtime/apps/embed.go ]; then
  ok "runtime/apps discovery.go + embed.go exist"
else
  skip "runtime/apps Phase 10 files not built"
fi

# 2. runtime/apps still builds CGo-free (no-CGo runtime guarantee, AGENTS.md §13).
if [ -f runtime/apps/discovery.go ]; then
  if CGO_ENABLED=0 go build ./runtime/apps/... >/dev/null 2>&1; then
    ok "runtime/apps builds CGo-free with the Phase 10 additions"
  else
    fail "runtime/apps does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/apps Phase 10 files not built — build check deferred"
fi

# 3. Convention discovery surface: Discover + RegisterDiscovered are exported.
if [ -f runtime/apps/discovery.go ]; then
  if grep -q 'func Discover(' runtime/apps/discovery.go \
     && grep -q 'func RegisterDiscovered(' runtime/apps/discovery.go \
     && grep -q 'ConventionDir' runtime/apps/discovery.go; then
    ok "Discover / RegisterDiscovered / ConventionDir surface present"
  else
    fail "runtime/apps/discovery.go missing the discovery surface"
  fi
else
  skip "runtime/apps/discovery.go not built — discovery-surface check deferred"
fi

# 4. The embed pipeline uses //go:embed and exposes the Bundle + clean-failure
#    seam (RFC §14 — build fails cleanly if dist/ is absent).
if [ -f runtime/apps/bundlefs.go ]; then
  if grep -q '//go:embed' runtime/apps/bundlefs.go \
     && grep -q 'ErrEmptyBundle' runtime/apps/embed.go \
     && grep -q 'func (b Bundle) Validate(' runtime/apps/embed.go; then
    ok "embed pipeline uses //go:embed and exposes Bundle.Validate + ErrEmptyBundle"
  else
    fail "runtime/apps embed pipeline incomplete"
  fi
else
  skip "runtime/apps/bundlefs.go not built — embed-pipeline check deferred"
fi

# 5. The discovered wiring is written into the manifest: internal/manifest
#    exposes WriteDiscoveredApps (RFC §7.6 — wiring stays inspectable).
if [ -f internal/manifest/wiring.go ]; then
  if grep -q 'func WriteDiscoveredApps(' internal/manifest/wiring.go; then
    ok "internal/manifest exposes WriteDiscoveredApps"
  else
    fail "internal/manifest/wiring.go missing WriteDiscoveredApps"
  fi
else
  skip "internal/manifest/wiring.go not built — manifest-wiring check deferred"
fi

# 6. The Phase 10 unit tests pass (discovery, embed, manifest wiring).
if [ -f runtime/apps/discovery_test.go ]; then
  if go test ./runtime/apps/... ./internal/manifest/... >/dev/null 2>&1; then
    ok "runtime/apps + internal/manifest tests pass"
  else
    fail "Phase 10 unit tests fail"
  fi
else
  skip "Phase 10 unit tests not built"
fi

# 7. The Phase 10 integration test exists (discovery + embed over a real MCP
#    transport, and the discovered wiring round-tripped through the manifest).
if [ -f test/integration/phase10_ui_discovery_test.go ]; then
  if go test ./test/integration/... -run Phase10 >/dev/null 2>&1; then
    ok "phase-10 integration test passes"
  else
    fail "phase-10 integration test fails"
  fi
else
  skip "test/integration/phase10_ui_discovery_test.go not built"
fi

smoke_summary
