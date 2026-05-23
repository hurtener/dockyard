package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hurtener/dockyard/internal/protocolcodec"
	"github.com/hurtener/dockyard/runtime/obs"
	"github.com/hurtener/dockyard/runtime/server"
)

// uiScheme is the required URI scheme of an MCP App resource (brief 01 §2.2).
const uiScheme = "ui://"

// Visibility values for an Apps tool (_meta.ui.visibility). The default when the
// array is absent is both — a host treats an omitted visibility as
// ["model","app"] (brief 01 §2.3).
const (
	// VisibilityModel makes a tool callable by the agent/LLM.
	VisibilityModel = protocolcodec.VisibilityModel
	// VisibilityApp restricts a tool to same-server App-initiated calls — the
	// standard pattern for UI-only actions that should not pollute the model's
	// tool list (brief 01 §2.3).
	VisibilityApp = protocolcodec.VisibilityApp
)

// ErrInvalidApp is returned (wrapped) when an App declaration is malformed.
var ErrInvalidApp = errors.New("dockyard/runtime/apps: invalid App")

// App is a server-side MCP App: a ui:// resource carrying an HTML bundle plus
// the host-facing _meta.ui metadata served on every resources/read of it
// (RFC §7.1). It is the unit apps.Register installs.
type App struct {
	// URI is the ui:// resource URI the App is served under. Required, and must
	// use the ui:// scheme (brief 01 §2.2).
	URI string
	// Name is the programmatic resource identifier. Required.
	Name string
	// Title is the human-readable display name. Optional.
	Title string
	// Description is a hint surfaced to the model. Optional.
	Description string
	// HTML is the App's HTML document — the built single-file Svelte bundle.
	// Required. Served as the text body of the resources/read response.
	HTML []byte
	// CSP is the App's Content-Security-Policy opt-out. The zero value is the
	// secure deny-by-default policy — RFC §7.4.
	CSP CSP
	// Permissions is the App's sandbox-capability request. The zero value
	// requests none.
	Permissions Permissions
	// Domain is the App's host-agnostic domain *label* — a request for a stable
	// dedicated sandbox origin. Dockyard does not carry it verbatim: it is the
	// input to host-profile derivation, which produces the concrete
	// _meta.ui.domain value on the resources/read response (RFC §7.5, D-062).
	// An empty Domain requests no dedicated origin and omits _meta.ui.domain.
	Domain string
	// HostProfile is the host id selecting the host profile whose derivation
	// function turns the Domain label into the concrete _meta.ui.domain origin.
	// An empty value selects the default verbatim profile — the Phase 09
	// behaviour (D-049). A signing host's id selects its derived origin form.
	// Host ids and their derivations live behind the host-profile seam
	// (hostprofile.go); the Apps core names no host (RFC §7.5, D-062).
	HostProfile string
	// ServerURL is the MCP server URL the dedicated origin is derived from. It
	// is required when Domain is set and HostProfile selects a signing host;
	// the default verbatim profile ignores it.
	ServerURL string
	// PrefersBorder is the App's visual-boundary preference. A nil pointer
	// declares none and lets the host decide.
	PrefersBorder *bool
}

func (a App) validate() error {
	if a.URI == "" {
		return fmt.Errorf("%w: URI is required", ErrInvalidApp)
	}
	if !strings.HasPrefix(a.URI, uiScheme) || a.URI == uiScheme {
		return fmt.Errorf("%w: URI %q must use the %q scheme", ErrInvalidApp, a.URI, uiScheme)
	}
	if a.Name == "" {
		return fmt.Errorf("%w: Name is required for App %q", ErrInvalidApp, a.URI)
	}
	if len(a.HTML) == 0 {
		return fmt.Errorf("%w: HTML is required for App %q", ErrInvalidApp, a.URI)
	}
	return nil
}

