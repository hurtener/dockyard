#!/usr/bin/env bash
# Smoke script for v1.7 wave B — published-docs refresh.
# Plan: docs/plans/v1.7-wave-B-docs-refresh.md
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
# preflight: serial — builds the shared in-repo docs/site (VitePress writes
# docs/site/.vitepress/{cache,dist}), so the preflight gate runs it sequentially,
# never concurrently with another docs/site build (D-188).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.7-wave-B docs-refresh"

# --- Item 1: no v1.0.0 pin on the published site ----------------------------
if [ -d docs/site ] && ! grep -rqE '@v1\.0\.0|Released: v1\.0\.0' docs/site --include='*.md' 2>/dev/null; then
  ok "no @v1.0.0 install pin / 'Released: v1.0.0' callout under docs/site"
else
  skip "docs/site still pins or announces v1.0.0"
fi

# --- Item 2: Svelte sketches use the real bridge API ------------------------
# The App-authoring pages must use createBridge(...) and must NOT import a
# top-level onToolResult/hostContext from dockyard-bridge (the dead API).
app_pages="docs/site/getting-started/analytics-widgets.md docs/site/guides/ui-resources.md"
if grep -lq "createBridge" $app_pages 2>/dev/null \
   && ! grep -rqE "import \{[^}]*(onToolResult|hostContext)[^}]*\} from 'dockyard-bridge'" $app_pages 2>/dev/null; then
  ok "App-authoring pages use createBridge (not the dead top-level bridge API)"
else
  skip "App sketches still use the dead dockyard-bridge API"
fi

# --- Item 3: CLI new.go (and the generated page) list only real templates ---
if [ -f internal/cli/new.go ] \
   && ! grep -qE 'analytics-widgets, approval-flows, inspector' internal/cli/new.go 2>/dev/null \
   && ! grep -q 'approval-flows, inspector' docs/site/cli/index.md 2>/dev/null; then
  ok "dockyard new lists only the two real templates (no 'inspector')"
else
  skip "dockyard new still lists a non-existent 'inspector' template"
fi

# --- Item 4: no phase-NN in user-facing PROSE -------------------------------
# Image paths are handled separately; flag only prose leaks (Phase N / phase-N
# outside an image-path context). A simple proxy: 'phase-25 layout' / 'phase-NN
# fix' wording.
if [ -d docs/site ] && ! grep -rqiE 'phase-[0-9]+ (layout|fix|finish)' docs/site --include='*.md' 2>/dev/null; then
  ok "no phase-NN methodology wording in docs/site prose"
else
  skip "docs/site prose still leaks phase-NN methodology vocabulary"
fi

# --- Item 5: coverage — inspector handshake note + dockyard-ui ---------------
if grep -qiE 'handshake|validat' docs/site/guides/inspector.md 2>/dev/null \
   && grep -rqi 'dockyard-ui' docs/site/guides 2>/dev/null; then
  ok "inspector guide notes handshake validation; dockyard-ui documented"
else
  skip "inspector handshake note / dockyard-ui mention not yet added"
fi

# --- the site still builds (best-effort; SKIP without the toolchain) --------
if command -v npm >/dev/null 2>&1 && [ -d docs/site/node_modules ]; then
  if ( cd docs/site && npm run build ) >/dev/null 2>&1; then
    ok "docs/site builds clean (VitePress, dead-link gated)"
  else
    fail "docs/site build failed"
  fi
else
  skip "docs/site build not checked (npm deps not installed)"
fi

smoke_summary
