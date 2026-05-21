package server

import (
	"errors"
	"fmt"
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPSecurity is the explicit security posture for the streamable-HTTP
// transport (RFC §5.2, AGENTS.md §7). Every field is set deliberately by
// Dockyard rather than inherited from an SDK default: the SDK has flipped
// security-relevant defaults between releases — cross-origin protection was on
// in v1.4.1 and off again in v1.6.0 (brief 03 §2.3) — so a Dockyard HTTP
// deployment must never depend on whatever the linked SDK happens to default
// to. The zero value has all protections OFF and is deliberately *not* the
// recommended posture; use DefaultHTTPSecurity for the secure default.
type HTTPSecurity struct {
	// DNSRebindingProtection rejects requests arriving via a localhost address
	// whose Host header is non-localhost — the SDK's localhost-protection
	// guard against DNS-rebinding attacks. Dockyard expresses it as a
	// positive-sense flag and maps it explicitly onto the SDK's negative
	// DisableLocalhostProtection knob (D-040).
	DNSRebindingProtection bool
	// CrossOriginProtection rejects non-safe cross-origin browser requests
	// (CSRF protection). Applied as net/http.CrossOriginProtection middleware
	// wrapping the SDK handler — the SDK's own field is deprecated in v1.6.0
	// in favour of this approach (D-041).
	CrossOriginProtection bool
	// TrustedOrigins are origins exempted from cross-origin protection — for
	// example a known App host. Each must be a scheme://host[:port] origin.
	// Ignored when CrossOriginProtection is false.
	TrustedOrigins []string
}

// DefaultHTTPSecurity returns the recommended secure posture for an HTTP
// deployment: DNS-rebinding protection and cross-origin protection both ON,
// with no trusted-origin exemptions. This is the value a Dockyard app uses
// unless it has a specific reason to relax a protection.
func DefaultHTTPSecurity() HTTPSecurity {
	return HTTPSecurity{
		DNSRebindingProtection: true,
		CrossOriginProtection:  true,
	}
}

// isZero reports whether s carries no explicit posture — every protection off
// and no trusted origins. It cannot use == because HTTPSecurity holds a slice.
func (s HTTPSecurity) isZero() bool {
	return !s.DNSRebindingProtection &&
		!s.CrossOriginProtection &&
		len(s.TrustedOrigins) == 0
}

// HTTPOptions configures the streamable-HTTP transport. A nil *HTTPOptions is
// treated as the zero value with DefaultHTTPSecurity.
type HTTPOptions struct {
	// Security is the explicit HTTP security posture. The zero value of
	// HTTPSecurity has all protections OFF; HTTPHandler substitutes
	// DefaultHTTPSecurity when Security is the zero value, so an app that does
	// not opt out is secure by default.
	Security HTTPSecurity
	// ServerForRequest is the per-request server seam (the SDK's getServer
	// callback, RFC §5.2): it is invoked once per incoming HTTP request to
	// select the Server that handles it, enabling per-session or multi-tenant
	// wiring. When nil, every request is served by the receiver Server.
	ServerForRequest func(*http.Request) *Server
	// Stateless serves each request with a fresh, default-initialized session
	// and no Mcp-Session-Id validation. Off by default.
	Stateless bool
}

func (o *HTTPOptions) security() HTTPSecurity {
	if o == nil {
		return DefaultHTTPSecurity()
	}
	// The zero HTTPSecurity is "all off" — not a posture an app would choose
	// deliberately — so treat it as "use the secure default". An app that
	// genuinely wants a protection off sets the other flag(s) on, making the
	// value non-zero and its intent explicit.
	if o.Security.isZero() {
		return DefaultHTTPSecurity()
	}
	return o.Security
}

// HTTPHandler returns an http.Handler that serves the MCP protocol over the
// streamable-HTTP transport (RFC §5.2). Mount it on an *http.Server.
//
// Security is set explicitly from opts.Security (see HTTPSecurity); the
// returned handler never inherits an SDK security default. Cross-origin
// protection, when enabled, is applied as standard net/http middleware
// wrapping the SDK handler (D-041).
func (s *Server) HTTPHandler(opts *HTTPOptions) (http.Handler, error) {
	if s == nil {
		return nil, errors.New("dockyard/runtime/server: HTTPHandler on nil server")
	}
	sec := opts.security()

	getServer := func(*http.Request) *mcpsdk.Server { return s.mcp }
	if opts != nil && opts.ServerForRequest != nil {
		fn := opts.ServerForRequest
		getServer = func(r *http.Request) *mcpsdk.Server {
			ds := fn(r)
			if ds == nil {
				return s.mcp
			}
			return ds.mcp
		}
	}

	stateless := opts != nil && opts.Stateless

	// DNS-rebinding (localhost) protection is on-by-default in the SDK and
	// disabled via DisableLocalhostProtection. Dockyard maps its positive-sense
	// flag onto that negative knob explicitly, so a future SDK default flip
	// cannot silently change behaviour (D-040).
	handler := mcpsdk.NewStreamableHTTPHandler(getServer, &mcpsdk.StreamableHTTPOptions{
		Stateless:                  stateless,
		Logger:                     s.log,
		DisableLocalhostProtection: !sec.DNSRebindingProtection,
	})

	var h http.Handler = handler
	if sec.CrossOriginProtection {
		// Cross-origin (CSRF) protection as net/http middleware — the SDK's own
		// CrossOriginProtection field is deprecated in v1.6.0 in favour of this
		// (D-041). Also covers Origin verification: CrossOriginProtection
		// rejects non-safe cross-origin requests by Origin/Sec-Fetch-Site.
		cop := http.NewCrossOriginProtection()
		for _, origin := range sec.TrustedOrigins {
			if err := cop.AddTrustedOrigin(origin); err != nil {
				return nil, fmt.Errorf("dockyard/runtime/server: trusted origin %q: %w", origin, err)
			}
		}
		h = cop.Handler(handler)
	}

	return h, nil
}
