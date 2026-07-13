package jwtjwks

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hurtener/dockyard/runtime/authz"
)

// DriverName is the name this validator driver registers under.
const DriverName = "jwt-jwks"

// Config controls bounded discovery and key caching. Zero values receive secure
// defaults. AllowedAlgorithms must be explicit.
type Config struct {
	AllowedAlgorithms []string         `json:"allowed_algorithms"`
	HTTPClient        *http.Client     `json:"-"`
	MaxResponseBytes  int64            `json:"max_response_bytes,omitempty"`
	MaxKeys           int              `json:"max_keys,omitempty"`
	CacheTTL          time.Duration    `json:"cache_ttl,omitempty"`
	RefreshCooldown   time.Duration    `json:"refresh_cooldown,omitempty"`
	ClockSkew         time.Duration    `json:"clock_skew,omitempty"`
	Now               func() time.Time `json:"-"`
}

type jwksDocument struct {
	Keys []json.RawMessage `json:"keys"`
}

type verificationKey struct {
	key any
	alg string
	kty string
	crv string
}

// Validator is safe for concurrent validation and key rotation.
type Validator struct {
	issuer, resource, jwksURI string
	algorithms                []string
	client                    *http.Client
	maxBytes                  int64
	maxKeys                   int
	ttl, cooldown, skew       time.Duration
	now                       func() time.Time
	mu                        sync.RWMutex
	refreshMu                 sync.Mutex
	keys                      map[string]verificationKey
	expires, lastRefresh      time.Time
}

func init() {
	authz.RegisterDriver(DriverName, func(ctx context.Context, cfg authz.Config) (authz.Validator, error) {
		driver, ok := cfg.DriverConfig.(Config)
		if cfg.DriverConfig != nil && !ok {
			return nil, errors.New("jwt-jwks config: DriverConfig must be jwtjwks.Config")
		}
		return New(ctx, cfg.Issuer, cfg.Resource, driver)
	})
}

// New discovers the configured issuer and primes its JWKS cache.
func New(ctx context.Context, issuer, resource string, cfg Config) (*Validator, error) {
	if len(cfg.AllowedAlgorithms) == 0 {
		return nil, errors.New("jwt-jwks: allowed algorithms are required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	client := *cfg.HTTPClient
	callerRedirectPolicy := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || req.URL.Scheme != "https" || !sameOrigin(req.URL, via[0].URL) {
			return errors.New("jwt-jwks: redirect must remain on the HTTPS origin")
		}
		if callerRedirectPolicy != nil {
			return callerRedirectPolicy(req, via)
		}
		if len(via) >= 10 {
			return errors.New("jwt-jwks: too many redirects")
		}
		return nil
	}
	cfg.HTTPClient = &client
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 1 << 20
	}
	if cfg.MaxKeys == 0 {
		cfg.MaxKeys = 32
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 15 * time.Minute
	}
	if cfg.RefreshCooldown == 0 {
		cfg.RefreshCooldown = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxResponseBytes < 1 || cfg.MaxKeys < 1 || cfg.CacheTTL < 0 || cfg.RefreshCooldown < 0 || cfg.ClockSkew < 0 {
		return nil, errors.New("jwt-jwks: invalid bounds")
	}
	for _, alg := range cfg.AllowedAlgorithms {
		if alg != "RS256" && alg != "RS384" && alg != "RS512" && alg != "ES256" && alg != "ES384" && alg != "ES512" {
			return nil, fmt.Errorf("jwt-jwks: unsupported algorithm %q", alg)
		}
	}
	discURL, err := discoveryURL(issuer)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := fetchJSON(ctx, cfg.HTTPClient, discURL, cfg.MaxResponseBytes, &metadata); err != nil {
		return nil, fmt.Errorf("jwt-jwks: discovery failed: %w", err)
	}
	if metadata.Issuer != issuer {
		return nil, errors.New("jwt-jwks: discovery issuer mismatch")
	}
	jwks, err := url.Parse(metadata.JWKSURI)
	if err != nil || jwks.Scheme != "https" || jwks.Host == "" || jwks.User != nil || jwks.Fragment != "" {
		return nil, errors.New("jwt-jwks: invalid HTTPS jwks_uri")
	}
	v := &Validator{issuer: issuer, resource: resource, jwksURI: metadata.JWKSURI, algorithms: append([]string(nil), cfg.AllowedAlgorithms...), client: cfg.HTTPClient, maxBytes: cfg.MaxResponseBytes, maxKeys: cfg.MaxKeys, ttl: cfg.CacheTTL, cooldown: cfg.RefreshCooldown, skew: cfg.ClockSkew, now: cfg.Now}
	if err := v.refresh(ctx, true); err != nil {
		return nil, err
	}
	return v, nil
}

func discoveryURL(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("jwt-jwks: issuer must be canonical HTTPS URL")
	}
	path := strings.TrimPrefix(u.EscapedPath(), "/")
	u.Path, u.RawPath = "/.well-known/oauth-authorization-server", ""
	if path != "" {
		u.Path += "/" + path
	}
	return u.String(), nil
}

