package authz

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrMissingToken indicates that no bearer token was supplied.
	ErrMissingToken = errors.New("authz: bearer token missing")
	// ErrInvalidToken indicates that a bearer token is malformed or failed validation.
	ErrInvalidToken = errors.New("authz: invalid token")
	// ErrInsufficientScope indicates that the principal lacks a required scope.
	ErrInsufficientScope = errors.New("authz: insufficient scope")
)

// ProtectedResourceMetadata is the RFC 9728 response shape used by Dockyard.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// Metadata returns defensively copied RFC 9728 handler data.
func (c Config) Metadata() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{c.Resource, []string{c.Issuer}, append([]string(nil), c.Scopes...), []string{"header"}}
}

// MetadataURL returns the RFC 9728 path-aware well-known URL. A resource path is
// appended after /.well-known/oauth-protected-resource.
func (c Config) MetadataURL() (string, error) {
	u, err := url.Parse(c.Resource)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("authz: invalid resource URL")
	}
	escapedPath := "/.well-known/oauth-protected-resource"
	if resourcePath := strings.TrimPrefix(u.EscapedPath(), "/"); resourcePath != "" {
		escapedPath += "/" + resourcePath
	}
	path, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", errors.New("authz: invalid escaped resource path")
	}
	u.Path, u.RawPath, u.Fragment = path, escapedPath, ""
	return u.String(), nil
}

// MetadataHandler serves immutable RFC 9728 metadata.
func (c Config) MetadataHandler() http.Handler {
	data, _ := json.Marshal(c.Metadata())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

// ParseBearer strictly accepts exactly one Authorization value containing the
// case-insensitive Bearer scheme, one SP, and a non-empty token68 value.
func ParseBearer(values []string) (string, error) {
	if len(values) == 0 {
		return "", ErrMissingToken
	}
	if len(values) != 1 {
		return "", ErrInvalidToken
	}
	v := values[0]
	if len(v) < 8 || !strings.EqualFold(v[:6], "Bearer") || v[6] != ' ' || v[7] == ' ' {
		return "", ErrInvalidToken
	}
	token := v[7:]
	padding := false
	for _, r := range token {
		if r == '=' {
			padding = true
			continue
		}
		if padding || (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("-._~+/", r) {
			return "", ErrInvalidToken
		}
	}
	return token, nil
}

// RequireScopes requires every listed scope and returns the missing scopes.
func RequireScopes(p Principal, required ...string) error {
	have := make(map[string]struct{}, len(p.Scopes))
	for _, scope := range p.Scopes {
		have[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := have[scope]; !ok {
			return fmt.Errorf("%w: required scope %q", ErrInsufficientScope, scope)
		}
	}
	return nil
}

// Challenge constructs a Bearer challenge without token-derived text.
func Challenge(metadataURL string, err error, requiredScopes []string) string {
	parts := []string{`Bearer resource_metadata="` + escapeChallenge(metadataURL) + `"`}
	if errors.Is(err, ErrInsufficientScope) {
		parts = append(parts, `error="insufficient_scope"`)
		if len(requiredScopes) != 0 {
			parts = append(parts, `scope="`+escapeChallenge(strings.Join(requiredScopes, " "))+`"`)
		}
	} else if err != nil && !errors.Is(err, ErrMissingToken) {
		parts = append(parts, `error="invalid_token"`)
	}
	return strings.Join(parts, ", ")
}

func escapeChallenge(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(s)
}