// normalizeMeta re-marshals a protocolcodec-produced Meta map into a plain,
// fully JSON-shaped map[string]any. protocolcodec stores typed wire structs as
// _meta values (e.g. _meta.ui is an appsUIToolWire); normalizing here means a
// caller sees the same plain JSON shape whether the _meta is inspected
// in-process or after a round-trip over the wire — and the runtime never has to
// reason about a protocolcodec-internal type. A nil/empty map stays nil.
func normalizeMeta(m protocolcodec.Meta) (map[string]any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// resourceMeta builds the resources/read response _meta map for the App, via
// internal/protocolcodec. It is the single choke point through which every
// resource-read reply gets its _meta.ui (brief 01 §2.2, RFC §7.1).
//
// The encoded policy is always present and correct: when the App declares no
// CSP, no permissions, no domain and no border preference the _meta.ui object
// is omitted entirely, which a host reads as the deny-by-default policy — zero
// external origins (RFC §7.4, brief 01 §2.5). When the App declares any of
// them, _meta.ui carries exactly those fields.
func (a App) resourceMeta() (map[string]any, error) {
	// _meta.ui.domain is auto-derived through the pluggable host-profile seam:
	// the App declares a host-agnostic Domain label, the host profile derives
	// the concrete dedicated origin (RFC §7.5, D-062). The core never names a
	// host — derivation lives behind DerivedDomain.
	domain, err := DerivedDomain(a.HostProfile, a.Domain, a.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/apps: derive domain for %q: %w", a.URI, err)
	}
	codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)
	meta, err := codec.EncodeAppsResourceMeta(nil, protocolcodec.AppsResourceMeta{
		CSP:           a.CSP.toCodec(),
		Permissions:   a.Permissions.toCodec(),
		Domain:        domain,
		PrefersBorder: a.PrefersBorder,
	})
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/apps: encode resource _meta for %q: %w", a.URI, err)
	}
	out, err := normalizeMeta(meta)
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/apps: normalize resource _meta for %q: %w", a.URI, err)
	}
	return out, nil
}

// Register installs app as a ui:// resource on s (RFC §7.1). The resource is
// served with MIME type text/html;profile=mcp-app and every resources/read
// reply carries the App's _meta.ui — CSP, permissions, domain, prefersBorder —
// built through internal/protocolcodec.
//
// Register must be called before the server runs. It returns a typed error
// (wrapping ErrInvalidApp) on a malformed App rather than panicking.
//
// Register does not gate on the host advertising the Apps extension: a non-Apps
// host still gets the resource as a plain MCP resource and the App's tools work
// as plain tools (RFC §7.5). To link a tool to this App, pass ToolMetaFor's
// result as server.ToolDef.Meta when registering the tool.
func Register(s *server.Server, app App) error {
	if s == nil {
		return errors.New("dockyard/runtime/apps: Register on nil server")
	}
	if err := app.validate(); err != nil {
		return err
	}
	meta, err := app.resourceMeta()
	if err != nil {
		return err
	}

	// The resources/read response is the choke point: a host reads _meta.ui.csp
	// and _meta.ui.domain from the read reply, not only the static declaration
	// (brief 01 §2.2). The handler returns the same _meta on every read; it is
	// host-independent, so concurrent reads are safe (graceful degradation
	// needs no per-host branching — RFC §7.5).
	//
	// The read handler also emits an obs/v1 app.load event (RFC §11.2, P2):
	// serving a ui:// App resource to a host is exactly the app-load signal the
	// observability protocol carries (brief 05 §2.5, §3.2). The runtime EMITS;
	// the inspector consumes — nothing reads apps internals to observe.
	html := app.HTML
	rec := s.Recorder()
	appURI := app.URI
	appName := app.Name
	read := func(ctx context.Context, _ string) (server.ResourceContent, error) {
		// The app.load event is minted inside a resources/read invocation,
		// which runtime/server now opens with obs.WithSpan (R5; D-121). Use
		// ChildOrNewTrace so app.load is a CHILD of the enclosing resource.read
		// — same trace id, parent span set — rather than an unrelated fresh
		// trace. With no enclosing span (e.g. an in-process call site that
		// bypassed AddResource) it cleanly falls back to a fresh root trace.
		rec.AppLoad(ctx, obs.ChildOrNewTrace(ctx), obs.AppLoadPayload{
			AppID:       appName,
			ResourceURI: appURI,
			MIME:        MIMETypeApp,
			Bytes:       len(html),
		})
		return server.ResourceContent{
			MIMEType: MIMETypeApp,
			Text:     string(html),
			Meta:     meta,
		}, nil
	}

	def := server.ResourceDef{
		URI:         app.URI,
		Name:        app.Name,
		Title:       app.Title,
		Description: app.Description,
		MIMEType:    MIMETypeApp,
		// The static resource declaration also carries _meta.ui so a host
		// inspecting resources/list sees it; the read response above is the
		// authoritative choke point the spec mandates (brief 01 §2.2).
		Meta: meta,
	}
	if err := s.AddResource(def, read); err != nil {
		return fmt.Errorf("dockyard/runtime/apps: register App %q: %w", app.URI, err)
	}
	return nil
}

