package server

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// jsonMediaType is the media type a streamable-HTTP MCP request body must carry
// on a POST: the JSON-RPC payload is application/json (MCP streamable-HTTP
// transport). contentTypeMiddleware rejects a POST whose body declares any
// other media type.
const jsonMediaType = "application/json"

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
	// ContentTypeVerification rejects a POST whose request-body Content-Type is
	// not the JSON media type (application/json) the MCP streamable-HTTP
	// transport mandates. Dockyard verifies this EXPLICITLY rather than relying
	// on whatever the linked go-sdk happens to enforce — SDK security defaults
	// have flipped between releases (AGENTS.md §7, D-112). Applied as Dockyard
	// middleware wrapping the SDK handler; a violating POST gets a 415.
	ContentTypeVerification bool
	// TrustedOrigins are origins exempted from cross-origin protection — for
	// example a known App host. Each must be a scheme://host[:port] origin.
	// Ignored when CrossOriginProtection is false.
	TrustedOrigins []string
}

// DefaultHTTPSecurity returns the recommended secure posture for an HTTP
// deployment: DNS-rebinding protection, cross-origin protection, and
// Content-Type verification all ON, with no trusted-origin exemptions. This is
// the value a Dockyard app uses unless it has a specific reason to relax a
// protection.
func DefaultHTTPSecurity() HTTPSecurity {
	return HTTPSecurity{
		DNSRebindingProtection:  true,
		CrossOriginProtection:   true,
		ContentTypeVerification: true,
	}
}

// isZero reports whether s carries no explicit posture — every protection off
// and no trusted origins. It cannot use == because HTTPSecurity holds a slice.
func (s HTTPSecurity) isZero() bool {
	return !s.DNSRebindingProtection &&
		!s.CrossOriginProtection &&
		!s.ContentTypeVerification &&
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
// wrapping the SDK handler (D-041); Content-Type verification, when enabled,
// is applied as Dockyard middleware (D-112) — both are Dockyard's own posture,
// never delegated to the linked SDK.
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

	// The middleware chain is layered inner-to-outer so the OUTERMOST check
	// runs first: cross-origin (CSRF) protection is the outer layer, then
	// Content-Type verification, then the optional Tasks transport mount, then
	// the SDK handler. A cross-site request is therefore rejected as
	// cross-origin regardless of method or body shape — the CSRF verdict does
	// not depend on a body-shape check or routing downstream of it.
	var h http.Handler = handler
	if s.tasksMount != nil {
		// When a Tasks engine is attached (D-108/109/110), wrap the SDK handler
		// with the Tasks transport mount so tasks/* JSON-RPC frames are
		// intercepted ahead of the SDK server and the capabilities.tasks block
		// is injected into the initialize handshake (RFC §8.2). The mount sits
		// INSIDE the security middleware applied below — DNS-rebinding,
		// Content-Type, and cross-origin protection all wrap the mount, so a
		// tasks/* frame is still subject to the explicit HTTPSecurity posture
		// and the mount cannot weaken it (CLAUDE.md §7).
		h = s.tasksMount.HTTPMiddleware(h)
	}
	if sec.ContentTypeVerification {
		// Content-Type verification as Dockyard middleware — set explicitly,
		// never inherited from an SDK default (AGENTS.md §7, D-112). A
		// wrong-Content-Type POST is rejected before it reaches the Tasks
		// mount or the SDK.
		h = contentTypeMiddleware(h)
	}
	if sec.CrossOriginProtection {
		// Cross-origin (CSRF) protection as net/http middleware — the SDK's own
		// CrossOriginProtection field is deprecated in v1.6.0 in favour of this
		// (D-041). Also covers Origin verification: CrossOriginProtection
		// rejects non-safe cross-origin requests by Origin/Sec-Fetch-Site. It is
		// the outer layer so a CSRF rejection takes precedence over the
		// Content-Type check.
		cop := http.NewCrossOriginProtection()
		for _, origin := range sec.TrustedOrigins {
			if err := cop.AddTrustedOrigin(origin); err != nil {
				return nil, fmt.Errorf("dockyard/runtime/server: trusted origin %q: %w", origin, err)
			}
		}
		// Wrap h (the SDK handler, plus the Tasks mount and Content-Type check
		// where attached), not the raw SDK handler — cross-origin protection
		// must sit OUTSIDE the Tasks mount so a tasks/* frame is CSRF-checked
		// before the mount answers it.
		h = cop.Handler(h)
	}

	return h, nil
}

// contentTypeMiddleware rejects a POST whose request-body Content-Type is not
// the JSON media type the MCP streamable-HTTP transport mandates. It is part of
// the explicit HTTPSecurity posture (AGENTS.md §7, D-112): the check is
// Dockyard's own, not delegated to the linked go-sdk, whose security defaults
// have flipped between releases.
//
// Only POST carries a JSON-RPC request body; GET (the SSE stream) and DELETE
// (session teardown) have no body and are passed through untouched. A POST with
// a missing or non-JSON Content-Type gets 415 Unsupported Media Type.
func contentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !isJSONContentType(r.Header.Get("Content-Type")) {
			http.Error(w,
				"unsupported media type: MCP streamable-HTTP request body must be "+jsonMediaType,
				http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isJSONContentType reports whether a Content-Type header value declares the
// JSON media type. It parses the header so a charset parameter
// (application/json; charset=utf-8) is accepted and a missing or mismatched
// type is rejected.
func isJSONContentType(header string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, jsonMediaType)
}
