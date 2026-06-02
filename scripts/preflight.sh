#!/usr/bin/env bash
# Dockyard preflight gate — the same gate the pre-commit hook and CI enforce.
# Build -> run every per-phase smoke script -> drift-audit. No-ops gracefully
# where the surface isn't built yet (smoke scripts SKIP on 404/405/501).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== preflight: build =="
make build

echo "== preflight: smoke =="
# Collect every smoke script that runs in this gate:
#  - phase-NN.sh: one per shipped phase (drift-audit pairs them).
#  - v<MAJOR>.<MINOR>-wave-<LETTER>.sh: post-V1 waves, intentionally not
#    phase-paired by drift-audit (a wave is not a phase) but still exercised on
#    every preflight so the wave's acceptance criteria are gated (D-164).
smoke_scripts=()
if compgen -G "scripts/smoke/phase-*.sh" >/dev/null; then
  for s in scripts/smoke/phase-*.sh; do smoke_scripts+=("$s"); done
fi
if compgen -G "scripts/smoke/v[0-9]*-wave-*.sh" >/dev/null; then
  for s in scripts/smoke/v[0-9]*-wave-*.sh; do smoke_scripts+=("$s"); done
fi

smoke_fail=0
if [ "${#smoke_scripts[@]}" -eq 0 ]; then
  echo "(no phase smoke scripts yet)"
else
  # Run the smoke scripts CONCURRENTLY (D-188). With ~40 phase scripts, each
  # spawning its own go build/test, a sequential run dominated preflight
  # wall-clock. Most scripts are hermetic — own mktemp/OS-assigned ports, or a
  # distinct fixed /tmp name — so they parallelise safely.
  #
  # EXCEPTION: a script that exercises the shared in-repo web/ workspace (npm
  # install + vitest --coverage in web/{bridge,ui,inspector}) is NOT hermetic —
  # two such runs clobber each other's web/<pkg>/coverage/.tmp and races the
  # node_modules install. Those scripts carry a `# preflight: serial` marker and
  # run sequentially, never concurrently with another web gate. The first CI run
  # of the all-parallel version proved the hazard (web/inspector coverage ENOENT
  # from phase-11 vs phase-23). The two batches do not overlap: the parallel
  # batch contains no web-workspace WRITER (the scaffold smokes build their own
  # temp web/; the rest only read in-repo web/ files), so it cannot collide with
  # the serial batch either.
  #
  # Output is captured per-script and replayed in stable (collection) order so
  # the log reads identically to the old sequential run.
  parallel_scripts=()
  serial_scripts=()
  for s in "${smoke_scripts[@]}"; do
    if grep -qE '^#[[:space:]]*preflight:[[:space:]]*serial\b' "$s"; then
      serial_scripts+=("$s")
    else
      parallel_scripts+=("$s")
    fi
  done

  # Concurrency: PREFLIGHT_JOBS overrides; otherwise min(cores, 6) — go itself
  # parallelises internally, so over-subscribing the cores hurts more than it
  # helps. xargs -P (not bash 4's `wait -n`) keeps this portable to the macOS
  # system bash 3.2.
  ncpu="$( { command -v nproc >/dev/null 2>&1 && nproc; } || sysctl -n hw.ncpu 2>/dev/null || echo 4 )"
  jobs="${PREFLIGHT_JOBS:-$(( ncpu > 6 ? 6 : (ncpu < 1 ? 1 : ncpu) ))}"
  echo "(running ${#parallel_scripts[@]} smoke scripts ${jobs}-way parallel + ${#serial_scripts[@]} serial web gate(s))"

  smoke_outdir="$(mktemp -d)"
  trap 'rm -rf "$smoke_outdir"' EXIT
  export smoke_outdir

  # run_smoke executes one script, capturing combined output to a per-script
  # file and recording a failure marker. It always exits 0 so xargs keeps
  # scheduling the rest; the aggregate verdict is computed from the markers.
  run_smoke() {
    local s="$1" base
    base="$(basename "$s" .sh)"
    if bash "$s" >"$smoke_outdir/$base.out" 2>&1; then
      echo "   ok   $base"
    else
      printf '%s\n' "$base" >>"$smoke_outdir/failed"
      echo "   FAIL $base"
    fi
  }
  export -f run_smoke

  # Parallel batch: the hermetic scripts, concurrently.
  if [ "${#parallel_scripts[@]}" -gt 0 ]; then
    printf '%s\n' "${parallel_scripts[@]}" \
      | xargs -P "$jobs" -I{} bash -c 'run_smoke "$@"' _ {}
  fi
  # Serial batch: the shared-web-workspace gates, one at a time.
  for s in "${serial_scripts[@]}"; do
    run_smoke "$s"
  done

  # Replay captured output in stable collection order — identical log shape to
  # the old sequential gate.
  for s in "${smoke_scripts[@]}"; do
    base="$(basename "$s" .sh)"
    echo "-- $s"
    [ -f "$smoke_outdir/$base.out" ] && cat "$smoke_outdir/$base.out"
  done
  if [ -f "$smoke_outdir/failed" ]; then
    smoke_fail=1
    echo "== preflight: smoke FAILURES =="
    sed 's/^/  /' "$smoke_outdir/failed"
  fi
fi

echo "== preflight: drift-audit =="
bash scripts/drift-audit.sh

if [ "$smoke_fail" -ne 0 ]; then
  echo "PREFLIGHT FAILED: a smoke script reported FAIL"
  exit 1
fi
echo "PREFLIGHT OK"
