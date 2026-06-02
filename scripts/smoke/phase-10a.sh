#!/usr/bin/env bash
# Smoke script for Phase 10a — UI design system, tokens & conventions.
# One assertion per acceptance criterion (docs/plans/phase-10a-design-system.md).
# A check against an unbuilt surface skips rather than fails — see common.sh.
# preflight: serial — exercises the shared in-repo web/ workspace (npm/vitest
# coverage), so the preflight gate runs it sequentially, never concurrently
# with another web gate (D-188).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-10a design-system"

UI=web/ui

# 1. The web/ui project and its config files exist.
if [ -f "$UI/package.json" ] \
   && [ -f "$UI/tsconfig.json" ] \
   && [ -f "$UI/svelte.config.js" ] \
   && [ -f "$UI/vitest.config.ts" ]; then
  ok "web/ui package + config files exist"
else
  fail "web/ui package or a config file is missing"
fi

# 2. The package is named dockyard-ui and carries the `gate` script.
if [ -f "$UI/package.json" ] \
   && grep -q '"dockyard-ui"' "$UI/package.json" \
   && grep -q '"gate"' "$UI/package.json"; then
  ok "web/ui is dockyard-ui with a gate script"
else
  fail "web/ui package.json missing name or gate script"
fi

# 3. The design-token module exists — CSS custom properties + typed export.
if [ -f "$UI/src/tokens.css" ] && [ -f "$UI/src/tokens.ts" ]; then
  if grep -q -- '--dy-' "$UI/src/tokens.css"; then
    ok "design tokens exist (tokens.css --dy-* + tokens.ts)"
  else
    fail "tokens.css carries no --dy-* custom properties"
  fi
else
  fail "design-token module (tokens.css / tokens.ts) missing"
fi

# 4. Every component in the design-spec.md §3 inventory exists.
COMPONENTS="AppShell PageHeader DetailRail RailCard ActionBar ConnectionFooter \
DataTable Pagination FilterBar MetricCard StatusChip Timeline JsonInspector \
CodeBlock PageState LoadingState EmptyState ErrorState PermissionState"
missing=""
for c in $COMPONENTS; do
  [ -f "$UI/src/$c.svelte" ] || missing="$missing $c"
done
if [ -z "$missing" ]; then
  ok "all 19 web/ui inventory components exist"
else
  fail "missing web/ui components:$missing"
fi

# 5. The public barrel exports the inventory.
if [ -f "$UI/src/index.ts" ] \
   && grep -q 'PageState' "$UI/src/index.ts" \
   && grep -q 'DataTable' "$UI/src/index.ts" \
   && grep -q 'tokens' "$UI/src/index.ts"; then
  ok "src/index.ts barrel exports the public surface"
else
  fail "src/index.ts barrel missing or incomplete"
fi

# 6. The web/ui package type-checks and its tests pass.
if ! command -v npm >/dev/null 2>&1; then
  skip "npm not installed — web/ui gate deferred"
elif [ -f "$UI/package.json" ]; then
  if ( cd "$UI" \
        && { [ -d node_modules ] || npm ci --no-audit --no-fund >/dev/null 2>&1; } \
        && npm run gate >/dev/null 2>&1 ); then
    ok "web/ui gate passes (svelte-check + tsc + vitest)"
  else
    fail "web/ui gate (npm run gate) failed"
  fi
else
  skip "web/ui not built — gate deferred"
fi

# 7. CONVENTIONS.md §3 is filled with the delivered inventory.
if grep -q 'PermissionState' docs/design/CONVENTIONS.md \
   && ! grep -q 'the planned set' docs/design/CONVENTIONS.md; then
  ok "CONVENTIONS.md §3 documents the delivered inventory"
else
  fail "CONVENTIONS.md §3 still shows the planned set"
fi

# 8. The logo and the approved inspector mockup exist.
if [ -f docs/design/logo.png ] && [ -f docs/design/mockups/inspector.png ]; then
  ok "logo + approved inspector mockup exist"
else
  fail "logo or inspector mockup missing"
fi

# 9. The Makefile web target gates web/ui alongside web/bridge.
if grep -q 'web/ui' Makefile; then
  ok "Makefile web/web-install targets reference web/ui"
else
  fail "Makefile does not gate web/ui"
fi

smoke_summary
