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

const (
	defaultFetchTimeout = 10 * time.Second
	minResponseBytes    = 1 << 10
	maxResponseBytes    = 4 << 20
	maxKeyCount         = 128
	minCacheTTL         = time.Second
	maxCacheTTL         = 24 * time.Hour
	minRefreshCooldown  = time.Second
	maxRefreshCooldown  = 5 * time.Minute
	maxClockSkew        = 5 * time.Minute
	minRSAModulusBytes  = 256
	maxRSAModulusBytes  = 1024
)

var errUnsupportedJWK = errors.New("jwt-jwks: unsupported jwk")

// Config controls bounded discovery and key caching. Zero values receive secure
// defaults. AllowedAlgorithms must explicitly contain only RS256, RS384, RS512,
// ES256, ES384, or ES512. MaxResponseBytes is constrained to 1 KiB..4 MiB,
// MaxKeys to 1..128, CacheTTL to 1 second..24 hours, RefreshCooldown to
// 1 second..5 minutes, and ClockSkew to 0..5 minutes.
type Config struct {
	// AllowedAlgorithms is mandatory and limits both JWT headers and usable JWKs.
	AllowedAlgorithms []string     `json:"allowed_algorithms"`
	HTTPClient        *http.Client `json:"-"`
	// MaxResponseBytes bounds each discovery or JWKS document (default 1 MiB).
	MaxResponseBytes int64 `json:"max_response_bytes,omitempty"`
	// MaxKeys bounds the number of keys in one JWKS document (default 32).
	MaxKeys int `json:"max_keys,omitempty"`
	// CacheTTL controls successful JWKS retention (default 15 minutes).
	CacheTTL time.Duration `json:"cache_ttl,omitempty"`
	// RefreshCooldown rate-limits repeated JWKS refresh attempts (default 5 seconds).
	RefreshCooldown time.Duration `json:"refresh_cooldown,omitempty"`
	// ClockSkew is JWT time-claim leeway (default zero).
	ClockSkew time.Duration    `json:"clock_skew,omitempty"`
	Now       func() time.Time `json:"-"`
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
	expires, lastAttempt      time.Time
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
		cfg.HTTPClient = &http.Client{Timeout: defaultFetchTimeout}
	}
	client := *cfg.HTTPClient
	if client.Timeout <= 0 || client.Timeout > defaultFetchTimeout {
		client.Timeout = defaultFetchTimeout
	}
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
	if cfg.MaxResponseBytes < minResponseBytes || cfg.MaxResponseBytes > maxResponseBytes {
		return nil, fmt.Errorf("jwt-jwks: max response bytes must be between %d and %d", minResponseBytes, maxResponseBytes)
	}
	if cfg.MaxKeys < 1 || cfg.MaxKeys > maxKeyCount {
		return nil, fmt.Errorf("jwt-jwks: max keys must be between 1 and %d", maxKeyCount)
	}
	if cfg.CacheTTL < minCacheTTL || cfg.CacheTTL > maxCacheTTL {
		return nil, fmt.Errorf("jwt-jwks: cache TTL must be between %s and %s", minCacheTTL, maxCacheTTL)
	}
	if cfg.RefreshCooldown < minRefreshCooldown || cfg.RefreshCooldown > maxRefreshCooldown {
		return nil, fmt.Errorf("jwt-jwks: refresh cooldown must be between %s and %s", minRefreshCooldown, maxRefreshCooldown)
	}
	if cfg.ClockSkew < 0 || cfg.ClockSkew > maxClockSkew {
		return nil, fmt.Errorf("jwt-jwks: clock skew must be between 0s and %s", maxClockSkew)
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
	parsed, claims, err := v.parse(ctx, token, true)
	if err != nil && errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		if refreshErr := v.refresh(ctx, true); refreshErr == nil {
			parsed, claims, err = v.parse(ctx, token, false)
		}
	}
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

