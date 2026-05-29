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

# --- Secondary: dockyard test guard ----------------------------------------

if grep -rq "resourceUri\|_meta.ui\|MetaUI\|ui.resourceUri" internal/testgate/*.go 2>/dev/null; then
  ok "dockyard test guards that a UI tool emits _meta.ui"
else
  skip "testgate has no _meta.ui guard yet"
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

smoke_summary
