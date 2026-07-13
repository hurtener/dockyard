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
// Resource and Issuer must be canonical HTTPS URLs. DriverConfig is interpreted
// only by the selected driver.
type Config struct {
	Driver   string
	Resource string
	Issuer   string
	// Scopes are advertised as supported in protected-resource metadata.
	Scopes []string
	// RequiredScopes are required on every protected MCP operation.
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
	for _, scope := range append(append([]string(nil), c.Scopes...), c.RequiredScopes...) {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") || scope == "offline_access" {
			return fmt.Errorf("authz: invalid resource scope %q", scope)
		}
	}
	if len(c.ContinuationKey) < 32 {
		return errors.New("authz: continuation key must contain at least 32 bytes")
	}
	return nil
}
