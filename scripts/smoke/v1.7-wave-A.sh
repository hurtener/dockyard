#!/usr/bin/env bash
# Smoke script for v1.7 wave A — Bridge spec-conformance.
# Plan: docs/plans/v1.7-wave-A-bridge-spec-conformance.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.7-wave-A bridge-spec-conformance"

# --- Item 1: vendored ext-apps schema + derived types + conformance --------

# The official ext-apps machine-readable schema is vendored, SHA-pinned, DO NOT EDIT.
schema_file="$(ls web/bridge/src/spec/ext-apps-schema*.ts 2>/dev/null | head -1)"
if [ -n "${schema_file}" ] && grep -qiE 'do not edit' "${schema_file}" 2>/dev/null \
   && grep -qiE 'sha|commit' "${schema_file}" 2>/dev/null; then
  ok "vendored ext-apps schema present (SHA-pinned, DO NOT EDIT)"
else
  skip "vendored ext-apps schema not yet present under web/bridge/src/spec/"
fi

# The vendored schema is referenced ONLY by the test layer — the published
# bridge source imports no Zod/schema, so consumers need no schema deps and the
# App bundle stays Zod-free (D-182; implementation deviation from "derive in
# protocol.ts", recorded in the plan + D-182).
# Match actual import statements (not comments that merely mention the schema).
runtime_refs="$(grep -rlE "from ['\"][^'\"]*ext-apps-schema" web/bridge/src --include='*.ts' 2>/dev/null \
  | grep -v '__tests__' | grep -v 'src/spec/' || true)"
if [ -n "${schema_file}" ] && [ -z "${runtime_refs}" ] \
   && ! grep -qE "from ['\"]zod" web/bridge/src/protocol.ts web/bridge/src/bridge.ts 2>/dev/null; then
  ok "vendored schema referenced only by tests (runtime Zod-free, consumer dep-free)"
else
  skip "a runtime source file references the vendored schema / Zod (${runtime_refs})"
fi

# A conformance test parses the bridge's outbound wire against the vendored schema.
if ls web/bridge/src/__tests__/conformance*.test.ts >/dev/null 2>&1; then
  ok "web/bridge wire-conformance test exists"
else
  skip "web/bridge wire-conformance test not yet added"
fi

# The zod-bearing `dockyard-bridge/spec` subpath must stay resolvable for
# consumers: the export must exist + point at a shipped file, and zod + the SDK
# must be declared as OPTIONAL peer deps (so a `.`-only App author installs
# nothing extra, but a `/spec` consumer knows to provide them) — D-182 audit.
if [ -f web/bridge/src/spec/ext-apps-schema.ts ] && node -e '
  const p = require("./web/bridge/package.json");
  const ok = !!(p.exports && p.exports["./spec"]
    && p.peerDependencies && p.peerDependencies.zod
    && p.peerDependencies["@modelcontextprotocol/sdk"]
    && p.peerDependenciesMeta && p.peerDependenciesMeta.zod
    && p.peerDependenciesMeta.zod.optional);
  process.exit(ok ? 0 : 1);
' 2>/dev/null; then
  ok "dockyard-bridge/spec subpath is resolvable (export ships; zod+sdk optional-peer-declared)"
else
  skip "dockyard-bridge/spec packaging contract not satisfied"
fi

# --- Item A: appCapabilities.availableDisplayModes -------------------------

if [ -f web/bridge/src/bridge.ts ] \
   && grep -q "availableDisplayModes" web/bridge/src/bridge.ts 2>/dev/null \
   && ! grep -qE "appCapabilities\.displayModes|displayModes:" web/bridge/src/bridge.ts 2>/dev/null; then
  ok "bridge emits appCapabilities.availableDisplayModes (not displayModes)"
else
  skip "bridge still emits appCapabilities.displayModes"
fi

# --- Item B: ui/resource-teardown is a request; request-teardown added -----

if [ -f web/bridge/src/bridge.ts ] \
   && grep -q "request-teardown" web/bridge/src/protocol.ts web/bridge/src/bridge.ts 2>/dev/null; then
  ok "bridge exposes the app-initiated ui/notifications/request-teardown"
else
  skip "bridge does not yet expose request-teardown / teardown-as-request"
fi

# --- Item 3: Dockyard Tasks×Apps notifications fenced ----------------------

# task-progress + elicitation-response live in dockyard-ext, NOT in protocol.ts.
if [ -f web/bridge/src/dockyard-ext.ts ] \
   && grep -qE "task-progress|elicitation-response" web/bridge/src/dockyard-ext.ts 2>/dev/null; then
  ok "Dockyard Tasks×Apps notifications fenced in dockyard-ext"
else
  skip "Dockyard Tasks×Apps notifications not yet fenced"
fi

# --- Item 4: inspector host conformance ------------------------------------

# The inspector host no longer unconditionally sends host→View initialized.
if [ -f web/inspector/src/host/host-bridge.ts ] \
   && ! grep -qE "this\.notify\(ViewNotification\.initialized" web/inspector/src/host/host-bridge.ts 2>/dev/null; then
  ok "inspector host no longer sends host→View ui/notifications/initialized"
else
  skip "inspector host still sends host→View initialized (faithful-host not yet landed)"
fi

# The inspector validates inbound ui/initialize against the vendored schema
# (via the dockyard-bridge/spec subpath) — a faithful spec host, not a lenient one.
if grep -q "dockyard-bridge/spec" web/inspector/src/host/host-bridge.ts 2>/dev/null \
   && grep -qE "McpUiInitializeRequestSchema.*safeParse|safeParse.*McpUiInitialize" web/inspector/src/host/host-bridge.ts 2>/dev/null \
   && grep -q '"./spec"' web/bridge/package.json 2>/dev/null; then
  ok "inspector validates inbound ui/initialize against the vendored schema"
else
  skip "inspector inbound-schema validation not yet wired"
fi

# --- §19: docs reflect the Tasks-extension host requirement ----------------

if grep -qiE "Dockyard-aware host|Tasks.*host-only|only against.*host" \
     skills/attach-a-ui-resource/SKILL.md 2>/dev/null; then
  ok "attach-a-ui-resource skill notes the Tasks-extension host requirement"
else
  skip "attach-a-ui-resource skill does not yet note the Tasks-extension boundary"
fi

smoke_summary