// ToolLink links a tool definition to a ui:// resource — the tool side of an
// MCP App (brief 01 §2.3). Pass ToolMetaFor(link) as server.ToolDef.Meta.
type ToolLink struct {
	// ResourceURI is the ui:// URI of the App resource the tool renders into.
	// Required, and must use the ui:// scheme.
	ResourceURI string
	// Visibility is who may invoke the tool: any of VisibilityModel /
	// VisibilityApp. An empty slice means "unspecified" — a host treats that as
	// ["model","app"]. Set ["app"] for a UI-only action tool (brief 01 §2.3).
	Visibility []string
}

func (l ToolLink) validate() error {
	if l.ResourceURI == "" {
		return fmt.Errorf("%w: ToolLink.ResourceURI is required", ErrInvalidApp)
	}
	if !strings.HasPrefix(l.ResourceURI, uiScheme) || l.ResourceURI == uiScheme {
		return fmt.Errorf("%w: ToolLink.ResourceURI %q must use the %q scheme",
			ErrInvalidApp, l.ResourceURI, uiScheme)
	}
	for _, v := range l.Visibility {
		if v != VisibilityModel && v != VisibilityApp {
			return fmt.Errorf("%w: ToolLink.Visibility %q must be %q or %q",
				ErrInvalidApp, v, VisibilityModel, VisibilityApp)
		}
	}
	return nil
}

// ToolMetaFor builds the tool-definition _meta map carrying _meta.ui for link
// (RFC §7.1, brief 01 §2.3). The encoding goes through internal/protocolcodec,
// which emits only the nested {resourceUri, visibility} form and never the
// deprecated flat tool-UI _meta form (P3, brief 01 §2.3).
//
// Use it when registering the App's tool:
//
//	meta, err := apps.ToolMetaFor(apps.ToolLink{ResourceURI: "ui://x/main"})
//	server.AddToolWithSchemas(s, server.ToolDef{Name: "x", Meta: meta}, ...)
//
// A host that does not support the Apps extension simply ignores _meta.ui —
// the tool still works as a plain MCP tool (RFC §7.5).
func ToolMetaFor(link ToolLink) (map[string]any, error) {
	if err := link.validate(); err != nil {
		return nil, err
	}
	codec := protocolcodec.CodecFor(protocolcodec.VersionApps20260126)
	meta, err := codec.EncodeAppsToolMeta(nil, protocolcodec.AppsToolMeta{
		ResourceURI: link.ResourceURI,
		Visibility:  link.Visibility,
	})
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/apps: encode tool _meta for %q: %w",
			link.ResourceURI, err)
	}
	out, err := normalizeMeta(meta)
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/apps: normalize tool _meta for %q: %w",
			link.ResourceURI, err)
	}
	return out, nil
}
