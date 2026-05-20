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
if compgen -G "scripts/smoke/phase-*.sh" >/dev/null; then
  for s in scripts/smoke/phase-*.sh; do
    echo "-- $s"
    bash "$s" || smoke_fail=1
  done
else
  echo "(no phase smoke scripts yet)"
fi

echo "== preflight: drift-audit =="
bash scripts/drift-audit.sh

if [ "$smoke_fail" -ne 0 ]; then
  echo "PREFLIGHT FAILED: a smoke script reported FAIL"
  exit 1
fi
echo "PREFLIGHT OK"
