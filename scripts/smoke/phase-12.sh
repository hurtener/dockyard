#!/usr/bin/env bash
# Smoke script for Phase 12 — host profiles + _meta.ui.domain.
# The pluggable host-profile seam (interface + factory + driver) is retained as
# the extensibility point for a future host-blessed origin transform. As of
# D-176 (v1.6 wave A) _meta.ui.domain is HOST-SUPPLIED VERBATIM — the
# synthesising Claude profile (D-062/D-063) is retired and the seam ships only
# the generic verbatim profile. One assertion per acceptance criterion
# (docs/plans/phase-12-host-profiles.md, as amended by D-176).
# A check against an unbuilt surface skips rather than fails — see common.sh.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-12 host-profiles"

# 1. The host-profile seam exists in runtime/apps.
if [ -f runtime/apps/hostprofile.go ]; then
  ok "runtime/apps/hostprofile.go exists"
else
  skip "runtime/apps/hostprofile.go not built"
fi

# 2. runtime/apps still builds CGo-free (no-CGo runtime guarantee, AGENTS.md §13).
if [ -f runtime/apps/hostprofile.go ]; then
  if CGO_ENABLED=0 go build ./runtime/apps/... >/dev/null 2>&1; then
    ok "runtime/apps builds CGo-free with the host-profile seam"
  else
    fail "runtime/apps does not build with CGO_ENABLED=0"
  fi
else
  skip "host-profile seam not built — build check deferred"
fi

# 3. The seam is interface + factory + driver (AGENTS.md §4.4): a HostProfile
#    interface and a RegisterHostProfile factory entrypoint.
if [ -f runtime/apps/hostprofile.go ]; then
  if grep -q 'type HostProfile interface' runtime/apps/hostprofile.go \
     && grep -q 'func RegisterHostProfile' runtime/apps/hostprofile.go; then
    ok "host-profile seam exposes HostProfile interface + RegisterHostProfile factory"
  else
    fail "host-profile seam missing the interface or the factory entrypoint"
  fi
else
  skip "host-profile seam not built — seam-shape check deferred"
fi

# 4. The generic verbatim profile is a driver file that self-registers via
#    init(); the synthesising Claude profile is retired (D-176).
if [ -f runtime/apps/hostprofile_generic.go ]; then
  if grep -q 'func init()' runtime/apps/hostprofile_generic.go \
     && grep -q 'RegisterHostProfile' runtime/apps/hostprofile_generic.go; then
    ok "generic verbatim profile is init()-registered behind the seam"
  else
    fail "generic profile missing init() registration"
  fi
else
  skip "runtime/apps/hostprofile_generic.go not built"
fi

# 4b. No production driver synthesises a host's signed origin — the derivation
#     is retired and _meta.ui.domain is host-supplied verbatim (D-176). (Test
#     files legitimately use such an origin as a verbatim example value, so the
#     scan is non-test production sources only.)
if [ -f runtime/apps/hostprofile.go ]; then
  synth_hits=$(grep -l 'claudemcpcontent.com\|sha256' runtime/apps/*.go 2>/dev/null | grep -v '_test.go' || true)
  if [ ! -f runtime/apps/hostprofile_claude.go ] && [ -z "$synth_hits" ]; then
    ok "the synthesising Claude profile is retired — no driver mints a signed origin"
  else
    fail "a host-profile driver still synthesises a signed origin (D-176 retires it)"
  fi
else
  skip "host-profile seam not built — synthesis-retired check deferred"
fi

# 5. The Apps core (apps.go) contains no hardcoded host name — host-specific
#    code lives only in driver files behind the seam (D-011, AGENTS.md §6).
if [ -f runtime/apps/apps.go ]; then
  if ! grep -Eq 'claudemcpcontent|"claude"' runtime/apps/apps.go; then
    ok "apps.go has no hardcoded host name — host code stays behind the seam"
  else
    fail "apps.go hardcodes a host name (D-011 / AGENTS.md §6 violation)"
  fi
else
  skip "runtime/apps/apps.go not built"
fi

# 6. _meta.ui.domain is carried VERBATIM at the resource-read choke point —
#    resourceMeta passes App.Domain straight to the codec, no derivation (D-176).
if [ -f runtime/apps/apps.go ]; then
  if grep -q 'Domain:        a.Domain' runtime/apps/apps.go \
     && ! grep -q 'DerivedDomain' runtime/apps/apps.go; then
    ok "resourceMeta carries _meta.ui.domain verbatim (App.Domain, no derivation)"
  else
    fail "apps.go does not carry _meta.ui.domain verbatim (D-176)"
  fi
else
  skip "runtime/apps/apps.go not built — verbatim-domain check deferred"
fi

# 7. The phase-12 host-profile + verbatim-domain tests pass under -race.
if [ -f runtime/apps/hostprofile_test.go ]; then
  if CGO_ENABLED=1 go test -race -count=1 \
       -run 'HostProfile|Generic|Signing|DerivedDomain|ResourceMeta_Domain' \
       ./runtime/apps/... >/dev/null 2>&1; then
    ok "phase-12 host-profile + verbatim-domain tests pass under -race"
  else
    fail "phase-12 host-profile / verbatim-domain tests failed"
  fi
else
  skip "phase-12 host-profile tests not built"
fi

smoke_summary
