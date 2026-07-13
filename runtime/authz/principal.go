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
