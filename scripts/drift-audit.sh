#!/usr/bin/env bash
# Dockyard drift-audit — mechanical design-coherence checks.
#   - AGENTS.md == CLAUDE.md (the verbatim mirror invariant)
#   - required hygiene files exist
#   - every docs/plans/phase-NN-*.md has a matching scripts/smoke/phase-NN.sh
#   - every `RFC §N.M` reference in a phase plan resolves to a real RFC heading
#   - every `brief NN` reference resolves to a docs/research/NN-*.md file
# Exits non-zero on any failure.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { echo "DRIFT: $*"; fail=1; }

# 1. Mirror invariant.
if ! diff -q AGENTS.md CLAUDE.md >/dev/null 2>&1; then
  note "AGENTS.md and CLAUDE.md differ"
fi

# 2. Required files.
for f in RFC-001-Dockyard.md README.md LICENSE Makefile \
         docs/decisions.md docs/glossary.md docs/research/INDEX.md \
         docs/plans/_template.md; do
  [ -f "$f" ] || note "required file missing: $f"
done

# 3. Phase plan <-> smoke script pairing.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  for p in docs/plans/phase-*.md; do
    nn=$(basename "$p" | sed -E 's/^phase-([0-9a-z]+)-.*/\1/')
    [ -f "scripts/smoke/phase-${nn}.sh" ] || \
      note "phase plan $p has no scripts/smoke/phase-${nn}.sh"
  done
fi

# 4. RFC section references in phase plans resolve.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  refs=$(grep -hoE 'RFC §[0-9]+(\.[0-9]+)*' docs/plans/phase-*.md 2>/dev/null \
         | sed -E 's/RFC §//' | sort -u)
  for r in $refs; do
    grep -qE "^#+ ${r}(\.| |\b)" RFC-001-Dockyard.md \
      || note "phase plan cites RFC §${r} — no matching RFC heading"
  done
fi

# 5. Brief references resolve.
if compgen -G "docs/plans/phase-*.md" >/dev/null; then
  briefs=$(grep -hoE 'brief [0-9]+' docs/plans/phase-*.md 2>/dev/null \
           | sed -E 's/brief 0*//' | sort -u)
  for b in $briefs; do
    bb=$(printf '%02d' "$b")
    compgen -G "docs/research/${bb}-*.md" >/dev/null \
      || note "phase plan cites brief ${b} — no docs/research/${bb}-*.md"
  done
fi

if [ "$fail" -ne 0 ]; then
  echo "DRIFT-AUDIT FAILED"
  exit 1
fi
echo "DRIFT-AUDIT OK"
