#!/usr/bin/env bash
# Smoke script for Phase 25 — approval-flows template + bridge
# elicitation-response (D-134) + scaffold tasks.Engine wiring (D-135).
#
# One assertion per acceptance criterion (docs/plans/phase-25-approval-flows.md):
#   - `dockyard new --template approval-flows` produces a project
#   - the produced project builds CGo-free
#   - the produced project's contract tests pass
#   - the materialised manifest declares 2 task-augmented tools + 1
#     inline-only app
#   - the scaffolded main.go constructs a real tasks.Engine + attaches it
#     via server.Options.Tasks (D-135)
#   - twelve fixtures (six per tool) ship under the materialised tree
#   - the shared FieldDiff lives in web/ui/ and is exported
#   - CONVENTIONS.md §3 lists FieldDiff
#   - the bridge ships ViewNotification.elicitationResponse and the
#     sendElicitationResponse helper (D-134)
#   - the inspector backend exposes POST /api/tasks/elicitation
#   - the AppShell supports fullViewport (Phase 25 cosmetic follow-up)
#   - the rename is complete (approval-flow singular, brief carve-outs)
#   - the integration test passes
#   - all phase-25 screenshots are checked in
#
# A check against an unbuilt surface skip()s, never fail()s.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-25 approval-flows template + bridge elicitation + tasks-engine scaffold"

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
  ok "dockyard binary builds CGo-free (with the approval-flows template embedded)"
else
  fail "dockyard binary build failed"; sed 's/^/    /' "$WORK/build.log"
  smoke_summary; exit $?
fi
DOCKYARD="$WORK/dockyard"

# --- `dockyard new --template approval-flows` -------------------------------
PROJ="$WORK/proj"
mkdir -p "$PROJ"
if "$DOCKYARD" new af-smoke --template approval-flows --dir "$PROJ" --dockyard-path "$REPO_ROOT" \
     >"$WORK/new.log" 2>&1; then
  ok "dockyard new --template approval-flows produces a project"
else
  fail "dockyard new --template approval-flows failed"; sed 's/^/    /' "$WORK/new.log"
  smoke_summary; exit $?
fi
SRV="$PROJ/af-smoke"

# --- the materialised project layout ----------------------------------------
if [ -f "$SRV/dockyard.app.yaml" ] && [ -f "$SRV/main.go" ] \
   && [ -f "$SRV/internal/contracts/contracts.go" ] \
   && [ -f "$SRV/internal/handlers/handlers.go" ] \
   && [ -f "$SRV/web/src/App.svelte" ] \
   && [ -f "$SRV/web/src/ApprovalCard.svelte" ] \
   && [ -f "$SRV/web/src/EditsForm.svelte" ] \
   && [ -f "$SRV/README.md" ]; then
  ok "materialised project includes the manifest, main, contracts, handlers, App, ApprovalCard and EditsForm"
else
  fail "materialised project is missing one of the expected files"
fi

# --- the materialised manifest declares 2 task-augmented tools + 1 app ------
required_count=$(grep -cE '^[[:space:]]+task_support: required' "$SRV/dockyard.app.yaml" || echo 0)
if grep -q "name: request_approval" "$SRV/dockyard.app.yaml" \
   && grep -q "name: propose_with_edits" "$SRV/dockyard.app.yaml" \
   && [ "$required_count" = "2" ] \
   && grep -q "display_modes: \[inline\]" "$SRV/dockyard.app.yaml"; then
  ok "manifest declares 2 task-augmented tools + 1 inline-only app"
else
  fail "manifest does not declare the expected tools + task_support + display_modes shape (required_count=$required_count)"
fi

# --- the scaffolded main.go wires tasks.Engine (D-135) ----------------------
if grep -q "tasks.NewEngine" "$SRV/main.go" \
   && grep -q "server.Options" "$SRV/main.go" \
   && grep -q "Tasks: engine" "$SRV/main.go" \
   && grep -q "engine.StartSweep" "$SRV/main.go"; then
  ok "the scaffolded main.go attaches a tasks.Engine via server.Options.Tasks (D-135)"
else
  fail "the scaffolded main.go does not wire the tasks.Engine"
fi

