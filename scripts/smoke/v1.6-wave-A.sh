#!/usr/bin/env bash
# Smoke script for v1.6 wave A — MCP Apps spec-alignment.
# Plan: docs/plans/v1.6-wave-A-apps-spec-alignment.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.6-wave-A apps-spec-alignment"

# --- Item 1: _meta.ui.domain is verbatim; auto-derivation retired ----------

# The synthesising Claude derivation is gone from the active emission path.
# (When it lands, hostprofile_claude.go no longer computes the hash subdomain;
# DerivedDomain returns the label verbatim.)
if [ -f runtime/apps/domain.go ] && \
   ! grep -rq "claudemcpcontent.com" runtime/apps/domain.go runtime/apps/hostprofile_claude.go 2>/dev/null; then
  ok "runtime/apps no longer synthesises claudemcpcontent.com in the domain path"
else
  skip "runtime/apps still synthesises the Claude signed origin (verbatim domain not yet landed)"
fi

# apps.App.Domain godoc reflects the host-supplied verbatim model.
# (Phrases the current "Dockyard does not carry it verbatim / auto-derives"
# godoc does NOT contain — so this stays SKIP until the rewrite lands.)
if [ -f runtime/apps/apps.go ] && \
   grep -qi "host-supplied\|host-minted\|remote-connector" runtime/apps/apps.go; then
  ok "apps.App documents the host-supplied verbatim domain model"
else
  skip "apps.App still documents server-side domain derivation"
fi

# Runtime guard: a stdio-only server with an App.Domain warns at startup.
if grep -rq "local connector\|remote-connector\|remote connector" runtime/server/*.go 2>/dev/null && \
   grep -rq "Domain" runtime/server/*.go 2>/dev/null; then
  ok "runtime/server guards Domain on a stdio-only (local connector) server"
else
  skip "runtime/server has no stdio-domain startup guard yet"
fi

# --- Item 2: opt-in flat ui/resourceUri key --------------------------------

# The encoder gains an opt-in flat-key path; the default stays nested-only,
# still proven by the existing "never emit the flat key" test. The check keys
# on the concrete opt-in identifier (locked in D-177) — not on the existing
# "deprecated flat ui/resourceUri" comments, which would false-positive.
if grep -rqi "EmitLegacyToolUIMeta\|emit_legacy_ui_meta" \
     runtime/server/*.go runtime/apps/*.go internal/protocolcodec/*.go internal/manifest/*.go 2>/dev/null; then
  ok "an opt-in path for the flat _meta[ui/resourceUri] key exists"
else
  skip "no opt-in flat-key emission path yet (default stays nested-only)"
fi

# The default-mode invariant survives: the apps test still forbids the flat key.
if [ -f runtime/apps/apps_test.go ] && \
   grep -q 'ui/resourceUri' runtime/apps/apps_test.go; then
  ok "default-mode test still asserts the flat ui/resourceUri key is absent"
else
  skip "default-mode no-flat-key assertion not present"
fi

# --- Item 3: html-style ui:// URI convention -------------------------------

if grep -rq "ui://.*\/index\.html" templates/*/dockyard.app.yaml templates/*/main.go.tmpl 2>/dev/null; then
  ok "templates scaffold the html-style ui://<server>/<app>/index.html URI"
else
  skip "templates do not yet use the html-style ui:// URI convention"
fi

# --- §19 + drift: docs/skill describe the new domain model -----------------

if [ -f skills/attach-a-ui-resource/SKILL.md ] && \
   grep -qi "host-supplied\|host-documented\|verbatim\|remote-connector\|remote connector" skills/attach-a-ui-resource/SKILL.md; then
  ok "attach-a-ui-resource skill documents the host-supplied domain model"
else
  skip "skill does not yet document the host-supplied domain model"
fi

# --- Decisions + RFC amendment + backlog spike -----------------------------

if grep -q "D-176" docs/decisions.md 2>/dev/null; then
  ok "docs/decisions.md records D-176 (domain spec-alignment)"
else
  skip "D-176 not recorded yet"
fi

if grep -qi "Claude Desktop\|claude-desktop render\|render spike" docs/V2-BACKLOG.md 2>/dev/null; then
  ok "V2-BACKLOG tracks the OPEN Claude-Desktop render spike"
else
  skip "V2-BACKLOG does not yet track the Claude-Desktop render spike"
fi

smoke_summary