// Validate verifies signature, exact algorithm/issuer/audience, subject and time
// claims. Returned errors never include token or claim values.
func (v *Validator) Validate(ctx context.Context, token string) (authz.Principal, error) {
	if token == "" {
		return authz.Principal{}, authz.ErrInvalidToken
	}
	v.mu.RLock()
	expired := !v.now().Before(v.expires)
	v.mu.RUnlock()
	if expired {
		if err := v.refresh(ctx, false); err != nil {
			return authz.Principal{}, authz.ErrInvalidToken
		}
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errors.New("missing key id")
		}
		key, ok := v.key(kid)
		if !ok {
			if refreshErr := v.refresh(ctx, true); refreshErr == nil {
				key, ok = v.key(kid)
			}
		}
		if !ok {
			return nil, errors.New("unknown key id")
		}
		if err := key.matches(t.Method.Alg()); err != nil {
			return nil, err
		}
		return key.key, nil
	}, jwt.WithValidMethods(v.algorithms), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.resource), jwt.WithExpirationRequired(), jwt.WithLeeway(v.skew), jwt.WithTimeFunc(v.now))
	if err != nil || !parsed.Valid {
		return authz.Principal{}, authz.ErrInvalidToken
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return authz.Principal{}, authz.ErrInvalidToken
	}
	scopes, ok := parseScopes(claims)
	if !ok {
		return authz.Principal{}, authz.ErrInvalidToken
	}
	return authz.Principal{Issuer: v.issuer, Subject: sub, Resource: v.resource, Scopes: scopes}, nil
}

func parseScopes(claims jwt.MapClaims) ([]string, bool) {
	value, exists := claims["scope"]
	if !exists {
		return nil, true
	}
	raw, ok := value.(string)
	if !ok {
		return nil, false
	}
	if raw == "" {
		return nil, true
	}
	parts := strings.Fields(raw)
	if strings.Join(parts, " ") != raw {
		return nil, false
	}
	return parts, true
}

func (v *Validator) key(kid string) (verificationKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	key, ok := v.keys[kid]
	return key, ok
}

func (v *Validator) refresh(ctx context.Context, force bool) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	now := v.now()
	v.mu.RLock()
	fresh := now.Before(v.expires)
	hasRefreshed := !v.lastRefresh.IsZero()
	cooldown := now.Sub(v.lastRefresh) < v.cooldown
	v.mu.RUnlock()
	if hasRefreshed && cooldown && fresh {
		return nil
	}
	if !force && fresh {
		return nil
	}
	var doc jwksDocument
	if err := fetchJSON(ctx, v.client, v.jwksURI, v.maxBytes, &doc); err != nil {
		return errors.New("jwt-jwks: key refresh failed")
	}
	if len(doc.Keys) == 0 || len(doc.Keys) > v.maxKeys {
		return errors.New("jwt-jwks: invalid key count")
	}
	keys := make(map[string]verificationKey, len(doc.Keys))
	for _, raw := range doc.Keys {
		kid, key, err := parseJWK(raw)
		if err != nil {
			return errors.New("jwt-jwks: invalid key set")
		}
		if _, duplicate := keys[kid]; duplicate {
			return errors.New("jwt-jwks: duplicate key id")
		}
		keys[kid] = key
	}
	v.mu.Lock()
	v.keys, v.lastRefresh, v.expires = keys, now, now.Add(v.ttl)
	v.mu.Unlock()
	return nil
}

