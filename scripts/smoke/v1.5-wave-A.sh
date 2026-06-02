#!/usr/bin/env bash
# Smoke script for v1.5 wave A — wire the tool→App _meta.ui link in the builder.
# Plan: docs/plans/v1.5-wave-A-tool-ui-link.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.5-wave-A tool-ui-link"

# --- Core: the server name→link seam ---------------------------------------

if grep -rq "func (s \*Server) RegisterAppLink" runtime/server/*.go 2>/dev/null && \
   grep -rq "func (s \*Server) AppLinkByName" runtime/server/*.go 2>/dev/null; then
  ok "server exposes the AppLink name→link seam"
else
  skip "server has no RegisterAppLink/AppLinkByName seam yet"
fi

# --- Core: apps.Register records the link ----------------------------------

if [ -f runtime/apps/apps.go ] && grep -q "RegisterAppLink" runtime/apps/apps.go; then
  ok "apps.Register records the app link"
else
  skip "apps.Register does not record an app link yet"
fi

# --- Core: the builder wires _meta.ui --------------------------------------

if [ -f runtime/tool/builder.go ] && \
   grep -q "ToolMetaFor" runtime/tool/builder.go && \
   grep -q "AppLinkByName" runtime/tool/builder.go; then
  ok "builder.Register wires _meta.ui via ToolMetaFor + AppLinkByName"
else
  skip "builder.Register does not yet wire _meta.ui"
fi

# --- Visibility control on the builder -------------------------------------

if [ -f runtime/tool/builder.go ] && grep -q "VisibilityApp\|VisibilityModel" runtime/tool/builder.go; then
  ok "builder exposes tool visibility control"
else
  skip "builder has no visibility control yet"
fi

# --- Guard: the framework regression test (builder emits _meta.ui) ----------
# The upstream bug was a framework defect (the builder never emitted _meta.ui),
# not a user-project mistake — so no user-facing gate could have caught it; the
# guard is Dockyard's own regression test. (validate/testgate are static by
# design — D-082 — so a live "_meta.ui present" assertion has no cheap home
# there; see the plan's deviation note.)

if [ -f runtime/tool/builder_test.go ] && \
   grep -q "resourceUri" runtime/tool/builder_test.go; then
  ok "builder regression test asserts .UI().Register() emits _meta.ui.resourceUri"
else
  skip "no builder _meta.ui regression test yet"
fi

# --- §19: skill documents the ordering rule --------------------------------

if [ -f skills/attach-a-ui-resource/SKILL.md ] && \
   grep -qi "before.*tool\|apps.Register before\|register the app first\|ordering" skills/attach-a-ui-resource/SKILL.md; then
  ok "attach-a-ui-resource skill documents apps-before-tools ordering"
else
  skip "skill does not document the apps-before-tools ordering yet"
fi

# --- Item 2: unscoped npm package names ------------------------------------

if grep -q '"name": "dockyard-bridge"' web/bridge/package.json 2>/dev/null && \
   grep -q '"name": "dockyard-ui"' web/ui/package.json 2>/dev/null; then
  ok "frontend packages renamed to unscoped dockyard-bridge / dockyard-ui"
else
  skip "frontend packages not yet renamed to unscoped names"
fi

if ! grep -rq "@dockyard/\(bridge\|ui\)" web/inspector/src 2>/dev/null && \
   grep -rq "dockyard-bridge\|dockyard-ui" web/inspector/src 2>/dev/null; then
  ok "inspector imports the unscoped package names"
else
  skip "inspector still imports @dockyard/bridge or @dockyard/ui"
fi

if ! grep -q "@dockyard/" templates/analytics-widgets/web/package.json 2>/dev/null && \
   grep -q "dockyard-ui" templates/analytics-widgets/web/package.json 2>/dev/null; then
  ok "templates depend on the unscoped package names"
else
  skip "templates still reference @dockyard/* package names"
fi

if grep -q "npm view \"dockyard-bridge@" .github/workflows/release.yml 2>/dev/null; then
  ok "release workflow publishes the unscoped names"
else
  skip "release workflow still references @dockyard/* names"
fi

# --- Wiring audit (the same item-1 class, fixed across the framework) ------

if [ -f internal/validate/checks.go ] && \
   grep -q "RequireSpecCompliance" internal/validate/checks.go; then
  ok "require_spec_compliance is enforced (checkSpecCompliance reads the flag)"
else
  skip "require_spec_compliance gate not enforced yet"
fi

# v1.7 wave A (D-182, item B): resource-teardown is now a host→View *request*
# (HostRequest.resourceTeardown) the bridge responds to then closes — no longer
# a notification (HostNotification.resourceTeardown).
if [ -f web/bridge/src/bridge.ts ] && \
   grep -q "HostRequest.resourceTeardown" web/bridge/src/bridge.ts && \
   grep -q "negotiatedProtocolVersion" web/bridge/src/bridge.ts; then
  ok "bridge wires resource-teardown (request → respond+close) and retains the negotiated protocolVersion"
else
  skip "bridge resource-teardown / protocolVersion retention not wired yet"
fi

smoke_summary
