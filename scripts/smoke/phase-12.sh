#!/usr/bin/env bash
# Smoke script for Phase 12 — host profiles + _meta.ui.domain derivation.
# Pluggable host profiles (interface + factory + driver) carry host-specific
# derivation functions; _meta.ui.domain is auto-derived, including Claude's
# SHA-256 signed claudemcpcontent.com origin. One assertion per acceptance
# criterion (docs/plans/phase-12-host-profiles.md).
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

# 4. The Claude profile is a driver file that self-registers via init().
if [ -f runtime/apps/hostprofile_claude.go ]; then
  if grep -q 'claudemcpcontent.com' runtime/apps/hostprofile_claude.go \
     && grep -q 'sha256' runtime/apps/hostprofile_claude.go \
     && grep -q 'func init()' runtime/apps/hostprofile_claude.go; then
    ok "claude driver derives a SHA-256 claudemcpcontent.com origin, init()-registered"
  else
    fail "claude driver missing SHA-256 derivation, the apex, or init() registration"
  fi
else
  skip "runtime/apps/hostprofile_claude.go not built"
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

# 6. _meta.ui.domain derivation is wired into the resource-read choke point.
if [ -f runtime/apps/apps.go ]; then
  if grep -q 'DerivedDomain' runtime/apps/apps.go; then
    ok "resourceMeta routes _meta.ui.domain through DerivedDomain (auto-derived)"
  else
    fail "apps.go does not route _meta.ui.domain through the derivation choke point"
  fi
else
  skip "runtime/apps/apps.go not built — derivation-wiring check deferred"
fi

# 7. The phase-12 host-profile + domain-derivation tests pass under -race.
if [ -f runtime/apps/hostprofile_test.go ]; then
  if CGO_ENABLED=1 go test -race -count=1 \
       -run 'HostProfile|Claude|Generic|DerivedDomain|ResourceMeta_Domain' \
       ./runtime/apps/... >/dev/null 2>&1; then
    ok "phase-12 host-profile + domain-derivation tests pass under -race"
  else
    fail "phase-12 host-profile / domain-derivation tests failed"
  fi
else
  skip "phase-12 host-profile tests not built"
fi

smoke_summary