# --- twelve fixtures (six per tool) -----------------------------------------
fixture_count=$(find "$SRV/fixtures" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
if [ "$fixture_count" = "12" ]; then
  ok "materialised project ships 12 fixtures (6 per tool)"
else
  fail "expected 12 fixtures (6 per tool), got $fixture_count"
fi

# --- the scaffolded project builds and tests pass ---------------------------
if ( cd "$SRV" && CGO_ENABLED=0 go mod tidy && CGO_ENABLED=0 go build ./... ) \
     >"$WORK/proj-build.log" 2>&1; then
  ok "the materialised approval-flows project builds CGo-free"
else
  fail "the materialised approval-flows project does not build"; sed 's/^/    /' "$WORK/proj-build.log"
fi

if ( cd "$SRV" && CGO_ENABLED=0 go test ./... ) >"$WORK/proj-test.log" 2>&1; then
  ok "the materialised approval-flows project's contract tests pass"
else
  fail "the materialised approval-flows project's contract tests failed"; sed 's/^/    /' "$WORK/proj-test.log"
fi

# --- FieldDiff lives in web/ui and is documented in CONVENTIONS.md §3 -------
if [ -f web/ui/src/FieldDiff.svelte ] \
   && grep -q "FieldDiff" web/ui/src/index.ts; then
  ok "FieldDiff lives in web/ui/ and is exported"
else
  fail "FieldDiff is missing from web/ui/ or its barrel export"
fi
if grep -q "FieldDiff" docs/design/CONVENTIONS.md; then
  ok "docs/design/CONVENTIONS.md §3 lists FieldDiff"
else
  fail "FieldDiff is not documented in docs/design/CONVENTIONS.md §3"
fi
# FieldDiff must not have been duplicated under the template.
if [ ! -f templates/approval-flows/web/src/FieldDiff.svelte ]; then
  ok "FieldDiff is not duplicated under the template (composed from web/ui)"
else
  fail "FieldDiff is duplicated under the template — must be composed from web/ui"
fi
# The App composes the shared FieldDiff (it imports from @dockyard/ui).
if grep -q "FieldDiff" templates/approval-flows/web/src/EditsForm.svelte \
   && grep -q "@dockyard/ui" templates/approval-flows/web/src/EditsForm.svelte; then
  ok "the EditsForm composes FieldDiff from @dockyard/ui"
else
  fail "the EditsForm does not compose FieldDiff from @dockyard/ui"
fi

# --- the bridge ships the elicitation-response message (D-134) --------------
if grep -q "elicitationResponse" web/bridge/src/protocol.ts \
   && grep -q "ui/notifications/elicitation-response" web/bridge/src/protocol.ts \
   && grep -q "sendElicitationResponse" web/bridge/src/bridge.ts \
   && grep -q "ElicitationResponseParams" web/bridge/src/protocol.ts; then
  ok "the bridge ships the elicitation-response notification + the typed View helper (D-134)"
else
  fail "the bridge is missing one of: ViewNotification.elicitationResponse, sendElicitationResponse, ElicitationResponseParams"
fi

# --- the host bridge dispatches the elicitation-response (D-134) ------------
if grep -q "elicitationResponder" web/inspector/src/host/host-bridge.ts \
   && grep -q "setElicitationResponder" web/inspector/src/host/host-bridge.ts; then
  ok "the inspector host-bridge dispatches the elicitation-response (D-134)"
else
  fail "the inspector host-bridge does not handle the elicitation-response notification"
fi

# --- the inspector backend exposes /api/tasks/elicitation (D-134) -----------
if grep -q "/api/tasks/elicitation" internal/inspector/assets.go \
   && grep -q "ElicitationFromServer" internal/inspector/elicitation.go \
   && grep -q "Elicitor" internal/inspector/inspector.go \
   && grep -q "ElicitationFromServer" internal/cli/inspect.go \
   && grep -q "postElicitationResponse" web/inspector/src/lib/api.ts; then
  ok "the inspector ships operator-initiated elicitation-response (D-134)"
else
  fail "the elicitation-response surface is missing one of its pieces"
fi

# --- the AppShell supports fullViewport (Phase 25 cosmetic follow-up) -------
if grep -q "fullViewport" web/ui/src/AppShell.svelte \
   && grep -q "fullViewport" web/inspector/src/App.svelte \
   && grep -q "data-fullvh" web/ui/src/AppShell.svelte; then
  ok "AppShell supports fullViewport and the inspector enables it"
else
  fail "the fullViewport layout support is missing"
fi

# --- the rename is complete (approval-flow singular) ------------------------
# Allowed historical references: the two research briefs (carve-out per the
# rename plan) and decisions log mentions of old phases.
# Source code, CLI flags, manifests, and scripts must use the plural form.
# Use word-boundary grep: `approval-flow` followed by NOT `s` and a word
# boundary (so `approval-flows` is excluded). BSD grep supports `[[:>:]]`;
# fall back to a simple "approval-flow[^s]" pattern which is enough for the
# files we search.
forbidden=$(grep -rln "approval-flow[^s]" \
  --include='*.go' --include='*.sh' --include='*.yaml' --include='*.yml' \
  --include='*.svelte' --include='*.ts' --include='*.js' \
  internal/ cmd/ scripts/ templates/ web/ 2>/dev/null \
  | grep -v 'scripts/smoke/phase-25.sh' \
  || true)
if [ -z "$forbidden" ]; then
  ok "no source file (internal/, cmd/, scripts/, templates/, web/) references the singular old name"
else
  fail "source files still reference 'approval-flow' (singular):"
  echo "$forbidden" | sed 's/^/    /'
fi

# --- the integration test runs ---------------------------------------------
if CGO_ENABLED=1 go test -race -count=1 -run 'TestPhase25_' \
     ./test/integration/ >"$WORK/itest.log" 2>&1; then
  ok "Phase 25 integration test passes (tasks lifecycle + elicitation round-trip)"
else
  fail "Phase 25 integration test failed"; sed 's/^/    /' "$WORK/itest.log"
fi

# --- the four phase-25 screenshots are checked in ---------------------------
shot_dir="docs/screenshots/phase-25"
missing=""
for f in request-approval.png propose-with-edits.png tasks-panel-live.png layout-fullvh.png; do
  [ -f "$shot_dir/$f" ] || missing="$missing $f"
done
if [ -z "$missing" ]; then
  ok "all four phase-25 screenshots are checked in"
else
  fail "missing phase-25 screenshot(s):$missing"
fi

smoke_summary
