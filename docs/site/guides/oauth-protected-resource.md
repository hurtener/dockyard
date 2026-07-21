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

The JWT/JWKS driver applies these configuration constraints:

- `AllowedAlgorithms`: one or more of `RS256`, `RS384`, `RS512`, `ES256`,
  `ES384`, or `ES512`.
- `MaxResponseBytes`: 1 KiB to 4 MiB; the default is 1 MiB.
- `MaxKeys`: 1 to 128; the default is 32.
- `CacheTTL`: 1 second to 24 hours; the default is 15 minutes.
- `RefreshCooldown`: 1 second to 5 minutes; the default is 5 seconds.
- `ClockSkew`: zero to 5 minutes; the default is zero.

Configuration errors identify the invalid field and accepted range. A supplied
HTTP client's timeout is capped at 10 seconds. RSA verification keys must have a
2048- to 8192-bit modulus and a valid odd exponent; unsupported key types and
keys whose `use`, `key_ops`, `alg`, type, or curve cannot verify an allowed
algorithm are ignored. A JWKS document with no usable verification key is
rejected.

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

### Delegated token exchange (RFC 8693)

By default the validated inbound token is discarded after the request — handlers
receive the parsed principal, not the token. A server that must obtain a
*downstream* token (for example, to call Microsoft Graph on the caller's behalf)
gets it by [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) token exchange:
present the validated inbound token as the `subject_token` to a trusted
token-exchange endpoint, which independently re-verifies the JWT and returns a
new token minted for the downstream audience.

For that — and only that — opt into exposing the validated token:

```go
opts := &server.HTTPOptions{
	Authorization: &authz.Config{
		// … driver, resource, issuer, required scopes …
		ExposeRawToken: true,
	},
}
```

The token is then retrievable in a handler, only after every validation gate has
passed:

```go
token, ok := authz.RawTokenFromContext(ctx)
if !ok {
	return result, errors.New("delegation token unavailable")
}
// Present `token` as the RFC 8693 subject_token to the TRUSTED exchange only.
form := url.Values{
	"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
	"subject_token":      {token},
	"subject_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
	"audience":           {"https://graph.microsoft.com"},
}
// POST form to the trusted token-exchange endpoint over TLS; use the
// provider-audience token it returns to call the downstream API.
```

::: warning This is delegation, not token passthrough
The inbound token's audience is *this server*, and it is presented **only** to
the trusted exchange — never forwarded to a downstream provider API. The
downstream call uses the exchange-minted token, which carries the provider's own
audience. The two tokens stay separate. Send the inbound token to nothing but the
trusted exchange, always over TLS, and never log or persist it. `ExposeRawToken`
is off by default; leave it off unless the server performs delegated exchange.
The token is request-scoped and never enters durable Task or MRTR state (D-201).
:::

## Client boundary

Dockyard is server-side only. It validates the inbound access token and, by
default, discards it; it never acquires, refreshes, or stores OAuth tokens, and
never forwards a token to a downstream resource API. The one exception is
opt-in, above: with `ExposeRawToken`, a handler may present the validated token
to a trusted RFC 8693 exchange — Dockyard still performs no OAuth-client flow
itself; the handler is the exchange client. Harbor remains the production
MCP/OAuth client and sends the access token to the protected Dockyard endpoint.
The local inspector and install boot check remain short-lived,
operator-initiated development clients; they do not implement an OAuth flow or
credential store.

## See also

- [Packaging + install](packaging)
- [Dev loop](dev-loop)
- [Inspector](inspector)
- [RFC §15 and §19.2](/reference/rfc)
