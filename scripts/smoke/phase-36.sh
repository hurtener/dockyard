#!/usr/bin/env bash
# Smoke script for Phase 36 — stateless OAuth resource server.
set -uo pipefail
cd "$(dirname "$0")/../.."
. scripts/smoke/common.sh

echo "smoke: phase-36 stateless OAuth resource server"

run_focused_test() {
  label=$1
  package=$2
  pattern=$3
  if output=$(go test -race "$package" -run "$pattern" -count=1 2>&1); then
    ok "$label"
  else
    fail "$label"
    printf '%s\n' "$output"
  fi
}

if [ -f runtime/authz/config.go ] && [ -f runtime/authz/jwtjwks/validator.go ]; then ok "authorization packages exist"; else fail "authorization packages missing"; fi
run_focused_test "JWT/JWKS driver registers" ./runtime/authz/jwtjwks '^TestDriverRegistered$'
run_focused_test "metadata and bearer challenges are standards-shaped" ./runtime/server '^TestHTTPAuthorizationMetadataChallengesAndPrincipal$'
run_focused_test "modern and legacy requests receive verified context" ./test/integration '^TestPhase36OAuthResourceServerEndToEnd$'
run_focused_test "Tasks and MRTR continuations reject cross-principal access" ./runtime/server '^(TestAuthenticatedContinuationRejectsCrossPrincipalAndTampering|TestVerifiedTaskIdentityOverridesLegacyCallback)$'
run_focused_test "real TLS issuer and protected server work end to end" ./test/integration '^TestPhase36OAuthResourceServerEndToEnd$'
run_focused_test "validator errors never leak bearer tokens" ./runtime/authz/jwtjwks '^(TestValidateExactClaimsAndNoTokenLeak|TestErrorsNeverContainToken)$'
run_focused_test "blank scaffold pins opt-in OAuth documentation" ./internal/scaffold '^(TestGolden|TestGoldenDocumentsOptInOAuthWithoutSecrets)$'
run_focused_test "built-in templates carry opt-in OAuth documentation" ./templates/... '^TestBuiltin_TemplateShape$'

if [ -f examples/oauth-resource-server/README.md ] &&
   grep -q 'DOCKYARD_OAUTH_RESOURCE' examples/oauth-resource-server/README.md &&
   grep -q 'DOCKYARD_OAUTH_CONTINUATION_KEY' examples/oauth-resource-server/README.md; then
  ok "environment-driven OAuth example is present"
else
  fail "environment-driven OAuth example is missing"
fi

if grep -Eq 'Phase [0-9]|phase-[0-9]|D-[0-9]{3}' examples/oauth-resource-server/README.md templates/*/README.md.tmpl internal/scaffold/testdata/golden/README.md.golden; then
  fail "OAuth user documentation contains contributor terminology"
else
  ok "OAuth user documentation excludes contributor terminology"
fi

if [ "$SMOKE_OK" -lt 10 ]; then
  fail "acceptance criteria require at least 10 OK checks (got $SMOKE_OK)"
fi

smoke_summary
