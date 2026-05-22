#!/usr/bin/env bash
# Smoke script for Phase 19 — dockyard dev (the fsnotify orchestrator).
# One assertion per acceptance criterion (master plan Phase 19):
#   - the cobra root exposes `dev` and `dockyard dev --help` works
#   - the internal/devloop orchestrator package exists
#   - the watcher embeds fsnotify — no shell-out to air/wgo (one process)
#   - the orchestrator's testable surface passes (debounce, classification,
#     restart, codegen-on-change, Vite supervision, clean teardown)
#   - `dockyard dev` degrades gracefully against a project with no web/ UI
#
# A smoke script is fast and non-interactive: it does NOT start a real
# long-running `dockyard dev`. Structural presence + the orchestrator package's
# unit tests are the smoke check; deep behaviour is the Phase 19 integration
# test's job. A check against an unbuilt surface skip()s, never fail()s.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-19 dockyard dev (fsnotify orchestrator)"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- the internal/devloop orchestrator package -------------------------------
if [ -f internal/devloop/devloop.go ] && [ -f internal/devloop/watcher.go ] \
   && [ -f internal/devloop/supervisor.go ]; then
  ok "internal/devloop orchestrator package exists"
else
  skip "internal/devloop not built — dev-loop phase not landed"
  smoke_summary
  exit $?
fi

# --- the watcher embeds fsnotify, no air/wgo shell-out -----------------------
if grep -rq 'fsnotify' internal/devloop/; then
  ok "internal/devloop embeds fsnotify (one process, no external dev tool)"
else
  fail "internal/devloop does not reference fsnotify"
fi
# Grep for an executable invocation of air/wgo, ignoring comment lines (the
# package doc legitimately names them as the tools it deliberately does NOT use).
if grep -rn 'exec\.Command' internal/devloop/ 2>/dev/null \
     | grep -vE '^\s*//' | grep -qE '"(air|wgo)"'; then
  fail "internal/devloop shells out to air/wgo — RFC §9.2 forbids it"
else
  ok "internal/devloop does not shell out to air/wgo"
fi

# --- the `dockyard dev` cobra verb -------------------------------------------
if [ -f internal/cli/dev.go ] && grep -q 'newDevCmd' internal/cli/root.go; then
  ok "internal/cli/dev.go exists and root.go registers the dev verb"
else
  fail "the dev verb is not wired into the cobra root"
fi

# --- the dockyard binary -----------------------------------------------------
if [ ! -f cmd/dockyard/main.go ]; then
  skip "cmd/dockyard/main.go absent — CLI phase not landed"
  smoke_summary
  exit $?
fi
if CGO_ENABLED=0 go build -o "$WORK/dockyard" ./cmd/dockyard 2>"$WORK/build.log"; then
  ok "dockyard binary builds CGo-free"
else
  fail "dockyard binary build failed"; sed 's/^/    /' "$WORK/build.log"
  smoke_summary; exit $?
fi
DOCKYARD="$WORK/dockyard"

# --- the cobra tree exposes `dev` --------------------------------------------
"$DOCKYARD" --help >"$WORK/help.txt" 2>&1 || true
if grep -q '\bdev\b' "$WORK/help.txt"; then
  ok "cobra root command exposes 'dev'"
else
  fail "'dockyard --help' does not list 'dev'"
fi

# --- `dockyard dev --help` describes the loop --------------------------------
"$DOCKYARD" dev --help >"$WORK/dev-help.txt" 2>&1 || true
if grep -qi 'fsnotify' "$WORK/dev-help.txt" && grep -qi 'vite' "$WORK/dev-help.txt"; then
  ok "dockyard dev --help describes the embedded loop (fsnotify + Vite)"
else
  fail "dockyard dev --help does not describe the loop"
  sed 's/^/    /' "$WORK/dev-help.txt"
fi

# --- the orchestrator's testable surface passes ------------------------------
# This drives debounce, event classification, the supervisor's restart and
# clean-teardown behaviour, codegen-on-change, and the graceful no-web/ case —
# without starting a real long-running dev session.
if CGO_ENABLED=1 go test -race -count=1 ./internal/devloop/ >"$WORK/devloop-test.log" 2>&1; then
  ok "internal/devloop tests pass (-race): debounce, restart, codegen, teardown"
else
  fail "internal/devloop tests failed"; sed 's/^/    /' "$WORK/devloop-test.log"
fi

smoke_summary
