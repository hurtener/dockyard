package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/hurtener/dockyard/runtime/authz"
)

const continuationLifetime = 10 * time.Minute

type continuationProtector struct{ key []byte }
type continuationProtectorKey struct{}
type continuationEnvelope struct {
	Binding   string `json:"b"`
	Tool      string `json:"t"`
	Arguments string `json:"a"`
	State     string `json:"s"`
	Expires   int64  `json:"e"`
}

func newContinuationProtector(key []byte) *continuationProtector {
	return &continuationProtector{key: append([]byte(nil), key...)}
}
func withContinuationProtector(ctx context.Context, p *continuationProtector) context.Context {
	return context.WithValue(ctx, continuationProtectorKey{}, p)
}
func continuationFromContext(ctx context.Context) (*continuationProtector, bool) {
	p, ok := ctx.Value(continuationProtectorKey{}).(*continuationProtector)
	return p, ok
}
func argumentDigest(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (p *continuationProtector) seal(principal authz.Principal, tool string, args json.RawMessage, state RequestState) (RequestState, error) {
	e := continuationEnvelope{Binding: principal.BindingKey(), Tool: tool, Arguments: argumentDigest(args), State: string(state), Expires: time.Now().Add(continuationLifetime).Unix()}
	payload, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write(payload)
	return RequestState(base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), nil
}
func (p *continuationProtector) open(principal authz.Principal, tool string, args json.RawMessage, state RequestState) (RequestState, error) {
	parts := []byte(state)
	dot := -1
	for i, b := range parts {
		if b == '.' {
			dot = i
			break
		}
	}
	if dot < 1 {
		return "", errors.New("invalid authenticated continuation")
	}
	payload, err := base64.RawURLEncoding.DecodeString(string(parts[:dot]))
	if err != nil {
		return "", errors.New("invalid authenticated continuation")
	}
	sig, err := base64.RawURLEncoding.DecodeString(string(parts[dot+1:]))
	if err != nil {
		return "", errors.New("invalid authenticated continuation")
	}
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", errors.New("invalid authenticated continuation")
	}
	var e continuationEnvelope
	if json.Unmarshal(payload, &e) != nil || e.Binding != principal.BindingKey() || e.Tool != tool || e.Arguments != argumentDigest(args) || time.Now().Unix() > e.Expires {
		return "", errors.New("invalid authenticated continuation")
	}
	return RequestState(e.State), nil
}
