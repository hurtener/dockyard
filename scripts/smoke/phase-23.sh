#!/usr/bin/env bash
# Smoke script for Phase 23 — Inspector advanced + `dockyard inspect`.
# One assertion per acceptance criterion (master plan / RFC §12).
# A check against an unbuilt surface skips(), never fails() — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-23 inspector advanced"

# 1. The fixture switcher exists with the six fixtures, wired to the generated
#    contracts (RFC §12, §6 — P1).
FIX=web/inspector/src/lib/fixtures.ts
if [ -f "$FIX" ]; then
  if grep -q "'happy'" "$FIX" && grep -q "'empty'" "$FIX" \
     && grep -q "'error'" "$FIX" && grep -q "'permission'" "$FIX" \
     && grep -q "'slow'" "$FIX" && grep -q "'large'" "$FIX" \
     && grep -q "ToolContract" "$FIX"; then
    ok "fixture switcher has the six fixtures wired to generated contracts"
  else
    fail "fixtures.ts missing a fixture kind or the contract wiring"
  fi
else
  skip "web/inspector fixture switcher not built"
fi
if [ -f web/inspector/src/lib/FixturesPanel.svelte ]; then
  ok "the Fixtures DetailRail panel exists"
else
  skip "Fixtures panel not built"
fi

# 2. The Verdicts panel + backend endpoint exist (contract-drift /
#    schema-validation / spec-compliance — RFC §12).
if [ -f internal/inspector/verdicts.go ]; then
  if grep -q "VerdictsFromValidate" internal/inspector/verdicts.go \
     && grep -q "validate.Run" internal/inspector/verdicts.go; then
    ok "the Verdicts backend reuses internal/validate.Run"
  else
    fail "verdicts.go does not reuse the validate engine"
  fi
else
  skip "internal/inspector verdicts backend not built"
fi
if [ -f web/inspector/src/lib/VerdictsPanel.svelte ]; then
  ok "the Verdicts DetailRail panel exists"
else
  skip "Verdicts panel not built"
fi

# 3. Capability-set emulation exists and is a TOGGLE SET — never a hardcoded
#    per-host capability matrix (CLAUDE.md §6 / §13).
CAP=web/inspector/src/lib/capability.ts
if [ -f "$CAP" ]; then
  if grep -q "CapabilitySet" "$CAP" \
     && grep -q "hostContextFor" "$CAP" \
     && grep -q "hostCapabilitiesFor" "$CAP"; then
    ok "capability-set emulation exists as a capability toggle set"
  else
    fail "capability.ts missing the toggle-set model"
  fi
  # The negation: no hardcoded per-host capability matrix. A matrix would key
  # capabilities on host names; assert no such keyed table exists.
  if grep -Eqi "hostMatrix|HOST_MATRIX|capabilityMatrix|chatgpt.*:.*true|claude.*:.*\{" "$CAP"; then
    fail "capability.ts looks like a hardcoded per-host matrix (§6/§13)"
  else
    ok "no hardcoded per-host capability matrix (§6/§13)"
  fi
else
  skip "capability-set emulation not built"
fi
if [ -f web/inspector/src/lib/HostControl.svelte ]; then
  ok "the Host capability-set control exists"
else
  skip "Host control not built"
fi

# 4. The Tasks panel exists (task-lifecycle + input_required — RFC §8.6).
if [ -f web/inspector/src/lib/TasksPanel.svelte ] \
   && [ -f web/inspector/src/lib/tasks.ts ]; then
  if grep -q "input_required" web/inspector/src/lib/tasks.ts; then
    ok "the Tasks DetailRail panel renders the lifecycle + input_required"
  else
    fail "tasks.ts missing the input_required lifecycle"
  fi
else
  skip "Tasks panel not built"
fi

# 5. The Tools/Resources panel exists.
if [ -f web/inspector/src/lib/ToolsPanel.svelte ]; then
  ok "the Tools/Resources DetailRail panel exists"
else
  skip "Tools/Resources panel not built"
fi

# 6. `dockyard inspect` is registered with --url / --dir / --port / --no-open.
if [ -f internal/cli/inspect.go ]; then
  if grep -q '"url"' internal/cli/inspect.go \
     && grep -q '"dir"' internal/cli/inspect.go \
     && grep -q '"port"' internal/cli/inspect.go \
     && grep -q '"no-open"' internal/cli/inspect.go; then
    ok "dockyard inspect is wired with --url / --dir / --port / --no-open"
  else
    fail "internal/cli/inspect.go missing a flag"
  fi
  if grep -q "newInspectCmd" internal/cli/root.go; then
    ok "dockyard inspect is registered on the root command"
  else
    fail "dockyard inspect is not registered in root.go"
  fi
  # Remediation R1: the shipping `dockyard inspect` MUST wire the Verdicts,
  # Contracts, and App-preview sources — a depth audit found `runInspect` set
  # only Addr/Relay/Assets, leaving those panels permanently empty in the
  # product. Assert runInspect wires all three.
  if grep -q "VerdictsFromValidate" internal/cli/inspect.go \
     && grep -q "ContractsFromProject" internal/cli/inspect.go \
     && grep -q "AppsFromServer" internal/cli/inspect.go; then
    ok "dockyard inspect wires Verdicts, Contracts, and the App-preview source (R1)"
  else
    fail "internal/cli/inspect.go does not wire the inspector sources (R1 Blockers 1/2)"
  fi
else
  skip "dockyard inspect command not built"
fi

# 7. `dockyard inspect --help` works against a built binary — the command
#    attaches to the CLI surface.
if [ -x bin/dockyard ]; then
  if bin/dockyard inspect --help >/dev/null 2>&1; then
    ok "dockyard inspect --help runs"
  else
    fail "dockyard inspect --help failed"
  fi
else
  skip "bin/dockyard not built — dockyard inspect runtime check deferred"
fi

# 8. The web/inspector frontend gate passes (type-check + unit tests + coverage).
if [ -f web/inspector/package.json ]; then
  if ! command -v npm >/dev/null 2>&1; then
    skip "npm not installed — web/inspector frontend gate deferred"
  else
    if ( cd web/inspector && \
         { [ -d node_modules ] || npm ci --no-audit --no-fund >/dev/null 2>&1; } && \
         npm run gate >/dev/null 2>&1 ); then
      ok "web/inspector type-check + unit tests + coverage pass"
    else
      fail "web/inspector frontend gate failed"
    fi
  fi
else
  skip "web/inspector not built — frontend gate deferred"
fi

smoke_summary
