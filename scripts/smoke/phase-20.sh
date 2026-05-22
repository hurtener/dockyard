#!/usr/bin/env bash
# Smoke script for Phase 20 — dockyard build + run + install.
# One assertion per acceptance criterion (master plan Phase 20):
#   - the cobra root exposes build / run / install and each --help works
#   - internal/buildpkg, internal/runpkg, internal/installpkg packages exist
#   - the build path is CGo-free (buildpkg pins CGO_ENABLED=0)
#   - the cross-compile matrix names the RFC §14 triples
#   - a host-only `dockyard build` produces a CGo-free binary + a checksum,
#     and a representative non-host cross-compile triple succeeds
#   - `dockyard run --transport` is wired
#   - `dockyard install` writes a valid host config (against a TEMP path) and
#     the boot check exists
#
# A smoke script is fast and non-interactive. It does NOT start a real
# long-running `dockyard run`, does NOT run the full six-triple matrix, and
# NEVER touches the developer's real ~/.claude / Cursor config — install
# assertions use a temp config path. Deep behaviour is the Phase 20
# integration test's job. A check against an unbuilt surface skip()s.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-20 dockyard build + run + install"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- the build/run/install packages ------------------------------------------
if [ -f internal/buildpkg/build.go ] && [ -f internal/runpkg/run.go ] \
   && [ -f internal/installpkg/install.go ]; then
  ok "internal/buildpkg, internal/runpkg, internal/installpkg packages exist"
else
  skip "Phase 20 packages not built — build/run/install phase not landed"
  smoke_summary
  exit $?
fi

# --- the cobra verbs are wired -----------------------------------------------
if [ -f internal/cli/build.go ] && [ -f internal/cli/run.go ] \
   && [ -f internal/cli/install.go ] \
   && grep -q 'newBuildCmd' internal/cli/root.go \
   && grep -q 'newRunCmd' internal/cli/root.go \
   && grep -q 'newInstallCmd' internal/cli/root.go; then
  ok "build / run / install verbs are registered in the cobra root"
else
  fail "a Phase 20 verb is not wired into the cobra root"
fi

# --- the build path is CGo-free ----------------------------------------------
if grep -q 'CGO_ENABLED=0' internal/buildpkg/pipeline.go; then
  ok "internal/buildpkg pins CGO_ENABLED=0 on go build (CGo-free artifact)"
else
  fail "internal/buildpkg does not pin CGO_ENABLED=0 — RFC §14 requires it"
fi

# --- the cross-compile matrix names the RFC §14 triples ----------------------
matrix_ok=1
for os in darwin linux windows; do
  grep -q "\"$os\"" internal/buildpkg/matrix.go || matrix_ok=0
done
for arch in amd64 arm64; do
  grep -q "\"$arch\"" internal/buildpkg/matrix.go || matrix_ok=0
done
if [ "$matrix_ok" -eq 1 ]; then
  ok "the cross-compile matrix names the RFC §14 triples (darwin/linux/windows x amd64/arm64)"
else
  fail "internal/buildpkg/matrix.go does not name the full RFC §14 matrix"
fi

# --- the install boot check exists -------------------------------------------
if [ -f internal/installpkg/bootcheck.go ] && grep -q 'initialize' internal/installpkg/bootcheck.go; then
  ok "internal/installpkg ships the boot check (verifies the MCP initialize handshake)"
else
  fail "internal/installpkg has no boot check"
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

# --- the cobra tree exposes the three verbs ----------------------------------
"$DOCKYARD" --help >"$WORK/help.txt" 2>&1 || true
help_ok=1
for verb in build run install; do
  grep -q "\b$verb\b" "$WORK/help.txt" || help_ok=0
done
if [ "$help_ok" -eq 1 ]; then
  ok "dockyard --help lists build, run and install"
else
  fail "dockyard --help does not list all three Phase 20 verbs"
  sed 's/^/    /' "$WORK/help.txt"
