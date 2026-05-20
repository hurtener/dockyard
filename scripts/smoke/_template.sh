#!/usr/bin/env bash
# Smoke script for Phase NN — <slug>.
# Copy to phase-NN.sh. Add one assertion per acceptance criterion.
# A check against an unbuilt surface should skip(), not fail() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-NN <slug>"

# Example assertions — replace with the phase's real checks:
# if [ -d internal/example ]; then ok "internal/example exists"; else skip "internal/example not built"; fi

smoke_summary
