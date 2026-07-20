package authz

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
)

// Principal is identity established by a Validator. Scopes is always copied at
// context boundaries so callers cannot mutate another component's identity.
type Principal struct {
	Issuer   string
	Subject  string
	Resource string
	Scopes   []string
}

type principalKey struct{}

// WithPrincipal returns a child context containing a defensive copy of p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.Scopes = append([]string(nil), p.Scopes...)
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext returns a defensive copy of the verified principal.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok {
		return Principal{}, false
	}
	p.Scopes = append([]string(nil), p.Scopes...)
	return p, true
}

type rawTokenKey struct{}

// WithRawToken returns a child context carrying the validated inbound bearer
// token. Framework-internal: the server sets it only when Config.ExposeRawToken
// is true, and only after the token has passed every validation gate (D-201).
func WithRawToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, rawTokenKey{}, token)
}

// RawTokenFromContext returns the validated inbound bearer token when the server
// was configured with ExposeRawToken. The false result — no token available — is
// the normal case; expose the token only to present it as an RFC 8693
// subject_token to a trusted token-exchange endpoint (never log, persist, or
// forward it elsewhere).
func RawTokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(rawTokenKey{}).(string)
	return t, ok && t != ""
}

// WithoutRawToken returns a child context in which the exposed token reads as
// absent. It keeps the delegation token request-scoped: work detached from the
// originating request (e.g. an async Task run) must not inherit it — such work
// re-exchanges on its own fresh inbound token. Registered as a Tasks
// detach-scrubber by runtime/server so the strip is automatic.
func WithoutRawToken(ctx context.Context) context.Context {
	if _, ok := ctx.Value(rawTokenKey{}).(string); !ok {
		return ctx
	}
	return context.WithValue(ctx, rawTokenKey{}, "")
}

// BindingKey returns a stable, non-reversible identity suitable for persistence.
// Length-prefixing prevents tuple ambiguity; SHA-256 makes collisions
// computationally infeasible without requiring a deployment secret.
func (p Principal) BindingKey() string {
	h := sha256.New()
	for _, value := range []string{p.Issuer, p.Subject, p.Resource} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		h.Write(size[:])
		h.Write([]byte(value))
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
