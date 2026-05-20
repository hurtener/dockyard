# Shared helpers for Dockyard per-phase smoke scripts. Source, do not execute.
# Convention: a check that hits an unbuilt surface (HTTP 404/405/501, or a
# missing binary/command) SKIPs rather than FAILs, so future phases coexist
# with the current build. OK >= count(acceptance criteria) and FAIL = 0 is the
# done-definition (AGENTS.md §4.2).

SMOKE_OK=0
SMOKE_FAIL=0
SMOKE_SKIP=0

ok()   { SMOKE_OK=$((SMOKE_OK+1));     echo "  OK   - $*"; }
fail() { SMOKE_FAIL=$((SMOKE_FAIL+1)); echo "  FAIL - $*"; }
skip() { SMOKE_SKIP=$((SMOKE_SKIP+1)); echo "  SKIP - $*"; }

smoke_summary() {
  echo "  -- OK=$SMOKE_OK FAIL=$SMOKE_FAIL SKIP=$SMOKE_SKIP"
  [ "$SMOKE_FAIL" -eq 0 ]
}
