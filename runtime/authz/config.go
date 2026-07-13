package authz

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Config is the authorization configuration consumed by HTTP server options.
// Resource and Issuer must be canonical HTTPS URLs. Scope values must satisfy
// RFC 6749's scope-token grammar; offline_access is rejected because Dockyard is
// a resource server and does not issue refresh tokens. DriverConfig is
// interpreted only by the selected driver.
type Config struct {
	Driver   string
	Resource string
	Issuer   string
	// Scopes are advertised as supported in protected-resource metadata. Each
	// value must be a non-empty RFC 6749 scope-token other than offline_access.
	Scopes []string
	// RequiredScopes are required on every protected MCP operation and follow
	// the same syntax restrictions as Scopes.
	RequiredScopes []string
	DriverConfig   any
	// ContinuationKey authenticates framework-owned MRTR continuation state.
	// It must contain at least 32 bytes and is never exposed on the wire.
	ContinuationKey []byte
}

// Validator validates an unadorned bearer token and never retains it.
type Validator interface {
	Validate(context.Context, string) (Principal, error)
}

// Factory constructs a validator for a validated Config.
type Factory func(context.Context, Config) (Validator, error)

var (
	driversMu sync.RWMutex
	drivers   = map[string]Factory{}
)

// RegisterDriver registers a process-wide validator driver. Duplicate or
// invalid registrations panic because they are programming errors at startup.
func RegisterDriver(name string, factory Factory) {
	if strings.TrimSpace(name) == "" || factory == nil {
		panic("dockyard/runtime/authz: invalid driver registration")
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := drivers[name]; exists {
		panic(fmt.Sprintf("dockyard/runtime/authz: driver %q already registered", name))
	}
	drivers[name] = factory
}

// Drivers returns registered driver names in deterministic order.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Open validates cfg and constructs its validator.
func Open(ctx context.Context, cfg Config) (Validator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	driversMu.RLock()
	factory := drivers[cfg.Driver]
	driversMu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("authz: unknown driver %q", cfg.Driver)
	}
	cfg.Scopes = append([]string(nil), cfg.Scopes...)
	cfg.RequiredScopes = append([]string(nil), cfg.RequiredScopes...)
	cfg.ContinuationKey = append([]byte(nil), cfg.ContinuationKey...)
	v, err := factory(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("authz: open driver %q: %w", cfg.Driver, err)
	}
	return v, nil
}

// Validate checks transport-independent protected-resource configuration.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Driver) == "" {
		return errors.New("authz: driver is required")
	}
	for field, raw := range map[string]string{"resource": c.Resource, "issuer": c.Issuer} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("authz: %s must be an absolute canonical HTTPS URL", field)
		}
		if field == "issuer" && u.RawQuery != "" {
			return errors.New("authz: issuer must not contain a query")
		}
	}
	supported := make(map[string]struct{}, len(c.Scopes))
	for _, scope := range c.Scopes {
		supported[scope] = struct{}{}
	}
	for _, scope := range append(append([]string(nil), c.Scopes...), c.RequiredScopes...) {
		if !validScopeToken(scope) || scope == "offline_access" {
			return fmt.Errorf("authz: invalid resource scope %q", scope)
		}
	}
	for _, scope := range c.RequiredScopes {
		if _, ok := supported[scope]; !ok {
			return fmt.Errorf("authz: required scope %q is not in the supported scope set", scope)
		}
	}
	if len(c.ContinuationKey) < 32 {
		return errors.New("authz: continuation key must contain at least 32 bytes")
	}
	return nil
}

// validScopeToken implements RFC 6749's scope-token ABNF:
// %x21 / %x23-5B / %x5D-7E.
func validScopeToken(scope string) bool {
	if scope == "" {
		return false
	}
	for i := 0; i < len(scope); i++ {
		b := scope[i]
		if b != 0x21 && (b < 0x23 || b > 0x5b) && (b < 0x5d || b > 0x7e) {
			return false
		}
	}
	return true
}
