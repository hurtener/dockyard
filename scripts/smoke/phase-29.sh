#!/usr/bin/env bash
# Smoke script for Phase 29 — Agent skills & published tech-docs site.
#
# One assertion per acceptance criterion in docs/plans/phase-29-skills-docs.md.
# A check against an unbuilt surface SKIPs rather than FAILs (the common.sh
# convention). The §19 drift hook itself is exercised by injecting a synthetic
# malformed-SKILL.md fixture and asserting the skillcheck CLI catches it.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-29 skills + tech-docs site"

# ---------------------------------------------------------------------------
# 1. Every named V1 skill exists at skills/<slug>/SKILL.md.
# ---------------------------------------------------------------------------
SKILLS=(
  scaffold-a-server
  add-a-tool
  attach-a-ui-resource
  define-contracts
  run-the-dev-loop
  validate
  package
  test-with-the-inspector
)
for s in "${SKILLS[@]}"; do
  if [ -f "skills/${s}/SKILL.md" ]; then
    ok "skill exists: ${s}"
  else
    fail "missing skill: skills/${s}/SKILL.md"
  fi
done

# ---------------------------------------------------------------------------
# 2. Every SKILL.md parses against the agentskills.io spec
#    (internal/skillcheck validator).
# ---------------------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  if go run ./internal/skillcheck/cmd/skillcheck skills >/tmp/dockyard-skillcheck.out 2>&1; then
    ok "skillcheck: skills/ tree is clean"
  else
    cat /tmp/dockyard-skillcheck.out | sed 's/^/  /'
    fail "skillcheck reported violations against skills/"
  fi
  rm -f /tmp/dockyard-skillcheck.out
else
  skip "skillcheck: 'go' not on PATH"
fi

# ---------------------------------------------------------------------------
# 3. The skillcheck CLI exits non-zero against the malformed-fixture set
#    (the validator's testdata directory).
# ---------------------------------------------------------------------------
if command -v go >/dev/null 2>&1; then
  if [ -d internal/skillcheck/testdata/invalid-name-uppercase ]; then
    if ! go run ./internal/skillcheck/cmd/skillcheck \
           internal/skillcheck/testdata/invalid-name-uppercase >/dev/null 2>&1; then
      ok "skillcheck rejects the malformed fixture (synthetic §19 drift case)"
    else
      fail "skillcheck did NOT reject the malformed fixture"
    fi
  else
    skip "skillcheck malformed fixture absent"
  fi
else
  skip "skillcheck CLI: 'go' not on PATH"
fi

# ---------------------------------------------------------------------------
# 4. The docs/site VitePress scaffold exists.
# ---------------------------------------------------------------------------
if [ -f docs/site/.vitepress/config.ts ]; then
  ok "docs/site/.vitepress/config.ts exists"
else
  fail "docs/site/.vitepress/config.ts missing"
fi
if [ -f docs/site/package.json ]; then
  ok "docs/site/package.json exists"
else
  fail "docs/site/package.json missing"
fi

# ---------------------------------------------------------------------------
# 5. The docs deploy workflow exists.
# ---------------------------------------------------------------------------
if [ -f .github/workflows/docs.yml ]; then
  ok ".github/workflows/docs.yml exists"
else
  fail ".github/workflows/docs.yml missing"
fi

# ---------------------------------------------------------------------------
# 6. Each shipped template has a getting-started walkthrough page.
# ---------------------------------------------------------------------------
for t in templates/*/; do
  name=$(basename "$t")
  [ "$name" = "_template" ] && continue
  [ -f "${t}builtin.go" ] || continue
  if [ -f "docs/site/getting-started/${name}.md" ]; then
    ok "docs walkthrough exists for template: ${name}"
  else
    fail "no docs/site/getting-started/${name}.md for template ${name}"
  fi
done

# ---------------------------------------------------------------------------
# 7. The CLI reference page exists and is non-trivial.
# ---------------------------------------------------------------------------
if [ -s docs/site/cli/index.md ]; then
  if grep -q "dockyard new" docs/site/cli/index.md \
     && grep -q "dockyard build" docs/site/cli/index.md \
     && grep -q "dockyard inspect" docs/site/cli/index.md; then
    ok "docs/site/cli/index.md references the core verbs"
  else
    fail "docs/site/cli/index.md is missing references to core verbs"
  fi
else
  fail "docs/site/cli/index.md is missing or empty"
fi

# ---------------------------------------------------------------------------
# 8. The agent-skills index page exists.
# ---------------------------------------------------------------------------
if [ -f docs/site/agent-skills/index.md ]; then
  ok "docs/site/agent-skills/index.md exists"
else
  fail "docs/site/agent-skills/index.md missing"
fi

# ---------------------------------------------------------------------------
# 9. The VitePress site builds (when npm + docs/site are present).
# ---------------------------------------------------------------------------
if command -v npm >/dev/null 2>&1 && [ -f docs/site/package.json ]; then
  if [ -d docs/site/node_modules ]; then
    if (cd docs/site && npm run build) >/tmp/dockyard-docs-build.out 2>&1; then
      ok "docs/site VitePress build is clean"
    else
      tail -30 /tmp/dockyard-docs-build.out | sed 's/^/  /'
      fail "docs/site VitePress build failed"
    fi
    rm -f /tmp/dockyard-docs-build.out
  else
    skip "docs/site build: node_modules absent (run 'make docs-install')"
  fi
else
  skip "docs/site build: npm or docs/site/package.json missing"
fi

# ---------------------------------------------------------------------------
# 10. The §19 drift-audit hook is wired (the script contains the §19 markers).
# ---------------------------------------------------------------------------
if grep -q "AGENTS.md §19" scripts/drift-audit.sh; then
  ok "scripts/drift-audit.sh carries the §19 hook"
else
  fail "scripts/drift-audit.sh missing the §19 hook"
fi

smoke_summary
