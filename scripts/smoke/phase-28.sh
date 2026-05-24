#!/usr/bin/env bash
# Smoke script for Phase 28 — examples, godoc, docs hygiene.
# One assertion per acceptance criterion (docs/plans/phase-28-examples-godoc-hygiene.md).
# A check against an unbuilt surface skip()s, never fail()s — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-28 examples + godoc + docs hygiene"

# --- the three worked examples ----------------------------------------------

for ex in backend-tools-only combined-patterns prompts-demo; do
  if [ -d "examples/${ex}" ]; then
    ok "examples/${ex}/ exists"
  else
    fail "examples/${ex}/ missing"
    continue
  fi
  if [ -f "examples/${ex}/dockyard.app.yaml" ]; then
    ok "examples/${ex}/dockyard.app.yaml present"
  else
    fail "examples/${ex}/dockyard.app.yaml missing"
  fi
  if [ -f "examples/${ex}/cmd/server/main.go" ]; then
    ok "examples/${ex}/cmd/server/main.go present"
  else
    fail "examples/${ex}/cmd/server/main.go missing"
  fi
  if [ -f "examples/${ex}/README.md" ]; then
    ok "examples/${ex}/README.md present"
  else
    fail "examples/${ex}/README.md missing"
  fi
done

# Each example builds against the current runtime (the same Go module).
if command -v go >/dev/null 2>&1; then
  if go build ./examples/backend-tools-only/... >/dev/null 2>&1; then
    ok "examples/backend-tools-only compiles"
  else
    fail "examples/backend-tools-only does not compile"
  fi
  if go build ./examples/combined-patterns/... >/dev/null 2>&1; then
    ok "examples/combined-patterns compiles"
  else
    fail "examples/combined-patterns does not compile"
  fi
  if go build ./examples/prompts-demo/... >/dev/null 2>&1; then
    ok "examples/prompts-demo compiles"
  else
    fail "examples/prompts-demo does not compile"
  fi
else
  skip "go not on PATH — example compile checks skipped"
fi

# --- the prompts API --------------------------------------------------------

if grep -qsE '^func AddPrompt\b' runtime/server/prompt.go; then
  ok "runtime/server.AddPrompt exists"
else
  fail "runtime/server.AddPrompt missing"
fi

if grep -qsE 'KindPromptGet[[:space:]]+EventKind' runtime/obs/event.go; then
  ok "obs.KindPromptGet exists"
else
  fail "obs.KindPromptGet missing"
fi

if grep -qsE 'type PromptGetPayload\b' runtime/obs/payload.go; then
  ok "obs.PromptGetPayload exists"
else
  fail "obs.PromptGetPayload missing"
fi

if grep -qsE '^func .*\) PromptGet\(' runtime/obs/recorder.go; then
  ok "Recorder.PromptGet exists"
else
  fail "Recorder.PromptGet missing"
fi

if [ -f runtime/server/prompt_test.go ] && grep -qsE '^func TestAddPrompt' runtime/server/prompt_test.go; then
  ok "runtime/server/prompt_test.go has TestAddPrompt* tests"
else
  fail "runtime/server/prompt_test.go has no TestAddPrompt* tests"
fi

# --- godoc Example functions ------------------------------------------------

if grep -qsE '^func Example[A-Za-z_]*\(' runtime/server/example_test.go 2>/dev/null; then
  ok "runtime/server has Example test(s) for pkg.go.dev"
else
  fail "runtime/server has no Example test for pkg.go.dev"
fi
if grep -qsE '^func Example[A-Za-z_]*\(' runtime/tool/example_test.go 2>/dev/null; then
  ok "runtime/tool has Example test(s) for pkg.go.dev"
else
  fail "runtime/tool has no Example test for pkg.go.dev"
fi

# --- D-144 read-only → operator-initiated framing ---------------------------

# The CLI source's `Long` help and docstring must no longer carry the
# unconditional "read-only" framing.
if grep -qsE 'read-only' internal/cli/inspect.go; then
  fail "internal/cli/inspect.go still carries 'read-only' framing — should be 'operator-initiated only' (D-144)"
else
  ok "internal/cli/inspect.go no longer carries the unconditional 'read-only' inspector framing"
fi

# The docs guide should mention D-144 (or operator-initiated).
if grep -qsE 'operator-initiated|D-144' docs/site/guides/inspector.md 2>/dev/null; then
  ok "docs/site/guides/inspector.md cites D-144 / operator-initiated"
else
  fail "docs/site/guides/inspector.md does not cite D-144"
fi
if grep -qsE 'operator-initiated|D-144' skills/test-with-the-inspector/SKILL.md 2>/dev/null; then
  ok "skills/test-with-the-inspector/SKILL.md cites D-144 / operator-initiated"
else
  fail "skills/test-with-the-inspector/SKILL.md does not cite D-144"
fi

# --- D-139 pre-publish workflow in templates --------------------------------

for tpl in analytics-widgets approval-flows; do
  if grep -qsE 'go mod tidy' "templates/${tpl}/README.md.tmpl" 2>/dev/null; then
    ok "templates/${tpl}/README.md.tmpl mentions 'go mod tidy' (D-139)"
  else
    fail "templates/${tpl}/README.md.tmpl missing 'go mod tidy' (D-139)"
  fi
done

# --- docs site examples index page ------------------------------------------

if [ -f docs/site/getting-started/examples.md ]; then
  ok "docs/site/getting-started/examples.md exists"
else
  fail "docs/site/getting-started/examples.md missing"
fi

# --- §19 hook extension to examples/ ----------------------------------------

if grep -qsE '6d\. Every shipped example' scripts/drift-audit.sh; then
  ok "scripts/drift-audit.sh §19 hook covers examples/"
else
  fail "scripts/drift-audit.sh §19 hook does not cover examples/"
fi

# --- the in-tree contract tests still pass ----------------------------------

if command -v go >/dev/null 2>&1; then
  if CGO_ENABLED=1 go test -race ./examples/... >/dev/null 2>&1; then
    ok "examples/... tests pass under -race"
  else
    fail "examples/... tests do not pass under -race"
  fi
  if CGO_ENABLED=1 go test -race -run "TestAddPrompt" ./runtime/server/ >/dev/null 2>&1; then
    ok "runtime/server TestAddPrompt tests pass under -race"
  else
    fail "runtime/server TestAddPrompt tests fail"
  fi
else
  skip "go not on PATH — test runs skipped"
fi

smoke_summary
