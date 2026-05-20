package protocolcodec

// This file holds the Dockyard domain types for the MCP Apps extension
// (io.modelcontextprotocol/ui) and the JSON shapes used to (un)marshal them.
// Spec snapshot: docs/specifications/mcp-apps-2026-01-26.mdx (SEP-1865).
//
// The exported `*Meta` types are Dockyard domain types — safe to pass around
// the codebase. The unexported `*wire` types are the raw `_meta` shapes and
// stay inside the seam.

// Visibility values for an Apps tool (`_meta.ui.visibility`). Per spec the
// default when the array is absent is both: ["model","app"].
const (
	// VisibilityModel makes a tool callable by the agent/LLM.
	VisibilityModel = "model"
	// VisibilityApp restricts a tool to same-server App-initiated calls.
	VisibilityApp = "app"
)

// AppsToolMeta is the Dockyard domain view of the MCP Apps metadata carried by
// a tool definition — the `_meta.ui` object on a `Tool` (brief 01 §2.3).
type AppsToolMeta struct {
	// ResourceURI is the `ui://` URI of the UI resource this tool renders
	// into. Empty means the tool declares no UI.
	ResourceURI string
	// Visibility is who may invoke the tool: any of [VisibilityModel] /
	// [VisibilityApp]. Empty slice means "unspecified"; per spec a host treats
	// that as ["model","app"]. Dockyard preserves it as-given and lets the
	// caller decide whether to default it.
	Visibility []string
}

// hasUI reports whether the tool meta carries any Apps information worth
// emitting.
func (m AppsToolMeta) hasUI() bool {
	return m.ResourceURI != "" || len(m.Visibility) > 0
}

// AppsCSP is the Dockyard domain view of `_meta.ui.csp` — the Content Security
// Policy domains a UI resource declares (brief 01 §2.5). All four lists map to
// CSP directives the host enforces; an empty/omitted list is the secure
// default (no external origins).
type AppsCSP struct {
	// ConnectDomains are origins for network requests (fetch/XHR/WebSocket) —
	// CSP `connect-src`.
	ConnectDomains []string
	// ResourceDomains are origins for static resources (scripts, images,
	// styles, fonts, media) — CSP `img-src`/`script-src`/`style-src`/etc.
	ResourceDomains []string
	// FrameDomains are origins for nested iframes — CSP `frame-src`.
	FrameDomains []string
	// BaseURIDomains are allowed document base URIs — CSP `base-uri`.
	BaseURIDomains []string
}

func (c AppsCSP) isZero() bool {
	return len(c.ConnectDomains) == 0 && len(c.ResourceDomains) == 0 &&
		len(c.FrameDomains) == 0 && len(c.BaseURIDomains) == 0
}

// AppsPermissions is the Dockyard domain view of `_meta.ui.permissions` — the
// sandbox iframe capabilities a UI resource requests. On the wire each granted
// permission is an empty object; here each is a bool (brief 01 §2.5).
type AppsPermissions struct {
	Camera         bool
	Microphone     bool
	Geolocation    bool
	ClipboardWrite bool
}

func (p AppsPermissions) isZero() bool {
	return !p.Camera && !p.Microphone && !p.Geolocation && !p.ClipboardWrite
}

// AppsResourceMeta is the Dockyard domain view of the MCP Apps metadata carried
// by a UI resource and, critically, by a `resources/read` response — the
// `_meta.ui` object on a `Resource` / resource-read content (brief 01 §2.2).
type AppsResourceMeta struct {
	// CSP is the Content Security Policy domain declaration.
	CSP AppsCSP
	// Permissions is the sandbox-capability request.
	Permissions AppsPermissions
	// Domain requests a stable dedicated sandbox origin. Host-dependent; the
	// seam carries it verbatim and never derives it (derivation lives behind
	// host profiles — RFC §7.5, D-012).
	Domain string
	// PrefersBorder is the visual-boundary preference. A nil pointer means the
	// resource declared none and lets the host decide; the spec recommends an
	// explicit value.
	PrefersBorder *bool
}

func (m AppsResourceMeta) isZero() bool {
	return m.CSP.isZero() && m.Permissions.isZero() &&
		m.Domain == "" && m.PrefersBorder == nil
}

// AppsExtensionCapability is the value Dockyard advertises (and a host
// advertises) for `capabilities.extensions["io.modelcontextprotocol/ui"]`
// (brief 01 §2.7). `MIMETypes` is REQUIRED by spec.
type AppsExtensionCapability struct {
	MIMETypes []string
}

// ---- wire shapes (raw `_meta` / capability JSON; stay inside the seam) ----

// appsUIToolWire is the value of the `_meta.ui` key on an Apps tool. Its field
// layout deliberately matches [AppsToolMeta] so the two are convertible.
type appsUIToolWire struct {
	ResourceURI string   `json:"resourceUri,omitempty"`
	Visibility  []string `json:"visibility,omitempty"`
}

type appsUIResourceWire struct {
	CSP           *appsCSPWire         `json:"csp,omitempty"`
	Permissions   *appsPermissionsWire `json:"permissions,omitempty"`
	Domain        string               `json:"domain,omitempty"`
	PrefersBorder *bool                `json:"prefersBorder,omitempty"`
}

type appsCSPWire struct {
	ConnectDomains  []string `json:"connectDomains,omitempty"`
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	FrameDomains    []string `json:"frameDomains,omitempty"`
	BaseURIDomains  []string `json:"baseUriDomains,omitempty"`
}

// appsPermissionsWire models the wire form where each granted permission is an
// empty JSON object (presence == requested).
type appsPermissionsWire struct {
	Camera         *struct{} `json:"camera,omitempty"`
	Microphone     *struct{} `json:"microphone,omitempty"`
	Geolocation    *struct{} `json:"geolocation,omitempty"`
	ClipboardWrite *struct{} `json:"clipboardWrite,omitempty"`
}

// appsExtensionCapabilityWire is the capability-block value.
type appsExtensionCapabilityWire struct {
	MIMETypes []string `json:"mimeTypes"`
}
