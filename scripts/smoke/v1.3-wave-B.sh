#!/usr/bin/env bash
# Smoke script for v1.3 wave B — npm publish + bridge View-side task progress.
# Plan: docs/plans/v1.3-wave-B-npm-and-bridge-progress.md
# Item 5 (bridge progress) ships in PR A; item 4 (npm publish) in PR B. A
# check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.3-wave-B npm-and-bridge-progress"

# --- Item 5: bridge View-side task-progress channel ------------------------

if [ -f web/bridge/src/protocol.ts ]; then
  if grep -q "ui/notifications/task-progress" web/bridge/src/protocol.ts; then
    ok "bridge protocol declares the task-progress notification"
  else
    skip "bridge protocol has no task-progress notification yet"
  fi
else
  skip "web/bridge/src/protocol.ts not present"
fi

if [ -f web/bridge/src/bridge.ts ] && grep -q "onTaskProgress" web/bridge/src/bridge.ts; then
  ok "BridgeShell exposes onTaskProgress"
else
  skip "BridgeShell has no onTaskProgress helper yet"
fi

if [ -f runtime/obs/payload.go ] && grep -q "Fraction" runtime/obs/payload.go; then
  ok "obs TaskProgressPayload carries a Fraction field"
else
  skip "obs TaskProgressPayload has no Fraction field yet"
fi

if [ -f runtime/tasks/handle.go ] && grep -q "PhaseProgress" runtime/tasks/handle.go; then
  ok "TaskHandle.Progress emits a PhaseProgress obs event"
else
  skip "TaskHandle.Progress does not emit a progress event yet"
fi

if [ -f web/inspector/src/host/host-bridge.ts ] && \
   grep -q "sendTaskProgress" web/inspector/src/host/host-bridge.ts; then
  ok "inspector host-bridge forwards task progress (sendTaskProgress)"
else
  skip "inspector host-bridge has no sendTaskProgress yet"
fi

# --- Item 4: publish @dockyard/bridge + @dockyard/ui to npm ----------------

if [ -f web/bridge/package.json ] && \
   grep -q "publishConfig" web/bridge/package.json && \
   grep -q "publishConfig" web/ui/package.json; then
  ok "both frontend packages set publishConfig (npm-publishable)"
else
  skip "frontend packages do not set publishConfig yet"
fi

if [ -f web/bridge/package.json ] && ! grep -q '"version": "0.1.0"' web/bridge/package.json && \
   [ -f web/ui/package.json ] && ! grep -q '"version": "0.1.0"' web/ui/package.json; then
  ok "frontend package versions track the repo version (off 0.1.0)"
else
  skip "frontend package versions still at 0.1.0"
fi

if [ -f .github/workflows/release.yml ] && grep -q "NPM_TOKEN" .github/workflows/release.yml; then
  ok "release workflow has a gated npm-publish job (NPM_TOKEN)"
else
  skip "release workflow has no npm-publish job yet"
fi

if [ -f internal/scaffold/templates.go ] && \
   grep -q "DOCKYARD_BRIDGE_SPEC\|bridgeSpec" internal/scaffold/templates.go && \
   ! grep -q 'bridgeSpec := "\*"' internal/scaffold/templates.go; then
  ok "scaffold resolves the spec tokens to a published version (no '*')"
else
  skip "scaffold still emits '*' for the spec tokens"
fi

if [ -f skills/scaffold-a-server/SKILL.md ]; then
  if grep -qi "dockyard-path.*required.*UI\|required for UI builds" skills/scaffold-a-server/SKILL.md; then
    skip "scaffold skill still carries the --dockyard-path-for-UI caveat"
  else
    ok "scaffold skill dropped the --dockyard-path-for-UI caveat"
  fi
else
  skip "skills/scaffold-a-server/SKILL.md not present"
fi

smoke_summary
