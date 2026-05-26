#!/usr/bin/env bash
# Dockyard preflight gate — the same gate the pre-commit hook and CI enforce.
# Build -> run every per-phase smoke script -> drift-audit. No-ops gracefully
# where the surface isn't built yet (smoke scripts SKIP on 404/405/501).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== preflight: build =="
make build

echo "== preflight: smoke =="
smoke_fail=0
# Phase smoke scripts (one per shipped phase, drift-audit pairs them).
if compgen -G "scripts/smoke/phase-*.sh" >/dev/null; then
  for s in scripts/smoke/phase-*.sh; do
    echo "-- $s"
    bash "$s" || smoke_fail=1
  done
else
  echo "(no phase smoke scripts yet)"
fi
# Post-V1 wave smoke scripts. The naming convention is
# scripts/smoke/v<MAJOR>.<MINOR>-wave-<LETTER>.sh; the matching plan lives
# under docs/plans/v<MAJOR>.<MINOR>-wave-<LETTER>-*.md and is intentionally
# not phase-paired by drift-audit (a wave is not a phase). The wave's smoke
# script still runs in the same gate so its acceptance criteria are
# exercised on every preflight (D-164).
if compgen -G "scripts/smoke/v[0-9]*-wave-*.sh" >/dev/null; then
  for s in scripts/smoke/v[0-9]*-wave-*.sh; do
    echo "-- $s"
    bash "$s" || smoke_fail=1
  done
fi

echo "== preflight: drift-audit =="
bash scripts/drift-audit.sh

if [ "$smoke_fail" -ne 0 ]; then
  echo "PREFLIGHT FAILED: a smoke script reported FAIL"
  exit 1
fi
echo "PREFLIGHT OK"
