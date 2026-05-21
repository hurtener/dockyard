package apps

import "github.com/hurtener/dockyard/internal/protocolcodec"

// CSP is an App's Content-Security-Policy opt-out: the external origins its
// deny-by-default policy is widened to allow (RFC §7.4, brief 01 §2.5). The
// zero value is the secure default — no external origins — and a single-file
// HTML bundle needs nothing more, so the deny-by-default CSP just works.
//
// A host enforces the resulting CSP and may further restrict it, but never
// loosens it: an origin a host does not see declared here is denied.
type CSP struct {
	// Connect are origins the App may open network connections to
	// (fetch / XHR / WebSocket) — CSP connect-src.
	Connect []string
	// Resource are origins the App may load passive resources from — scripts,
	// styles, images, fonts, media.
	Resource []string
	// Frame are origins the App may embed in nested iframes — CSP frame-src.
	Frame []string
	// BaseURI are document base URIs the App may declare — CSP base-uri.
	BaseURI []string
}

// toCodec converts the runtime-facing CSP into the protocolcodec domain type.
func (c CSP) toCodec() protocolcodec.AppsCSP {
	return protocolcodec.AppsCSP{
		ConnectDomains:  c.Connect,
		ResourceDomains: c.Resource,
		FrameDomains:    c.Frame,
		BaseURIDomains:  c.BaseURI,
	}
}

// Permissions is an App's sandbox-capability request: the iframe permissions it
// asks the host to grant (RFC §7.4, brief 01 §2.5). Each is opt-in; the zero
// value requests none. A host may decline any of them.
type Permissions struct {
	Camera         bool
	Microphone     bool
	Geolocation    bool
	ClipboardWrite bool
}

func (p Permissions) toCodec() protocolcodec.AppsPermissions {
	return protocolcodec.AppsPermissions(p)
}
