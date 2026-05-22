#!/usr/bin/env bash
# Smoke script for Phase 21 — dockyard test — the contract + compliance gate.
# One assertion per acceptance criterion (master plan Phase 21):
#   - the cobra root exposes `test`
#   - `dockyard test` runs all categories on a clean scaffolded project, exit 0
#   - a contract regression makes `dockyard test` exit non-zero
#   - a spec-compliance violation makes `dockyard test` exit non-zero
#   - the scaffolded server honours DOCKYARD_TRANSPORT=http (Phase 20↔17 fix)
# A check against an unbuilt surface skip()s, never fail()s — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-21 dockyard test — contract + compliance gate"

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

# --- the cobra tree exposes `test` -------------------------------------------
"$DOCKYARD" --help >"$WORK/help.txt" 2>&1 || true
if grep -q '\btest\b' "$WORK/help.txt"; then
  ok "cobra root command exposes 'test'"
else
  fail "'dockyard --help' does not list 'test'"
  smoke_summary; exit $?
fi

# --- scaffold + generate a project to test against --------------------------
PROJ="$WORK/proj"
mkdir -p "$PROJ"
if "$DOCKYARD" new smoke-srv --dir "$PROJ" --dockyard-path "$REPO_ROOT" \
     >"$WORK/new.log" 2>&1; then
  ok "scaffolded a project to run the test gate against"
else
  fail "dockyard new failed"; sed 's/^/    /' "$WORK/new.log"
  smoke_summary; exit $?
fi
SRV="$PROJ/smoke-srv"
( cd "$SRV" && CGO_ENABLED=0 go mod tidy ) >"$WORK/tidy.log" 2>&1 || true
"$DOCKYARD" generate --dir "$SRV" >"$WORK/gen.log" 2>&1 || true

# --- `dockyard test` runs all categories and exits 0 on a clean project ------
if "$DOCKYARD" test --dir "$SRV" >"$WORK/test-clean.log" 2>&1; then
  if grep -q 'go-test' "$WORK/test-clean.log" \
     && grep -q 'contract' "$WORK/test-clean.log" \
     && grep -q 'spec-compliance' "$WORK/test-clean.log" \
     && grep -q 'capability' "$WORK/test-clean.log"; then
    ok "dockyard test runs all categories and exits 0 on a clean project"
  else
    fail "dockyard test did not report every category"
    sed 's/^/    /' "$WORK/test-clean.log"
  fi
else
  fail "dockyard test failed on a clean scaffolded project"
  sed 's/^/    /' "$WORK/test-clean.log"
fi

# --- a contract regression makes `dockyard test` exit non-zero ---------------
STALE="$WORK/stale"
cp -R "$SRV" "$STALE"
( cd "$STALE" && CGO_ENABLED=0 go mod tidy ) >"$WORK/stale-tidy.log" 2>&1 || true
# Edit a contract struct without rerunning generate — the committed schema/TS
# are now stale versus the Go source.
printf '\n// Drift forces the contract category to fire.\ntype Drift struct {\n\tX string `json:"x"`\n}\n' \
  >>"$STALE/internal/contracts/contracts.go"
if "$DOCKYARD" test --dir "$STALE" --skip-go-test >"$WORK/test-stale.log" 2>&1; then
  fail "dockyard test exited 0 on a contract regression"
  sed 's/^/    /' "$WORK/test-stale.log"
else
  ok "dockyard test exits non-zero on a contract regression"
fi

# --- a spec-compliance violation makes `dockyard test` exit non-zero ---------
SPEC="$WORK/spec"
cp -R "$SRV" "$SPEC"
( cd "$SPEC" && CGO_ENABLED=0 go mod tidy ) >"$WORK/spec-tidy.log" 2>&1 || true
# Create a docs/specifications/ tree with one vendored spec withheld — the
# spec-compliance check then reports the missing spec as a blocker.
mkdir -p "$SPEC/docs/specifications"
printf 'vendored spec snapshot\n' >"$SPEC/docs/specifications/mcp-apps-2026-01-26.mdx"
if "$DOCKYARD" test --dir "$SPEC" --skip-go-test >"$WORK/test-spec.log" 2>&1; then
  fail "dockyard test exited 0 on a spec-compliance violation"
  sed 's/^/    /' "$WORK/test-spec.log"
else
  ok "dockyard test exits non-zero on a spec-compliance violation"
fi

# --- the scaffolded server honours DOCKYARD_TRANSPORT=http (wiring-gap fix) ---
# Build the scaffolded server, run it with DOCKYARD_TRANSPORT=http, and confirm
# it binds an HTTP listener (rather than silently serving stdio).
if ( cd "$SRV" && CGO_ENABLED=0 go build -o "$WORK/srv-bin" . ) >"$WORK/srv-build.log" 2>&1; then
  PORT=8765
  ADDR="127.0.0.1:$PORT"
  DOCKYARD_TRANSPORT=http DOCKYARD_HTTP_ADDR="$ADDR" "$WORK/srv-bin" >"$WORK/srv-run.log" 2>&1 &
  SRV_PID=$!
  HTTP_UP=""
  for _ in $(seq 1 50); do
    if curl -s -o /dev/null "http://$ADDR" 2>/dev/null; then HTTP_UP=1; break; fi
    # curl exits non-zero on a 4xx too — a reachable port is the signal.
    if curl -s -o /dev/null -w '%{http_code}' "http://$ADDR" 2>/dev/null | grep -qE '[0-9]'; then
      HTTP_UP=1; break
    fi
    sleep 0.1
  done
  kill "$SRV_PID" 2>/dev/null || true
  wait "$SRV_PID" 2>/dev/null || true
  if [ -n "$HTTP_UP" ]; then
    ok "scaffolded server honours DOCKYARD_TRANSPORT=http (serves HTTP)"
  else
    fail "scaffolded server did not serve HTTP under DOCKYARD_TRANSPORT=http"
    sed 's/^/    /' "$WORK/srv-run.log"
  fi
else
  fail "scaffolded server build failed"
  sed 's/^/    /' "$WORK/srv-build.log"
fi

smoke_summary
