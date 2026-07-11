package server

import (
	"context"
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

const (
	protocolVersionHeader    = "Mcp-Protocol-Version"
	statelessProtocolVersion = "2026-07-28"
)

// ProtocolMode selects the MCP HTTP lifecycle accepted by HTTPHandler.
type ProtocolMode uint8

const (
	// Legacy serves the session-based 2025-11-25 lifecycle.
	Legacy ProtocolMode = iota
	// Stateless20260728 serves only the stateless 2026-07-28 lifecycle.
	Stateless20260728
	// Dual selects a lifecycle per request from Mcp-Protocol-Version.
	Dual
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
	// ProtocolMode selects the accepted MCP HTTP lifecycle. The zero value,
	// Legacy, preserves the pre-2026 behavior. Dual dispatches by the declared
	// Mcp-Protocol-Version header before the JSON-RPC body is decoded.
	ProtocolMode ProtocolMode
	// Stateless serves each request with a fresh, default-initialized session
	// and no Mcp-Session-Id validation. Deprecated: use ProtocolMode:
	// Stateless20260728 or Dual. It remains for source compatibility.
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

	// DNS-rebinding (localhost) protection is on-by-default in the SDK and
	// disabled via DisableLocalhostProtection. Dockyard maps its positive-sense
	// flag onto that negative knob explicitly, so a future SDK default flip
	// cannot silently change behaviour (D-040).
	mode, err := opts.protocolMode()
	if err != nil {
		return nil, err
	}
	// Stateless predates ProtocolMode and accepted legacy frames without a
	// protocol header. Preserve that deprecated behavior; only the explicit
	// modern mode requires its version header before decoding.
	legacyStateless := opts != nil && opts.Stateless && opts.ProtocolMode == Legacy
	newSDKHandler := func(stateless bool) http.Handler {
		h := http.Handler(mcpsdk.NewStreamableHTTPHandler(getServer, &mcpsdk.StreamableHTTPOptions{
			Stateless:                  stateless,
			Logger:                     s.log,
			DisableLocalhostProtection: !sec.DNSRebindingProtection,
		}))
		if stateless {
			// The SDK creates a temporary ServerSession for a modern request so
			// its normal handler APIs still work. Mark the request before it
			// reaches the SDK so handler edges do not publish that temporary ID.
			return statelessRequestMiddleware(h)
		}
		if s.tasksMount != nil {
			// Tasks stays on the legacy lifecycle until Phase 33 migrates its
			// wire layer, so it cannot interpret a modern stateless frame.
			return s.tasksMount.HTTPMiddleware(h)
		}
		return h
	}

	var handler http.Handler
	switch mode {
	case Legacy:
		handler = newSDKHandler(false)
	case Stateless20260728:
		handler = newSDKHandler(true)
		if !legacyStateless {
			handler = protocolVersionMiddleware(handler)
		}
	case Dual:
		legacy := newSDKHandler(false)
		modern := newSDKHandler(true)
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			version := r.Header.Get(protocolVersionHeader)
			switch version {
			case "":
				legacy.ServeHTTP(w, r)
			case statelessProtocolVersion:
				modern.ServeHTTP(w, r)
			default:
				if version > statelessProtocolVersion {
					http.Error(w, "unsupported MCP protocol version "+version+"; supported versions: 2025-11-25, 2026-07-28", http.StatusBadRequest)
					return
				}
				legacy.ServeHTTP(w, r)
			}
		})
	}

	// The middleware chain is layered inner-to-outer so the OUTERMOST check
	// runs first: cross-origin (CSRF) protection is the outer layer, then
	// Content-Type verification, then the optional Tasks transport mount, then
	// the SDK handler. A cross-site request is therefore rejected as
	// cross-origin regardless of method or body shape — the CSRF verdict does
	// not depend on a body-shape check or routing downstream of it.
	h := handler
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

	// W3C TraceContext extractor (R5 — depth-audit remediation; D-122). It
	// reads the inbound `traceparent` header — purely read-only — and stamps
	// the parsed parent SpanContext onto the request context via
	// obs.WithInboundTrace. Handler-edge call sites then mint their unit-of-
	// work span via obs.NewTraceFromContext, which inherits the caller's
	// trace when one is present. The middleware sits OUTERMOST so the parent
	// context reaches every handler downstream — including a CSRF-rejected
	// request's no-op pass-through, which carries no security risk because
	// extraction never authorises anything and a rejected request never
	// emits an obs/v1 event. Unconditional: the propagator runs even with
	// CrossOriginProtection off; it adds no cost when no traceparent is
	// present.
	h = traceparentMiddleware(h)

	return h, nil
}

func (o *HTTPOptions) protocolMode() (ProtocolMode, error) {
	if o == nil {
		return Legacy, nil
	}
	if o.ProtocolMode > Dual {
		return 0, fmt.Errorf("dockyard/runtime/server: unsupported protocol mode %d", o.ProtocolMode)
	}
	if o.Stateless {
		if o.ProtocolMode == Legacy {
			return Stateless20260728, nil
		}
		if o.ProtocolMode != Stateless20260728 {
			return 0, errors.New("dockyard/runtime/server: Stateless cannot be combined with ProtocolMode Dual")
		}
	}
	return o.ProtocolMode, nil
}

// protocolVersionMiddleware rejects missing or unsupported versions before the
// stateless SDK handler decodes JSON-RPC.
func protocolVersionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get(protocolVersionHeader) != statelessProtocolVersion {
			http.Error(w, "Mcp-Protocol-Version must be 2026-07-28 for the stateless lifecycle", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statelessRequestKey struct{}

func statelessRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), statelessRequestKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isStatelessRequest(ctx context.Context) bool {
	v, _ := ctx.Value(statelessRequestKey{}).(bool)
	return v
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