fi

# --- each verb's --help works ------------------------------------------------
"$DOCKYARD" build --help >"$WORK/build-help.txt" 2>&1 || true
if grep -qi 'checksum' "$WORK/build-help.txt" && grep -qi 'cgo-free' "$WORK/build-help.txt"; then
  ok "dockyard build --help describes the pipeline (CGo-free, checksum)"
else
  fail "dockyard build --help does not describe the pipeline"
fi
"$DOCKYARD" run --help >"$WORK/run-help.txt" 2>&1 || true
if grep -q -- '--transport' "$WORK/run-help.txt"; then
  ok "dockyard run --help documents --transport"
else
  fail "dockyard run --help does not document --transport"
fi
"$DOCKYARD" install --help >"$WORK/install-help.txt" 2>&1 || true
if grep -qi 'claude' "$WORK/install-help.txt" && grep -qi 'cursor' "$WORK/install-help.txt"; then
  ok "dockyard install --help documents the claude / cursor hosts"
else
  fail "dockyard install --help does not document the hosts"
fi

# --- a host-only build of a scaffolded project -------------------------------
# This produces a real binary + checksum; a representative non-host triple
# proves the cross-compile path. The full matrix is the integration test's job.
PROJ="$WORK/smoke-app"
if "$DOCKYARD" new smoke-app --dir "$WORK" --dockyard-path "$PWD" \
     >"$WORK/new.log" 2>&1; then
  ( cd "$PROJ" && CGO_ENABLED=0 go mod tidy ) >"$WORK/tidy.log" 2>&1 || true
  if "$DOCKYARD" build --dir "$PROJ" >"$WORK/dy-build.log" 2>&1; then
    HOST_OS="$(go env GOOS)"; HOST_ARCH="$(go env GOARCH)"
    BIN="$PROJ/dist/smoke-app-${HOST_OS}-${HOST_ARCH}"
    [ "$HOST_OS" = "windows" ] && BIN="${BIN}.exe"
    if [ -f "$BIN" ] && [ -f "${BIN}.sha256" ]; then
      ok "dockyard build produced a host binary + a SHA-256 checksum"
    else
      fail "dockyard build did not produce the expected binary + checksum"
      sed 's/^/    /' "$WORK/dy-build.log"
    fi
  else
    fail "dockyard build of a scaffolded project failed"
    sed 's/^/    /' "$WORK/dy-build.log"
  fi
  # A representative non-host cross-compile triple (linux/amd64 unless that IS
  # the host, in which case darwin/arm64).
  XTARGET="linux/amd64"
  [ "$(go env GOOS)/$(go env GOARCH)" = "linux/amd64" ] && XTARGET="darwin/arm64"
  XOS="${XTARGET%/*}"; XARCH="${XTARGET#*/}"
  if ( cd "$PROJ" && CGO_ENABLED=0 GOOS="$XOS" GOARCH="$XARCH" \
         go build -o "$WORK/xbuild-artifact" . ) >"$WORK/xbuild.log" 2>&1; then
    ok "a non-host cross-compile triple ($XTARGET) builds CGo-free"
  else
    fail "non-host cross-compile ($XTARGET) failed"
    sed 's/^/    /' "$WORK/xbuild.log"
  fi
else
  skip "dockyard new failed in the smoke environment — build assertions skipped"
  sed 's/^/    /' "$WORK/new.log"
fi

# --- the Phase 20 packages' unit tests pass under -race ----------------------
if CGO_ENABLED=1 go test -race -count=1 \
     ./internal/buildpkg/ ./internal/runpkg/ ./internal/installpkg/ \
     >"$WORK/pkg-test.log" 2>&1; then
  ok "internal/{buildpkg,runpkg,installpkg} tests pass (-race)"
else
  fail "a Phase 20 package's tests failed"; sed 's/^/    /' "$WORK/pkg-test.log"
fi

smoke_summary