func (v *Validator) parse(ctx context.Context, token string, refreshUnknown bool) (*jwt.Token, jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errors.New("missing key id")
		}
		key, ok := v.key(kid)
		if !ok && refreshUnknown {
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
	return parsed, claims, err
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
	hasAttempted := !v.lastAttempt.IsZero()
	cooldown := now.Sub(v.lastAttempt) < v.cooldown
	attemptedSinceExpiry := !v.lastAttempt.Before(v.expires)
	v.mu.RUnlock()
	if hasAttempted && cooldown {
		if fresh {
			return nil
		}
		// The successful fill that produced this expiry must not prevent the
		// first refresh after expiration when CacheTTL is shorter than the
		// cooldown. Once a post-expiry attempt fails, keep throttling retries.
		if attemptedSinceExpiry {
			return errors.New("jwt-jwks: key refresh throttled")
		}
	}
	if !force && fresh {
		return nil
	}
	v.mu.Lock()
	v.lastAttempt = now
	v.mu.Unlock()
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
		if errors.Is(err, errUnsupportedJWK) {
			continue
		}
		if err != nil {
			return errors.New("jwt-jwks: invalid key set")
		}
		if key.alg != "" && !contains(v.algorithms, key.alg) || key.alg == "" && !keySupportsAny(key, v.algorithms) {
			continue
		}
		if _, duplicate := keys[kid]; duplicate {
			return errors.New("jwt-jwks: duplicate key id")
		}
		keys[kid] = key
	}
	if len(keys) == 0 {
		return errors.New("jwt-jwks: no usable verification keys")
	}
	v.mu.Lock()
	v.keys, v.expires = keys, now.Add(v.ttl)
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
	var j struct {
		Kty, Kid, Use, Alg, N, E, Crv, X, Y string
		KeyOps                              json.RawMessage `json:"key_ops"`
	}
	if err := json.Unmarshal(raw, &j); err != nil || j.Kid == "" {
		return "", verificationKey{}, errors.New("bad jwk")
	}
	if j.Use != "" && j.Use != "sig" {
		return "", verificationKey{}, errUnsupportedJWK
	}
	if len(j.KeyOps) > 0 {
		var keyOps []string
		if err := json.Unmarshal(j.KeyOps, &keyOps); err != nil {
			return "", verificationKey{}, errors.New("bad jwk key operations")
		}
		if !contains(keyOps, "verify") {
			return "", verificationKey{}, errUnsupportedJWK
		}
	}
	decode := func(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
	switch j.Kty {
	case "RSA":
		if j.Alg != "" && !strings.HasPrefix(j.Alg, "RS") {
			return "", verificationKey{}, errUnsupportedJWK
		}
		nb, err := decode(j.N)
		if err != nil {
			return "", verificationKey{}, err
		}
		if len(nb) < minRSAModulusBytes || len(nb) > maxRSAModulusBytes || nb[0] == 0 {
			return "", verificationKey{}, errors.New("invalid rsa modulus")
		}
		eb, err := decode(j.E)
		if err != nil {
			return "", verificationKey{}, err
		}
		if len(eb) == 0 || len(eb) > 4 {
			return "", verificationKey{}, errors.New("invalid rsa exponent")
		}
		e := uint64(0)
		for _, b := range eb {
			e = e<<8 + uint64(b)
		}
		if e < 3 || e > 1<<31-1 || e%2 == 0 {
			return "", verificationKey{}, errors.New("weak rsa key")
		}
		n := new(big.Int).SetBytes(nb)
		if n.BitLen() < 8*minRSAModulusBytes || n.BitLen() > 8*maxRSAModulusBytes {
			return "", verificationKey{}, errors.New("invalid rsa modulus size")
		}
		return j.Kid, verificationKey{key: &rsa.PublicKey{N: n, E: int(e)}, alg: j.Alg, kty: j.Kty}, nil
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
		if !ok {
			return "", verificationKey{}, errUnsupportedJWK
		}
		xb, xerr := decode(j.X)
		yb, yerr := decode(j.Y)
		if xerr != nil || yerr != nil || len(xb) != curve.fieldSize || len(yb) != curve.fieldSize {
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
		if j.Alg != "" && j.Alg != wantAlg {
			return "", verificationKey{}, errors.New("ec algorithm mismatch")
		}
		x, y := new(big.Int).SetBytes(xb), new(big.Int).SetBytes(yb)
		return j.Kid, verificationKey{key: &ecdsa.PublicKey{Curve: curve.ecdsa, X: x, Y: y}, alg: j.Alg, kty: j.Kty, crv: j.Crv}, nil
	default:
		return "", verificationKey{}, errUnsupportedJWK
	}
}

func (k verificationKey) matches(alg string) error {
	if k.alg != "" && k.alg != alg {
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func keySupportsAny(key verificationKey, algorithms []string) bool {
	for _, alg := range algorithms {
		if key.matches(alg) == nil {
			return true
		}
	}
	return false
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