func fetchJSON(ctx context.Context, client *http.Client, endpoint string, limit int64, dst any) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return errors.New("response too large")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func parseJWK(raw json.RawMessage) (string, verificationKey, error) {
	var j struct{ Kty, Kid, Use, Alg, N, E, Crv, X, Y string }
	if err := json.Unmarshal(raw, &j); err != nil || j.Kid == "" || j.Use != "" && j.Use != "sig" {
		return "", verificationKey{}, errors.New("bad jwk")
	}
	if j.Alg == "" {
		return "", verificationKey{}, errors.New("jwk alg is required")
	}
	decode := func(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
	switch j.Kty {
	case "RSA":
		nb, err := decode(j.N)
		if err != nil {
			return "", verificationKey{}, err
		}
		eb, err := decode(j.E)
		if err != nil {
			return "", verificationKey{}, err
		}
		e := 0
		for _, b := range eb {
			e = e<<8 + int(b)
		}
		if len(nb) < 256 || e < 3 {
			return "", verificationKey{}, errors.New("weak rsa key")
		}
		if !strings.HasPrefix(j.Alg, "RS") {
			return "", verificationKey{}, errors.New("rsa algorithm mismatch")
		}
		return j.Kid, verificationKey{key: &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, alg: j.Alg, kty: j.Kty}, nil
	case "EC":
		curves := map[string]struct {
			ecdh      ecdh.Curve
			ecdsa     elliptic.Curve
			fieldSize int
		}{
			"P-256": {ecdh.P256(), elliptic.P256(), 32},
			"P-384": {ecdh.P384(), elliptic.P384(), 48},
			"P-521": {ecdh.P521(), elliptic.P521(), 66},
		}
		curve, ok := curves[j.Crv]
		xb, xerr := decode(j.X)
		yb, yerr := decode(j.Y)
		if !ok || xerr != nil || yerr != nil || len(xb) != curve.fieldSize || len(yb) != curve.fieldSize {
			return "", verificationKey{}, errors.New("bad ec key")
		}
		encoded := make([]byte, 1+2*curve.fieldSize)
		encoded[0] = 4 // SEC1 uncompressed point marker.
		copy(encoded[1:], xb)
		copy(encoded[1+curve.fieldSize:], yb)
		if _, err := curve.ecdh.NewPublicKey(encoded); err != nil {
			return "", verificationKey{}, errors.New("off-curve key")
		}
		wantAlg := map[string]string{"P-256": "ES256", "P-384": "ES384", "P-521": "ES512"}[j.Crv]
		if j.Alg != wantAlg {
			return "", verificationKey{}, errors.New("ec algorithm mismatch")
		}
		x, y := new(big.Int).SetBytes(xb), new(big.Int).SetBytes(yb)
		return j.Kid, verificationKey{key: &ecdsa.PublicKey{Curve: curve.ecdsa, X: x, Y: y}, alg: j.Alg, kty: j.Kty, crv: j.Crv}, nil
	default:
		return "", verificationKey{}, errors.New("unsupported key type")
	}
}

func (k verificationKey) matches(alg string) error {
	if k.alg == "" || k.alg != alg {
		return errors.New("jwk algorithm does not match token")
	}
	if strings.HasPrefix(alg, "RS") && k.kty != "RSA" || strings.HasPrefix(alg, "ES") && k.kty != "EC" {
		return errors.New("jwk key type does not match token")
	}
	if wantCurve := map[string]string{"ES256": "P-256", "ES384": "P-384", "ES512": "P-521"}[alg]; wantCurve != "" && k.crv != wantCurve {
		return errors.New("jwk curve does not match token")
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
