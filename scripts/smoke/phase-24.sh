#!/usr/bin/env bash
# Smoke script for Phase 24 — template system + analytics-widgets.
# One assertion per acceptance criterion (docs/plans/phase-24-analytics-widgets.md):
#   - `dockyard new --template analytics-widgets` produces a project
#   - the produced project builds CGo-free
#   - the produced project's contract tests pass
#   - the materialised manifest declares 3 tools + 1 inline-only app
#   - the App's three widget contracts ship generated artifacts (the
#     analytics-widgets template's contracts.go is in-tree as a .tmpl;
#     this assertion checks the materialised tree's contracts package
#     exists)
#   - the shared Sparkline lives in web/ui/ and is exported
#   - CONVENTIONS.md §3 lists Sparkline
#   - the template-discovery seam exists (interface + Registry + builtin
#     init)
#   - `analytical-card` does not appear in any source file (the rename is
#     complete except the historical research-brief mention in
#     docs/research/04-mcp-use-dx-teardown.md)
#   - the no-template `dockyard new` path still works (Phase 17 unchanged)
# A check against an unbuilt surface skip()s, never fail()s — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-24 template system + analytics-widgets"

REPO_ROOT="$(pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- the dockyard binary -----------------------------------------------------
if [ ! -f cmd/dockyard/main.go ]; then
  skip "cmd/dockyard/main.go absent — CLI phase not landed"
  smoke_summary
  exit $?
fi

if CGO_ENABLED=0 go build -o "$WORK/dockyard" ./cmd/dockyard 2>"$WORK/build.log"; then
  ok "dockyard binary builds CGo-free (with the analytics-widgets template embedded)"
else
  fail "dockyard binary build failed"; sed 's/^/    /' "$WORK/build.log"
  smoke_summary; exit $?
fi
DOCKYARD="$WORK/dockyard"

# --- `dockyard new --template analytics-widgets` ----------------------------
PROJ="$WORK/proj"
mkdir -p "$PROJ"
if "$DOCKYARD" new aw-smoke --template analytics-widgets --dir "$PROJ" --dockyard-path "$REPO_ROOT" \
     >"$WORK/new.log" 2>&1; then
  ok "dockyard new --template analytics-widgets produces a project"
else
  fail "dockyard new --template analytics-widgets failed"; sed 's/^/    /' "$WORK/new.log"
  smoke_summary; exit $?
fi
SRV="$PROJ/aw-smoke"

# --- the materialised project layout ----------------------------------------
if [ -f "$SRV/dockyard.app.yaml" ] && [ -f "$SRV/main.go" ] \
   && [ -f "$SRV/internal/contracts/contracts.go" ] \
   && [ -f "$SRV/internal/handlers/handlers.go" ] \
   && [ -f "$SRV/web/src/App.svelte" ] \
   && [ -f "$SRV/web/src/widgets/ChartFrame.svelte" ] \
   && [ -f "$SRV/README.md" ]; then
  ok "materialised project includes the manifest, main, contracts, handlers, App and ChartFrame"
else
  fail "materialised project is missing one of the expected files"
fi

# --- the materialised manifest is what the template declares ----------------
if grep -q "name: create_chart" "$SRV/dockyard.app.yaml" \
   && grep -q "name: create_table" "$SRV/dockyard.app.yaml" \
   && grep -q "name: create_metric_card" "$SRV/dockyard.app.yaml" \
   && grep -q "display_modes: \[inline\]" "$SRV/dockyard.app.yaml"; then
  ok "manifest declares 3 tools + 1 inline-only app"
else
  fail "manifest does not declare the expected tools + display_modes shape"
fi

