# OAuth protected resource

Dockyard can make an HTTP MCP endpoint an OAuth protected resource. Dockyard
publishes RFC 9728 metadata and validates bearer tokens; it does not run an
authorization server or OAuth client flow. Harbor owns authorization-code/PKCE,
token acquisition, refresh, and credential storage.

## Configure the HTTP handler

Configure authorization on `server.HTTPOptions`. The resource and issuer must
be canonical absolute HTTPS URLs. The resource is the public MCP endpoint and
must exactly match the JWT audience; do not derive it from proxy headers.

```go
import (
	"os"
	"time"

	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/authz/jwtjwks"
	"github.com/hurtener/dockyard/runtime/server"
)

continuationKey := []byte(os.Getenv("DOCKYARD_CONTINUATION_KEY"))

handler, err := srv.HTTPHandler(&server.HTTPOptions{
	ProtocolMode: server.Dual,
	Security:     server.DefaultHTTPSecurity(),
	Authorization: &authz.Config{
		Driver:          jwtjwks.DriverName,
		Resource:        "https://mcp.example.com/mcp",
		Issuer:          "https://identity.example.com/tenant",
		Scopes:          []string{"mcp:read", "mcp:write", "mcp:admin"},
		RequiredScopes:  []string{"mcp:read", "mcp:write"},
		ContinuationKey: continuationKey,
		DriverConfig: jwtjwks.Config{
			AllowedAlgorithms: []string{"RS256"},
			CacheTTL:          15 * time.Minute,
			RefreshCooldown:   5 * time.Second,
		},
	},
})
if err != nil {
	return err
}
```

Importing `jwtjwks` for its typed configuration also runs its driver
registration. `runtime/server` blank-imports the built-in driver as a fallback,
so a separate application blank import is not required. Other drivers use the
same `authz.Validator` factory seam.

Generate `ContinuationKey` from a cryptographically secure source and provide at
least 32 bytes. Keep it stable across instances and restarts that may receive an
MRTR retry, distribute it through your secret manager, and rotate it only when
invalidating outstanding continuation state is acceptable. It is not sent on
the wire.

`AllowedAlgorithms` is mandatory. The driver begins RFC 8414 discovery only at
the configured trusted issuer, requires the discovered issuer to match exactly,
requires an HTTPS `jwks_uri`, bounds response size and key count, and rate-limits
key refresh. Redirects for discovery and JWKS retrieval must stay on the original
HTTPS origin. An expired cache is never used when refresh fails. It validates
signature, JWT/JWK algorithm and key-type compatibility, exact issuer, exact
audience, expiry, subject, and scopes. It never retains or logs the bearer token.

## Metadata and challenges

For resource `https://mcp.example.com/mcp`, the same HTTP handler serves:

```text
https://mcp.example.com/.well-known/oauth-protected-resource/mcp
```

The path-aware RFC 9728 document advertises the canonical resource, configured
authorization server, scopes, and header bearer method. Only `GET` is accepted.
Every other route remains protected.

`Scopes` is the complete supported scope set advertised in metadata.
`RequiredScopes` is the global operation policy: every protected MCP request must
contain every listed scope. Dockyard does not currently select scopes per tool or
operation; use separate endpoints when operations need different scope policies.

A request with no token receives `401` and a Bearer challenge containing
`resource_metadata`. A malformed or rejected token also includes
`error="invalid_token"`. A valid token missing any configured scope receives
`403`, `error="insufficient_scope"`, and the complete required `scope` value.
Challenge text never includes token-derived claims or the token itself.

## Principal, Tasks, and MRTR

After validation, handlers can read a defensive copy of the identity:

```go
principal, ok := authz.PrincipalFromContext(ctx)
if !ok {
	return result, errors.New("verified principal missing")
}
```

The verified principal contains issuer, subject, resource, and scopes. Dockyard
derives a non-reversible binding from issuer + subject + resource for durable
Task ownership; it does not persist the raw subject or token. Task create, get,
update, cancel, and input operations use that binding.

For core MRTR, Dockyard authenticates `requestState` with the continuation key
and binds it to the verified principal, tool, arguments, and a ten-minute
lifetime. Another principal cannot replay it. This state is independent of
durable Task input and contains no bearer token.

## Client boundary

Dockyard is server-side only. It does not acquire, forward, refresh, or store
OAuth tokens. Harbor is the production MCP/OAuth client and sends the access
token to the protected Dockyard endpoint. The local inspector and install boot
check remain short-lived, operator-initiated development clients; they do not
implement an OAuth flow or credential store.

## See also

- [Packaging + install](packaging)
- [Dev loop](dev-loop)
- [Inspector](inspector)
- [RFC §15 and §19.2](/reference/rfc)
