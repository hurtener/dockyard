# Phase 36 — Stateless OAuth resource server

## Summary

Make an HTTP Dockyard server an OAuth 2.1 protected resource after the stateless
transport migration. Dockyard publishes RFC 9728 metadata and validates a signed
JWT bearer token per request using a configured issuer's RFC 8414 metadata/JWKS;
Harbor remains responsible for every OAuth-client flow and credential lifecycle.

## RFC anchor

- RFC §15
- RFC §16
- RFC §19.1
- RFC §19.2

## Briefs informing this phase

- brief 08
- brief 07
- brief 03
- brief 02

## Brief findings incorporated

- **brief 08 §1.1:** Dockyard is a protected resource, never an OAuth client or
  authorization server.
- **brief 08 §1.2:** RFC 9728 metadata, Bearer challenges, and audience/resource
  validation are resource-server duties.
- **brief 08 §1.3:** issuer/JWKS discovery starts only from explicit trusted
  configuration, never a token claim.
- **brief 08 §1.4:** Tasks bind to a verified request principal, not a header or
  MCP session.

## Findings I'm departing from (if any)

None.

## Goals

- Publish standards-shaped resource metadata and validate signed bearer tokens
  without retaining OAuth or MCP session state.
- Make one verified principal available to tool handlers and all Tasks operations.
- Preserve the existing unauthenticated stdio and HTTP defaults unless an app
  explicitly enables resource-server configuration.

## Non-goals

- Authorization-code/PKCE flow, RFC 9207 callback validation, DCR, refresh-token
  storage, credential migration, or scope accumulation; Harbor owns these.
- OAuth client credentials, enterprise-managed authorization, or opaque-token
  introspection drivers.
- Advertising `offline_access` or forwarding an inbound access token upstream.

## Acceptance criteria

- [ ] Auth-enabled HTTP serves path-aware RFC 9728 metadata with one configured
      canonical resource URL, issuer, Bearer header support, and resource scopes.
- [ ] Missing, malformed, expired, bad-signature, wrong-algorithm, wrong-issuer,
      or wrong-audience tokens receive `401` with `resource_metadata`.
- [ ] A valid token lacking all required scopes receives `403` with
      `insufficient_scope`, every required operation scope, and resource metadata.
- [ ] The JWT/JWKS driver discovers metadata only from its configured issuer,
      handles bounded key rotation safely, and never logs/stores a token.
- [ ] Each modern and legacy HTTP request receives a verified principal through
      context; an untrusted `_meta` value cannot impersonate it.
- [ ] Task creation, get, update, cancel, and MRTR retries bind to the same
      verified issuer/subject identity and reject cross-principal access.
- [ ] A real local TLS authorization-server fixture, a real Dockyard server, and
      a test-only SDK client prove metadata → token → protected MCP call end to end.

## Files added or changed

- `runtime/authz/*`, `runtime/authz/jwtjwks/*`
- `runtime/server/http.go`, `server.go`, handler context tests
- `runtime/tasks/*`, authorization binding tests
- `internal/coveragecheck/coverage.json`
- `test/integration/phase36_oauth_resource_server_test.go`
- auth examples in `internal/scaffold/*` and `templates/*`
- `docs/site/*`, `skills/package/SKILL.md`, `skills/run-the-dev-loop/SKILL.md`,
  `skills/test-with-the-inspector/SKILL.md`
- `docs/plans/phase-36-oauth-resource-server.md`
- `scripts/smoke/phase-36.sh`

## Public API surface

```go
type Principal struct {
	Issuer, Subject, Resource string
	Scopes []string
}

func PrincipalFromContext(context.Context) (Principal, bool)

type Validator interface {
	Validate(context.Context, string) (Principal, error)
}
```

`HTTPOptions` gains typed authorization configuration. Driver selection follows
the repository's interface + factory + init-registration rule; handler-facing APIs
never expose JWT/JWKS wire types or a raw bearer string.

## Test plan

- **Unit:** metadata URL construction, RFC 8414 issuer validation, JWKS cache/key
  rotation, Authorization parsing, claims, challenge construction, principal context.
- **Integration:** TLS fixture issuer/JWKS plus real stateless and legacy Dockyard
  HTTP calls; task creation and MRTR continuation across independently routed requests.
- **Concurrency / golden:** validator/JWKS cache under `-race`, header/metadata
  goldens, fuzzed Authorization/JWT/config parse surfaces, validator benchmark.

## Smoke script additions

- Assert auth runtime packages, JWT driver registration, RFC 9728 integration
  test, Tasks-principal binding test, and no-token-logging test exist.

## Coverage target

- `runtime/authz`: 80% new-package band.
- `runtime/authz/jwtjwks`: 80% new-package band.
- `runtime/server`, `runtime/tasks`: 85% conformance band.

## Dependencies

- 31 — final spec/SDK foundation.
- 32 — stateless per-request HTTP context.
- 33 — modern Tasks/MRTR identity paths.
- 35 — test-only client and conformance tooling.

## Risks / open questions

- Canonical public resource URL requires explicit deployment configuration and
  HTTPS; it must not infer proxy headers.
- The JWT driver supports one issuer initially. Multiple issuer selection and
  opaque-token introspection are later drivers behind the unchanged seam.
- Task durable records must avoid raw subject/token data; choose a stable,
  collision-safe derived key in the implementation decision.

## Glossary additions

- Verified principal.
- Protected Resource Metadata.

## Pre-merge checklist

- [ ] `make drift-audit` passes
- [ ] `make check-mirror` passes
- [ ] `make preflight` passes
- [ ] `go test -race ./...` and `golangci-lint run` clean
- [ ] All cross-references (`RFC §X.Y`, `brief NN`) resolve
- [ ] Coverage on touched packages ≥ stated target
- [ ] New CLI command / manifest field / public API has a smoke check in this PR
- [ ] Reusable-artifact change ⇒ concurrent-reuse test under `-race`
- [ ] Cross-subsystem seam opened/consumed ⇒ integration test (AGENTS.md §17)
- [ ] New vocabulary added to `docs/glossary.md`
- [ ] New / changed architectural decision filed in `docs/decisions.md`
