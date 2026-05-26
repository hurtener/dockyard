#!/usr/bin/env bash
# Smoke script for v1.1 wave B — runtime cleanups.
#   D-164 — scaffold + dockyard run auto-wire of tasks.Engine
#   D-165 — Claude signed-origin testgate cleanup (retires the synthetic-URL workaround)
#
# A check against an unbuilt surface skips, never fails — same discipline as
# the per-phase smoke scripts (CLAUDE.md §4.1, scripts/smoke/common.sh).
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: v1.1-wave-B runtime cleanups"

# --- D-164 — scaffold auto-wire of tasks.Engine -----------------------------

# 1. The detection helper exists.
if [ -f internal/scaffold/manifest_detect.go ] \
   && grep -q "RequiresTasksEngine" internal/scaffold/manifest_detect.go; then
  ok "internal/scaffold.RequiresTasksEngine helper exists"
else
  fail "internal/scaffold/manifest_detect.go missing or RequiresTasksEngine absent"
fi

# 2. The engine-wired renderer branch exists.
if grep -q "renderMainGoWithTasks" internal/scaffold/templates.go; then
  ok "renderMainGoWithTasks branch is present in templates.go"
else
  fail "renderMainGoWithTasks branch missing — auto-wire path not implemented"
fi

# 3. The Options field that opts the scaffold's example tool into task_support exists.
if grep -q "ExampleToolTaskSupport" internal/scaffold/scaffold.go; then
  ok "Options.ExampleToolTaskSupport field exists"
else
  fail "Options.ExampleToolTaskSupport field missing"
fi

# 4. The dockyard run audit emits a warning when the manifest declares task
#    support but main.go does not appear to wire the engine.
if grep -q "auditAutoWire" internal/runpkg/run.go \
   && grep -q "mainGoWiresTasks" internal/runpkg/run.go; then
  ok "dockyard run audit (auditAutoWire + mainGoWiresTasks) is wired"
else
  fail "dockyard run is missing the D-164 manifest-vs-main.go audit"
fi

# 5. The detection helper is unit-tested.
if [ -f internal/scaffold/manifest_detect_test.go ] \
   && grep -q "TestRequiresTasksEngine" internal/scaffold/manifest_detect_test.go; then
  ok "TestRequiresTasksEngine is present"
else
  fail "TestRequiresTasksEngine missing — detection helper is untested"
fi

# 6. The scaffold's main.go renderer is unit-tested with both branches.
if grep -q "TestScaffoldMainGo_AutoWiresTasksEngine" internal/scaffold/scaffold_test.go \
   && grep -q "TestGenerate_AutoWireEndToEnd" internal/scaffold/scaffold_test.go; then
  ok "renderMainGo auto-wire test coverage is present"
else
  fail "renderMainGo auto-wire tests missing"
fi

# 7. The audit is unit-tested.
if grep -q "TestMainGoWiresTasks_Heuristic" internal/runpkg/run_test.go \
   && grep -q "TestAuditAutoWire_WarnsOnUnwiredTaskSupport" internal/runpkg/run_test.go; then
  ok "dockyard run audit unit tests present"
else
  fail "dockyard run audit unit tests missing"
fi

# 8. The approval-flows template still wires the engine — the auto-wire
#    generalises the same shape it has always used.
if grep -q "tasks.NewEngine" templates/approval-flows/main.go.tmpl \
   && grep -q "Tasks: engine" templates/approval-flows/main.go.tmpl; then
  ok "approval-flows template continues to wire tasks.Engine"
else
  fail "approval-flows template no longer wires tasks.Engine — regression"
fi

# 9. The analytics-widgets template does NOT wire the engine — every tool
#    declares task_support: forbidden so the auto-wire stays off.
if grep -q "tasks.NewEngine\|tasks.NewInMemoryStore" templates/analytics-widgets/main.go.tmpl; then
  fail "analytics-widgets template wires a tasks.Engine it does not need — regression"
else
  ok "analytics-widgets template stays engine-free (no task-supporting tools)"
fi

# --- D-165 — Claude signed-origin testgate cleanup --------------------------

# 10. The synthetic-URL workaround is retired from the capability category.
if grep -q "capability-test.example" internal/testgate/categories.go; then
  fail "internal/testgate/categories.go still references the retired synthetic URL"
else
  ok "internal/testgate/categories.go no longer carries the synthetic-URL workaround"
fi

# 11. The new HostProfile.RequiresServerURL method exists on the interface.
if grep -q "RequiresServerURL() bool" runtime/apps/hostprofile.go; then
  ok "HostProfile.RequiresServerURL is declared on the interface"
else
  fail "HostProfile.RequiresServerURL method missing from the interface"
fi

# 12. Both shipped profiles implement RequiresServerURL.
if grep -q "RequiresServerURL" runtime/apps/hostprofile_generic.go \
   && grep -q "RequiresServerURL" runtime/apps/hostprofile_claude.go; then
  ok "generic + claude profiles implement RequiresServerURL"
else
  fail "one or both shipped host profiles do not implement RequiresServerURL"
fi

# 13. The capability category consults RequiresServerURL.
if grep -q "profile.RequiresServerURL()" internal/testgate/categories.go; then
  ok "runCapability consults profile.RequiresServerURL"
else
  fail "runCapability does not consult RequiresServerURL — gate still relies on a placeholder"
fi

# 14. A focused unit test asserts the new method's behaviour per profile.
if grep -q "TestHostProfile_RequiresServerURL" runtime/apps/hostprofile_test.go; then
  ok "TestHostProfile_RequiresServerURL is present"
else
  fail "TestHostProfile_RequiresServerURL missing — new method untested"
fi

# 15. A capability-category test asserts the no-synthetic-URL behaviour.
if grep -q "TestRunCapability_UIAppPassesWithoutSyntheticURL" internal/testgate/testgate_test.go; then
  ok "TestRunCapability_UIAppPassesWithoutSyntheticURL is present"
else
  fail "capability-category test for the retired workaround missing"
fi

# --- preflight discovery ----------------------------------------------------

# 16. preflight discovers vN.N-wave-*.sh smoke scripts in addition to phase-*.sh.
if grep -qE 'v\[0-9\]\*-wave-\*\.sh|v1\.1-wave-' scripts/preflight.sh; then
  ok "scripts/preflight.sh discovers vN.N-wave-*.sh smoke scripts"
else
  fail "scripts/preflight.sh does not yet discover vN.N-wave-*.sh"
fi

# --- decisions log ----------------------------------------------------------

# 17. The decisions log carries D-164 and D-165 in ascending order.
if grep -q "^## D-164 " docs/decisions.md && grep -q "^## D-165 " docs/decisions.md; then
  ok "decisions log carries D-164 and D-165"
else
  fail "decisions log missing D-164 or D-165"
fi

smoke_summary
