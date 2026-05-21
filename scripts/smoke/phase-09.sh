#!/usr/bin/env bash
# Smoke script for Phase 09 — MCP Apps extension, server-side: ui:// resource
# registration, _meta.ui on tools and resource-read responses, the extensions
# capability, and plain-MCP graceful degradation. One assertion per acceptance
# criterion (docs/plans/phase-09-apps-extension.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-09 apps-extension"

# 1. The runtime/apps package exists.
if [ -f runtime/apps/apps.go ]; then
  ok "runtime/apps package exists"
else
  skip "runtime/apps/apps.go not built"
fi

# 2. runtime/apps builds CGo-free (no-CGo runtime guarantee, AGENTS.md §13).
if [ -f runtime/apps/apps.go ]; then
  if CGO_ENABLED=0 go build ./runtime/apps/... >/dev/null 2>&1; then
    ok "runtime/apps builds CGo-free"
  else
    fail "runtime/apps does not build with CGO_ENABLED=0"
  fi
else
  skip "runtime/apps not built — build check deferred"
fi

# 3. All MCP extension wire shapes go through internal/protocolcodec (P3):
#    runtime/apps imports protocolcodec and constructs no raw wire JSON keys.
if [ -f runtime/apps/apps.go ]; then
  if grep -rq 'internal/protocolcodec' runtime/apps/*.go \
     && ! grep -rEq '"(resourceUri|connectDomains|resourceDomains|mimeTypes)"' \
          runtime/apps/apps.go runtime/apps/csp.go runtime/apps/capability.go; then
    ok "runtime/apps routes wire shapes through protocolcodec — no raw keys (P3)"
  else
    fail "runtime/apps constructs a raw extension wire shape (P3 violation)"
  fi
else
  skip "runtime/apps not built — P3 check deferred"
fi

# 4. The deprecated flat _meta["ui/resourceUri"] form is never emitted: no
#    non-comment line of the runtime/apps source uses the flat key (a comment
#    or a test may legitimately mention it; protocolcodec enforces the policy).
if [ -f runtime/apps/apps.go ]; then
  if ! grep -h 'ui/resourceUri' \
       runtime/apps/apps.go runtime/apps/csp.go \
       runtime/apps/capability.go runtime/apps/doc.go 2>/dev/null \
       | grep -qvE '^[[:space:]]*//'; then
    ok "runtime/apps never emits the deprecated flat ui/resourceUri form"
  else
    fail "runtime/apps emits the deprecated flat ui/resourceUri form"
  fi
else
  skip "runtime/apps not built — flat-form check deferred"
fi

# 5. The Apps MIME type and extension id are defined.
if [ -f runtime/apps/capability.go ]; then
  if grep -q 'MIMETypeApp' runtime/apps/capability.go \
     && grep -q 'ExtensionID' runtime/apps/capability.go; then
    ok "Apps MIME type + extension id surface defined"
  else
    fail "runtime/apps/capability.go missing MIMETypeApp / ExtensionID"
  fi
else
  skip "runtime/apps/capability.go not built"
fi

# 6. runtime/server exposes the additive _meta + extension-capability seams the
#    Apps layer composes (ToolDef.Meta, ResourceContent.Meta, Options.Extensions).
if [ -f runtime/server/server.go ]; then
  if grep -q 'Extensions \[\]ExtensionCapability' runtime/server/server.go \
     && grep -q 'Meta map\[string\]any' runtime/server/tool.go \
     && grep -q 'Meta map\[string\]any' runtime/server/resource.go; then
    ok "runtime/server exposes the _meta + extension-capability seams"
  else
    fail "runtime/server missing the Phase 09 additive seams"
  fi
else
  skip "runtime/server/server.go not built — seam check deferred"
fi

# 7. The runtime/apps and runtime/server tests pass.
if [ -f runtime/apps/apps_test.go ]; then
  if go test ./runtime/apps/... ./runtime/server/... >/dev/null 2>&1; then
    ok "runtime/apps and runtime/server tests pass"
  else
    fail "runtime/apps / runtime/server tests fail"
  fi
else
  skip "phase-09 tests not built"
fi

# 8. The Phase 09 integration test exists (Apps extension over a real transport).
if [ -f test/integration/phase09_apps_extension_test.go ]; then
  ok "phase-09 integration test present"
else
  skip "test/integration/phase09_apps_extension_test.go not built"
fi

smoke_summary
