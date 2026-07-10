# Brief 08 — OAuth resource-server profile

**Date:** 2026-07-10
**Sources:** MCP draft authorization specification; RFC 8414, RFC 8707, RFC 9207,
and RFC 9728. Retrieved 2026-07-10.
**Status:** Informing V2 phase 36.

## 1. Findings

### 1.1 Dockyard is a protected resource, not an OAuth client

An HTTP Dockyard server advertises its authorization server through RFC 9728
Protected Resource Metadata and validates inbound bearer tokens. Harbor, as the
MCP client, performs authorization-server discovery, PKCE, client registration,
credential storage, callback validation, refresh, and scope accumulation.

**Implication:** Dockyard must never acquire, store, refresh, or forward a user
bearer token. The inspector remains test-only and does not become a production
OAuth client.

### 1.2 Resource metadata and access-token checks are server duties

An authorized MCP server serves RFC 9728 metadata with the canonical resource
identifier and authorization server issuer, returns a `401` Bearer challenge
containing `resource_metadata`, and returns `403 insufficient_scope` with every
scope needed for the current operation. It validates that each presented token is
issued for its own resource.

**Implication:** the configured canonical resource URL must be fixed and never
derived from a `Host` or forwarded header. Scope challenge state stays client-side.

### 1.3 RFC 8414 is trusted-issuer discovery, not arbitrary discovery

Authorization-server metadata exposes the JWKS URI and issuer. A JWT/JWKS
validator may retrieve it only from a configured issuer, validate exact issuer
equality, and refresh keys safely for rotation. It must not follow an issuer or
JWKS URL from an unvalidated token claim.

**Implication:** initial support is a CGo-free signed-JWT/JWKS driver behind an
interface/factory/driver seam. Opaque-token introspection is a later driver.

### 1.4 Stateless requests need a verified principal

Bearer validation yields immutable issuer, subject, audience/resource, expiry,
and scopes for one request. Tasks must bind records to that verified identity,
including task creation and all continuation operations. A raw authorization
header, an untrusted request `_meta`, or an MCP session cannot supply identity.

**Implication:** the runtime needs a typed principal context accessor, and the
Tasks engine must inherit it when a tool creates a task.

### 1.5 Refresh is not a protected-resource requirement

The draft explicitly says a protected resource should not advertise
`offline_access`. It is an authorization-server/client lifecycle concern.

**Implication:** Dockyard only advertises resource scopes. Harbor owns issuer-bound
registrations, RFC 9207 callback `iss` comparison, DCR `application_type`, refresh
storage, and step-up scope unions.

## 2. Sources

- MCP draft authorization: <https://modelcontextprotocol.io/specification/draft/basic/authorization>
- RFC 8414: <https://www.rfc-editor.org/rfc/rfc8414>
- RFC 8707: <https://www.rfc-editor.org/rfc/rfc8707>
- RFC 9207: <https://www.rfc-editor.org/rfc/rfc9207>
- RFC 9728: <https://www.rfc-editor.org/rfc/rfc9728>

## 3. Avoid

- Treating `TasksAuthContext` as bearer-token validation.
- Accepting a token intended for another resource or forwarding it upstream.
- Including `offline_access` in resource metadata or challenges.
- Persisting raw tokens, refresh tokens, or raw subjects in task state or obs.
