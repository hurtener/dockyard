#!/usr/bin/env bash
# Smoke script for Phase 18 — dockyard generate + dockyard validate.
# One assertion per acceptance criterion (master plan Phase 18):
#   - the cobra root exposes `generate` and `validate`
#   - `dockyard generate` runs in a scaffolded project
#   - `dockyard generate` is idempotent (a second run produces no diff)
#   - `dockyard validate` exits 0 on a clean scaffolded project
#   - `dockyard validate` exits non-zero on an invalid manifest
#   - `dockyard validate` exits non-zero on a broken tool↔UI mapping
#   - `dockyard validate` exits non-zero on stale generated output
# A check against an unbuilt surface skip()s, never fail()s — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-18 dockyard generate + validate"

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
  ok "dockyard binary builds CGo-free"
else
  fail "dockyard binary build failed"; sed 's/^/    /' "$WORK/build.log"
  smoke_summary; exit $?
fi
DOCKYARD="$WORK/dockyard"

# --- the cobra tree exposes `generate` and `validate` ------------------------
"$DOCKYARD" --help >"$WORK/help.txt" 2>&1 || true
if grep -q '\bgenerate\b' "$WORK/help.txt" && grep -q '\bvalidate\b' "$WORK/help.txt"; then
  ok "cobra root command exposes 'generate' and 'validate'"
else
  fail "'dockyard --help' does not list 'generate' and 'validate'"
  smoke_summary; exit $?
fi

# --- scaffold a project to operate on ----------------------------------------
PROJ="$WORK/proj"
mkdir -p "$PROJ"
if "$DOCKYARD" new smoke-srv --dir "$PROJ" --dockyard-path "$REPO_ROOT" \
     >"$WORK/new.log" 2>&1; then
  ok "scaffolded a project to generate/validate against"
else
  fail "dockyard new failed"; sed 's/^/    /' "$WORK/new.log"
  smoke_summary; exit $?
fi
SRV="$PROJ/smoke-srv"
( cd "$SRV" && CGO_ENABLED=0 go mod tidy ) >"$WORK/tidy.log" 2>&1 || true

# --- `dockyard generate` runs ------------------------------------------------
if "$DOCKYARD" generate --dir "$SRV" >"$WORK/gen1.log" 2>&1; then
  ok "dockyard generate runs in a scaffolded project"
else
  fail "dockyard generate failed"; sed 's/^/    /' "$WORK/gen1.log"
  smoke_summary; exit $?
fi

# --- `dockyard generate` is idempotent ---------------------------------------
# Snapshot the generated artifacts, regenerate, diff.
cp -R "$SRV/internal/contracts" "$WORK/contracts-before"
if "$DOCKYARD" generate --dir "$SRV" >"$WORK/gen2.log" 2>&1 \
   && diff -r "$WORK/contracts-before" "$SRV/internal/contracts" >"$WORK/diff.log" 2>&1; then
  ok "dockyard generate is idempotent (second run produces no diff)"
else
  fail "dockyard generate is not idempotent"; sed 's/^/    /' "$WORK/diff.log"
fi

# --- `dockyard validate` exits 0 on a clean project --------------------------
if "$DOCKYARD" validate --dir "$SRV" >"$WORK/val-clean.log" 2>&1; then
  ok "dockyard validate exits 0 on a clean scaffolded project"
else
  fail "dockyard validate failed on a clean project"; sed 's/^/    /' "$WORK/val-clean.log"
fi

# --- `dockyard validate` exits non-zero on an invalid manifest ---------------
BADMAN="$WORK/badman"
cp -R "$SRV" "$BADMAN"
# Blank the manifest name — a required field; the manifest no longer loads.
sed 's/^name: .*/name: ""/' "$SRV/dockyard.app.yaml" >"$BADMAN/dockyard.app.yaml"
if "$DOCKYARD" validate --dir "$BADMAN" >"$WORK/val-badman.log" 2>&1; then
  fail "dockyard validate exited 0 on an invalid manifest"
else
  ok "dockyard validate exits non-zero on an invalid manifest"
fi

# --- `dockyard validate` exits non-zero on a broken tool↔UI mapping ----------
BADMAP="$WORK/badmap"
cp -R "$SRV" "$BADMAP"
# Wire the tool to a ui id that no apps[] entry declares.
sed 's#\(output: internal/contracts.GreetOutput\)#\1\n    ui: ghost#' \
  "$SRV/dockyard.app.yaml" >"$BADMAP/dockyard.app.yaml"
if "$DOCKYARD" validate --dir "$BADMAP" >"$WORK/val-badmap.log" 2>&1; then
  fail "dockyard validate exited 0 on a broken tool↔UI mapping"
else
  ok "dockyard validate exits non-zero on a broken tool↔UI mapping"
fi

# --- `dockyard validate` exits non-zero on stale generated output ------------
STALE="$WORK/stale"
cp -R "$SRV" "$STALE"
( cd "$STALE" && CGO_ENABLED=0 go mod tidy ) >"$WORK/stale-tidy.log" 2>&1 || true
# Edit a contract struct without rerunning generate — the committed schema/TS
# are now stale versus the Go source.
printf '\n// Drift forces the stale-codegen check to fire.\ntype Drift struct {\n\tX string `json:"x"`\n}\n' \
  >>"$STALE/internal/contracts/contracts.go"
if "$DOCKYARD" validate --dir "$STALE" >"$WORK/val-stale.log" 2>&1; then
  fail "dockyard validate exited 0 on stale generated output"
  sed 's/^/    /' "$WORK/val-stale.log"
else
  ok "dockyard validate exits non-zero on stale generated output"
fi

smoke_summary
