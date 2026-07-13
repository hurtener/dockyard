# OAuth-Protected HTTP

Dockyard HTTP servers are unauthenticated unless authorization is explicitly
configured. This example shows the complete resource-server configuration while
keeping stdio and ordinary HTTP behavior unchanged.

Add these imports to the server entrypoint:

```go
import (
	"fmt"
	"os"
	"strings"

	"github.com/hurtener/dockyard/runtime/authz"
	"github.com/hurtener/dockyard/runtime/authz/jwtjwks"
)
```

Build the authorization configuration only for HTTP. An entirely unset
configuration leaves HTTP unauthenticated; a partial configuration is rejected.

```go
func authorizationFromEnv() (*authz.Config, error) {
	resource := strings.TrimSpace(os.Getenv("DOCKYARD_OAUTH_RESOURCE"))
	issuer := strings.TrimSpace(os.Getenv("DOCKYARD_OAUTH_ISSUER"))
	scopeText := strings.TrimSpace(os.Getenv("DOCKYARD_OAUTH_SCOPES"))
	continuationKey := os.Getenv("DOCKYARD_OAUTH_CONTINUATION_KEY")

	if resource == "" && issuer == "" && scopeText == "" && continuationKey == "" {
		return nil, nil
	}
	if resource == "" || issuer == "" || scopeText == "" || continuationKey == "" {
		return nil, fmt.Errorf("OAuth requires DOCKYARD_OAUTH_RESOURCE, DOCKYARD_OAUTH_ISSUER, DOCKYARD_OAUTH_SCOPES, and DOCKYARD_OAUTH_CONTINUATION_KEY")
	}

	cfg := &authz.Config{
		Driver:          jwtjwks.DriverName,
		Resource:        resource,
		Issuer:          issuer,
		Scopes:          strings.Fields(scopeText),
		RequiredScopes:  strings.Fields(scopeText),
		ContinuationKey: []byte(continuationKey),
		DriverConfig: jwtjwks.Config{
			AllowedAlgorithms: []string{"RS256"},
		},
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid OAuth resource-server configuration: %w", err)
	}
	return cfg, nil
}
```

Pass the result to the existing HTTP handler setup:

```go
authorization, err := authorizationFromEnv()
if err != nil {
	return err
}
handler, err := srv.HTTPHandler(&server.HTTPOptions{
	ProtocolMode:  server.Dual,
	Authorization: authorization,
})
```

Set deployment-specific values in the process environment:

```sh
export DOCKYARD_OAUTH_RESOURCE=https://api.example.com/mcp
export DOCKYARD_OAUTH_ISSUER=https://identity.example.com/
export DOCKYARD_OAUTH_SCOPES='mcp:read mcp:write'
export DOCKYARD_OAUTH_CONTINUATION_KEY="$(openssl rand -base64 32)"
DOCKYARD_TRANSPORT=http go run .
```

The resource and issuer must be canonical HTTPS URLs. The continuation key must
contain at least 32 bytes and must come from secret storage in production; never
commit it or print it. `RS256` is intentionally explicit. Change the allowed
algorithm only to match a trusted issuer's documented signing policy.

Dockyard publishes RFC 9728 metadata and Bearer challenges from this
configuration. A validated token's issuer and subject become the verified
principal for both modern and legacy HTTP requests. The same identity binds
Tasks and multi-round-trip continuation retries, without exposing the bearer
token to handlers.
