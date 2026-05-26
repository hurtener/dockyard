#!/usr/bin/env bash
# Smoke script for v1.1 Wave A — inspector polish.
# Closes V2-backlog items D-101 (dockyard dev auto-attaches the inspector)
# and D-151 (the inspector Prompts panel).
# A check against an unbuilt surface SKIPs; OK >= acceptance criteria and
# FAIL = 0 is the done bar (CLAUDE.md §4.2).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.1 wave A — inspector polish"

# 1. Item 1 — dev auto-attach + --no-inspector opt-out flag.
if [ -f internal/cli/dev.go ]; then
  if grep -q 'no-inspector' internal/cli/dev.go; then
    ok "dockyard dev gained the --no-inspector flag"
  else
    fail "dockyard dev is missing the --no-inspector flag"
  fi
else
  skip "internal/cli/dev.go missing — devloop CLI not yet wired"
fi

# 2. Item 1 — the devloop carries the inspector child wiring.
if [ -f internal/devloop/inspector.go ]; then
  ok "internal/devloop/inspector.go exists (the supervised inspector child)"
else
  fail "internal/devloop/inspector.go missing — auto-attach seam not wired"
fi

if [ -f internal/devloop/devloop.go ]; then
  if grep -q 'inspectorChild' internal/devloop/devloop.go; then
    ok "devloop orchestrator references inspectorChild"
  else
    fail "devloop orchestrator does not wire inspectorChild"
  fi
  if grep -q 'DisableInspector' internal/devloop/devloop.go; then
    ok "devloop Config gained DisableInspector"
  else
    fail "devloop Config missing DisableInspector"
  fi
fi

# 3. Item 2 — inspector Go-side Prompts surface.
if [ -f internal/inspector/prompts.go ]; then
  ok "internal/inspector/prompts.go exists (PromptSource + PromptInvoker)"
else
  fail "internal/inspector/prompts.go missing — Prompts surface not wired"
fi

if [ -f internal/inspector/assets.go ]; then
  if grep -q 'POST /api/prompts/get' internal/inspector/assets.go; then
    ok "POST /api/prompts/get endpoint exists in internal/inspector"
  else
    fail "POST /api/prompts/get endpoint missing from internal/inspector/assets.go"
  fi
  if grep -q 'GET /api/prompts' internal/inspector/assets.go; then
    ok "GET /api/prompts endpoint exists in internal/inspector"
  else
    fail "GET /api/prompts endpoint missing from internal/inspector/assets.go"
  fi
fi

if [ -f internal/inspector/inspector.go ]; then
  if grep -q 'PromptInvoker' internal/inspector/inspector.go; then
    ok "inspector.Options carries PromptInvoker"
  else
    fail "inspector.Options is missing PromptInvoker"
  fi
fi

# 4. Item 2 — inspector frontend Prompts panel.
if [ -f web/inspector/src/lib/PromptsPanel.svelte ]; then
  ok "PromptsPanel.svelte exists"
else
  fail "PromptsPanel.svelte missing from web/inspector/src/lib/"
fi

if [ -f web/inspector/src/lib/prompts.ts ]; then
  ok "web/inspector/src/lib/prompts.ts (typed prompt model) exists"
else
  fail "web/inspector/src/lib/prompts.ts missing"
fi

if [ -f web/inspector/src/lib/api.ts ]; then
  if grep -q 'invokePrompt' web/inspector/src/lib/api.ts; then
    ok "api.ts exposes invokePrompt"
  else
    fail "api.ts is missing the invokePrompt client"
  fi
fi

if [ -f web/inspector/src/App.svelte ]; then
  if grep -q 'PromptsPanel' web/inspector/src/App.svelte; then
    ok "App.svelte composes the PromptsPanel rail tab"
  else
    fail "App.svelte does not render PromptsPanel"
  fi
fi

# 5. The Phase 27 inspector mcp.NewClient allow-list gained the
#    prompts.go entry (D-163 extends the audit).
if [ -f test/integration/phase27_inspector_security_test.go ]; then
  if grep -q 'internal/inspector/prompts.go' test/integration/phase27_inspector_security_test.go; then
    ok "Phase 27 inspector security audit allow-list includes prompts.go (D-163)"
  else
    fail "Phase 27 audit allow-list missing the prompts.go entry — D-163 not honored"
  fi
fi

# 6. The decisions log + the V2 backlog reflect the two closures.
if [ -f docs/decisions.md ]; then
  for d in D-161 D-162 D-163; do
    if grep -q "^## ${d}\b" docs/decisions.md; then
      ok "docs/decisions.md carries ${d}"
    else
      fail "docs/decisions.md missing ${d}"
    fi
  done
fi

if [ -f docs/V2-BACKLOG.md ]; then
  if grep -qi 'Closed in v1.1' docs/V2-BACKLOG.md; then
    ok "docs/V2-BACKLOG.md marks v1.1 closures"
  else
    skip "docs/V2-BACKLOG.md not yet annotated for v1.1 closures"
  fi
fi

# 7. Affected skills + docs site pages reference the new shape (§19).
if [ -f skills/run-the-dev-loop/SKILL.md ]; then
  if grep -q 'no-inspector\|auto-attach' skills/run-the-dev-loop/SKILL.md; then
    ok "run-the-dev-loop skill mentions inspector auto-attach"
  else
    fail "run-the-dev-loop skill does not mention auto-attach / --no-inspector"
  fi
fi

if [ -f skills/test-with-the-inspector/SKILL.md ]; then
  if grep -qi 'Prompts panel\|prompts/get\|/api/prompts' skills/test-with-the-inspector/SKILL.md; then
    ok "test-with-the-inspector skill mentions the Prompts panel"
  else
    fail "test-with-the-inspector skill does not mention the Prompts panel"
  fi
fi

if [ -f docs/site/guides/dev-loop.md ]; then
  if grep -q 'no-inspector\|auto-attach' docs/site/guides/dev-loop.md; then
    ok "docs-site dev-loop guide mentions auto-attach"
  else
    fail "docs-site dev-loop guide does not mention auto-attach / --no-inspector"
  fi
fi

if [ -f docs/site/guides/inspector.md ]; then
  if grep -qi 'Prompts' docs/site/guides/inspector.md; then
    ok "docs-site inspector guide mentions the Prompts panel"
  else
    fail "docs-site inspector guide does not mention the Prompts panel"
  fi
fi

smoke_summary