# --- every required fixture lives under the materialised tree ---------------
fixture_count=$(find "$SRV/fixtures" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
if [ "$fixture_count" = "18" ]; then
  ok "materialised project ships 18 fixtures (6 per tool)"
else
  fail "expected 18 fixtures (6 per tool), got $fixture_count"
fi

# --- the scaffolded project builds and tests pass ---------------------------
if ( cd "$SRV" && CGO_ENABLED=0 go mod tidy && CGO_ENABLED=0 go build ./... ) \
     >"$WORK/proj-build.log" 2>&1; then
  ok "the materialised project builds CGo-free"
else
  fail "the materialised project does not build"; sed 's/^/    /' "$WORK/proj-build.log"
fi

if ( cd "$SRV" && CGO_ENABLED=0 go test ./... ) >"$WORK/proj-test.log" 2>&1; then
  ok "the materialised project's contract tests pass"
else
  fail "the materialised project's contract tests failed"; sed 's/^/    /' "$WORK/proj-test.log"
fi

# --- Sparkline lives in web/ui and is documented in CONVENTIONS.md §3 -------
if [ -f web/ui/src/Sparkline.svelte ] \
   && grep -q "Sparkline" web/ui/src/index.ts; then
  ok "Sparkline lives in web/ui/ and is exported"
else
  fail "Sparkline is missing from web/ui/ or its barrel export"
fi
if grep -q "Sparkline" docs/design/CONVENTIONS.md; then
  ok "docs/design/CONVENTIONS.md §3 lists Sparkline"
else
  fail "Sparkline is not documented in docs/design/CONVENTIONS.md §3"
fi
# The template-local ChartFrame must NOT have leaked into web/ui (decision D-127).
if [ ! -f web/ui/src/ChartFrame.svelte ]; then
  ok "ChartFrame stays template-local (decision D-127)"
else
  fail "ChartFrame leaked into web/ui — wrappers around third-party libs are template-local"
fi
# And a Sparkline must NOT have been duplicated under the template.
if [ ! -f templates/analytics-widgets/web/src/widgets/Sparkline.svelte ] \
   && [ ! -f templates/analytics-widgets/web/src/Sparkline.svelte ]; then
  ok "Sparkline is not duplicated under the template (composed from web/ui)"
else
  fail "Sparkline is duplicated under the template — must be composed from web/ui"
fi

# --- the template-discovery seam exists -------------------------------------
if [ -f internal/scaffold/template.go ] \
   && grep -q "type Template interface" internal/scaffold/template.go \
   && grep -q "RegisterTemplate" internal/scaffold/template.go \
   && grep -q "ErrUnknownTemplate" internal/scaffold/template.go; then
  ok "internal/scaffold ships the template-discovery seam (interface + RegisterTemplate + ErrUnknownTemplate)"
else
  fail "the template-discovery seam is missing"
fi

# --- the rename is complete (analytical-card → analytics-widgets) -----------
# Allowed historical references: brief 04 keeps the original two mentions,
# the decisions log + glossary + plan + RFC may mention the old name in the
# context of the rename. Source code, CLI flags, manifests, and scripts must
# carry only the new name.
# Search for the old name across source-bearing dirs. Exclude this very
# script — it has to spell the old name to assert its absence — and exclude
# the research-brief 04 historical mention (the only documented carve-out,
# per the rename plan; the brief is research, not design source-of-truth).
forbidden=$(grep -rln "analytical""-card" \
  --include='*.go' --include='*.sh' --include='*.yaml' --include='*.yml' \
  --include='*.svelte' --include='*.ts' --include='*.js' \
  internal/ cmd/ scripts/ templates/ web/ 2>/dev/null \
  | grep -v 'scripts/smoke/phase-24.sh' \
  || true)
if [ -z "$forbidden" ]; then
  ok "no source file (internal/, cmd/, scripts/, templates/, web/) references the old name"
else
  fail "source files still reference the old template name:"
  echo "$forbidden" | sed 's/^/    /'
fi

# --- the no-template path still works (Phase 17 unchanged) ------------------
BLANK="$WORK/blank-proj"
mkdir -p "$BLANK"
if "$DOCKYARD" new no-template-server --dir "$BLANK" --dockyard-path "$REPO_ROOT" \
     >"$WORK/blank.log" 2>&1; then
  ok "the no-template 'dockyard new' path still works (Phase 17 first-class)"
else
  fail "the no-template 'dockyard new' path is broken"; sed 's/^/    /' "$WORK/blank.log"
fi

# --- the --template flag is wired into the help -----------------------------
"$DOCKYARD" new --help >"$WORK/help.txt" 2>&1 || true
if grep -q '\-\-template' "$WORK/help.txt"; then
  ok "dockyard new --help lists --template"
else
  fail "dockyard new --help does not list the --template flag"
fi

# --- the integration test runs ---------------------------------------------
if CGO_ENABLED=1 go test -race -count=1 -run 'TestPhase24_' \
     ./test/integration/ >"$WORK/itest.log" 2>&1; then
  ok "Phase 24 integration test passes (real materialise → build → tools/call cycle)"
else
  fail "Phase 24 integration test failed"; sed 's/^/    /' "$WORK/itest.log"
fi

# --- the on-disk fixture loader (D-130) -------------------------------------
# A FixturesFromDir source exists, a /api/fixtures handler is registered, and
# the inspect CLI wires both. The loader's unit tests run as part of
# `go test ./internal/inspector/...`; this smoke just asserts the surface.
if grep -q "FixturesFromDir" internal/inspector/fixtures.go \
   && grep -q "FixtureSource" internal/inspector/inspector.go \
   && grep -q "/api/fixtures" internal/inspector/assets.go \
   && grep -q "FixturesFromDir" internal/cli/inspect.go; then
  ok "the inspector loads on-disk project fixtures via /api/fixtures (D-130)"
else
  fail "the on-disk fixture loader surface is missing one of its pieces"
fi

# --- the bridge default peer posts with a wildcard targetOrigin (D-129) ----
# A regression test in web/bridge/src/__tests__/bridge.test.ts exists; the
# bridge.ts itself ships defaultParentSink. This smoke asserts both.
if grep -q "defaultParentSink" web/bridge/src/bridge.ts \
   && grep -q "wildcard targetOrigin" web/bridge/src/__tests__/bridge.test.ts; then
  ok "the bridge default peer posts with targetOrigin='*' (D-129 regression)"
else
  fail "the bridge default-peer wildcard fix or its regression test is missing"
fi

# --- the host bridge clones every outbound message (D-129) -----------------
if grep -q "postSafe" web/inspector/src/host/host-bridge.ts; then
  ok "the host bridge unwraps Svelte \$state via postSafe (D-129)"
else
  fail "the host bridge does not unwrap Svelte \$state before postMessage"
fi

# --- the three Playwright-captured widget screenshots are checked in -------
# The end-to-end demo (D-129 + D-130 closure) produces three real renderings:
# chart, table, metric-card. The PR embeds them inline.
if [ -f docs/screenshots/analytics-widgets/chart.png ] \
   && [ -f docs/screenshots/analytics-widgets/table.png ] \
   && [ -f docs/screenshots/analytics-widgets/metric-card.png ]; then
  ok "the three Playwright-captured widget screenshots are checked in"
else
  fail "one or more of the three widget screenshots is missing under docs/screenshots/analytics-widgets/"
fi

# --- Phase 24 finish (D-131): operator-initiated tools/call surface ---------
# The inspector backend exposes POST /api/tools/invoke and the inspect CLI
# wires it through ToolsFromServer when --url is set. The frontend's
# ToolsPanel renders the operator form and POSTs to the endpoint.
if grep -q "ToolsFromServer" internal/inspector/invoke.go \
   && grep -q "/api/tools/invoke" internal/inspector/assets.go \
   && grep -q "ToolsFromServer" internal/cli/inspect.go \
   && grep -q "invokeTool" web/inspector/src/lib/api.ts \
   && grep -q "invoke-form" web/inspector/src/lib/ToolsPanel.svelte; then
  ok "the inspector ships operator-initiated tools/call (D-131)"
else
  fail "the operator-initiated tools/call surface is missing one of its pieces"
fi

# --- Phase 24 finish: the Dockyard wordmark sits in the inspector header ----
# The asset is staged in web/inspector/src/assets/, imported by App.svelte,
# and wired through PageHeader.lead.
if [ -f web/inspector/src/assets/dockyard-logo.png ] \
   && grep -q "dockyard-logo" web/inspector/src/App.svelte \
   && grep -q "header-logo" web/inspector/src/App.svelte; then
  ok "the Dockyard wordmark is wired into PageHeader.lead"
else
  fail "the inspector header logo is missing one of its pieces"
fi

# --- Phase 24 finish (D-132): analytics-widgets template wires obs/v1 -------
# The template constructs an SSESink and mounts /obs/v1/stream on the MCP
# listener so a single --url makes the inspector's Events/Analytics real.
if grep -q "obs.NewSSESink" templates/analytics-widgets/main.go.tmpl \
   && grep -q "obsSink.Handler" templates/analytics-widgets/main.go.tmpl \
   && grep -q "/obs/v1/stream" templates/analytics-widgets/main.go.tmpl; then
  ok "the analytics-widgets template exposes obs/v1 on the MCP listener (D-132)"
else
  fail "the analytics-widgets template does not expose the obs/v1 SSE endpoint"
fi

# --- Phase 24 finish (D-133): AppFrame.sendToolResult is loop-guarded -------
if grep -q "lastSentPayload" web/inspector/src/lib/AppFrame.svelte; then
  ok "the AppFrame push-tool-result effect guards against re-fire loops (D-133)"
else
  fail "AppFrame.sendToolResult is not guarded against re-fire loops"
fi

# --- Phase 24 finish: every rail tab has a captured end-to-end screenshot ---
finish_dir="docs/screenshots/phase-24-finish"
missing=""
for f in events.png rpc.png tools-invoke.png verdicts.png tasks.png analytics.png fixtures-with-logo.png; do
  [ -f "$finish_dir/$f" ] || missing="$missing $f"
done
if [ -z "$missing" ]; then
  ok "every rail tab is captured working end-to-end under $finish_dir/"
else
  fail "missing rail-tab screenshot(s):$missing"
fi

smoke_summary
