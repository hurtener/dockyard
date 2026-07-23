#!/usr/bin/env bash
# Smoke script for v1.12 wave A — server branding (SEP-973 icons) in serverInfo.
# Plan: docs/plans/v1.12-wave-A-server-icons.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.12-wave-A server-icons"

# --- Item 1: the runtime branding API exists ---------------------------------
if grep -q "Icons \[\]Icon" runtime/server/server.go 2>/dev/null &&
   grep -q "type Icon struct" runtime/server/icon.go 2>/dev/null &&
   grep -q "WebsiteURL" runtime/server/server.go 2>/dev/null; then
  ok "server.Info branding fields + server.Icon type exist"
else
  fail "server.Info branding fields or server.Icon type missing"
fi

# --- Item 2: branding is emitted on both lifecycles --------------------------
if grep -q "Icons:.*sdkIcons" runtime/server/server.go 2>/dev/null &&
   grep -q "info.Icons" internal/protocolcodec/response.go 2>/dev/null; then
  ok "branding wired to legacy Implementation and modern EncodeServerInfo"
else
  fail "branding not wired into one of the lifecycle serverInfo paths"
fi

# --- Item 3: the manifest carries branding declaratively ---------------------
if grep -q 'Icons \[\]Icon `yaml:"icons"`' internal/manifest/manifest.go 2>/dev/null &&
   grep -q "website_url" internal/manifest/manifest.go 2>/dev/null &&
   grep -q "func (m \*Manifest) validateBranding" internal/manifest/validate.go 2>/dev/null; then
  ok "manifest icons/website_url fields + validateBranding exist"
else
  fail "manifest branding fields or validation missing"
fi

# --- Item 4: the example manifest demonstrates branding ----------------------
if grep -q "^icons:" examples/customer-health/dockyard.app.yaml 2>/dev/null; then
  ok "example manifest carries a branding block"
else
  fail "example manifest missing the icons block"
fi

# --- Item 5: the invariants are asserted by tests ----------------------------
if grep -rqs "TestLegacyInitializeAdvertisesIcons" runtime/server 2>/dev/null &&
   grep -rqs "TestServerInfoOmitsBrandingWhenUnset" runtime/server 2>/dev/null &&
   grep -rqs "TestEncodeServerInfoBranding" internal/protocolcodec 2>/dev/null &&
   grep -rqs "TestValidate_Branding" internal/manifest 2>/dev/null; then
  ok "serverInfo emission, omitempty, codec, and manifest branding tests exist"
else
  fail "server-branding invariant tests missing"
fi

# --- Item 6: the decision and RFC note exist ---------------------------------
if grep -qs "^## D-203" docs/decisions.md 2>/dev/null &&
   grep -q "Server identity & branding (serverInfo)" RFC-001-Dockyard.md 2>/dev/null; then
  ok "D-203 + RFC §5.1 branding note recorded"
else
  fail "D-203 or RFC branding note missing"
fi

# --- Item 7: the published guide documents branding --------------------------
if [ -f docs/site/guides/server-branding.md ] &&
   grep -q "client-dependent" docs/site/guides/server-branding.md 2>/dev/null; then
  ok "server-branding guide documents the API and the client-dependent caveat"
else
  skip "server-branding guide not present yet"
fi

smoke_summary
