package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/obs"
)

// ResourceDef describes a resource to register on the server. URI and Name are
// required; the rest are hints surfaced to MCP hosts (RFC §5.3).
//
// A resource is a server-provided piece of content addressable by URI. Wave 4's
// MCP Apps work serves an App's HTML bundle as a resource under the ui:// scheme
// (RFC §7.1, brief 01); Phase 07 lands the typed registration surface so that
// layer composes the Dockyard runtime rather than reaching past it to the raw
// SDK (P3, RFC §5.4).
type ResourceDef struct {
	// URI is the resource's address. Required, and must be absolute (carry a
	// scheme), as the MCP spec requires.
	URI string
	// Name is the programmatic resource identifier. Required.
	Name string
	// Title is the human-readable display name. Optional.
	Title string
	// Description is a hint surfaced to the model. Optional.
	Description string
	// MIMEType is the resource's media type, if known. Optional.
	MIMEType string
	// Meta is the resource declaration's `_meta` object — the metadata a host
	// sees in resources/list. The Apps layer (runtime/apps, Phase 09) supplies
	// `_meta.ui` here; the runtime copies it verbatim and never inspects it
	// (P3, RFC §5.4). Note that the MCP Apps spec reads CSP/domain from the
	// resources/read *response*, so the choke point is ResourceContent.Meta —
	// this field carries the static declaration only (brief 01 §2.2).
	Meta map[string]any
}

func (d ResourceDef) validate() error {
	if d.URI == "" {
		return errors.New("dockyard/runtime/server: ResourceDef.URI is required")
	}
	if d.Name == "" {
		return errors.New("dockyard/runtime/server: ResourceDef.Name is required")
	}
	// The MCP spec requires a resource URI to be absolute (carry a scheme).
	// The SDK only panics on a URI that fails to parse at all; it tolerates a
	// scheme-less string, so Dockyard validates the scheme itself rather than
	// registering a spec-invalid resource (RFC §5.3).
	u, err := url.Parse(d.URI)
	if err != nil {
		return fmt.Errorf("dockyard/runtime/server: ResourceDef.URI %q is not a valid URI: %w", d.URI, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("dockyard/runtime/server: ResourceDef.URI %q must be absolute (carry a scheme)", d.URI)
	}
	return nil
}

// ResourceContent is the body of a resource read. It is the runtime-facing
// return type for a ResourceFunc: it carries either Text or Blob, never raw SDK
// structs, so the runtime surface stays free of protocol types (P3, RFC §5.4).
//
// Exactly one of Text or Blob is meaningful for a given resource; when both are
// set, Blob takes precedence.
type ResourceContent struct {
	// MIMEType is the media type of the content. When empty, the registered
	// ResourceDef.MIMEType is used.
	MIMEType string
	// Text is the textual content of the resource.
	Text string
	// Blob is the binary content of the resource. When non-empty it takes
	// precedence over Text.
	Blob []byte
	// Meta is the `_meta` object attached to this resource-read content entry.
	// It is the choke point the MCP Apps spec mandates: a host reads
	// `_meta.ui.csp` and `_meta.ui.domain` from the resources/read *response*,
	// not only the static resource declaration (brief 01 §2.2). The Apps layer
	// (runtime/apps, Phase 09) sets it through internal/protocolcodec; the
	// runtime copies it verbatim onto the read reply and never inspects it
	// (P3, RFC §5.4). A nil map yields a read reply with no `_meta`.
	Meta map[string]any
}

// ResourceFunc reads a resource. It receives the requested URI — useful when a
// handler is registered for a family of URIs — and returns the resource
// content or an error. A handler must never panic across the MCP boundary
// (AGENTS.md §5, §13).
type ResourceFunc func(ctx context.Context, uri string) (ResourceContent, error)

// AddResource registers a resource on the server. It must be called before Run.
// The URI must be absolute (carry a scheme); a duplicate URI is rejected.
//
// AddResource is a method (unlike AddTool) because a resource is not generic
// over typed contracts — its body is opaque content addressed by URI.
func (s *Server) AddResource(def ResourceDef, fn ResourceFunc) error {
	if s == nil {
		return errors.New("dockyard/runtime/server: AddResource on nil server")
	}
	if err := def.validate(); err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: resource %q has a nil handler", def.URI)
	}
	for _, existing := range s.resources {
		if existing == def.URI {
			return fmt.Errorf("dockyard/runtime/server: resource %q already registered", def.URI)
		}
	}

	// Adapt the Dockyard handler to the SDK's ResourceHandler shape. The SDK
	// reports a missing resource via ResourceNotFoundError; a handler that
	// returns an error surfaces it to the host.
	//
	// guardHandler wraps the app author's read handler so a panic on a live
	// resources/read becomes a typed error, never a process crash — the "no
	// panic across the MCP boundary" rule made a toolchain guarantee
	// (AGENTS.md §5, §13; D-053).
	handler := func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := def.URI
		if req != nil && req.Params != nil && req.Params.URI != "" {
			uri = req.Params.URI
		}
		// Emit the obs/v1 resource.read lifecycle (RFC §11.2, P2).
		endObs := s.rec.ResourceRead(ctx, obs.NewTrace(), uri)
		var content ResourceContent
		err := guardHandler(ctx, s.log, "resource", uri, func() error {
			var herr error
			content, herr = fn(ctx, uri)
			return herr
		})
		if err != nil {
			endObs("", 0, err)
			return nil, err
		}
		mime := content.MIMEType
		if mime == "" {
			mime = def.MIMEType
		}
		rc := &mcpsdk.ResourceContents{URI: uri, MIMEType: mime, Meta: cloneMeta(content.Meta)}
		if len(content.Blob) > 0 {
			rc.Blob = content.Blob
		} else {
			rc.Text = content.Text
		}
		endObs(mime, resourceBytes(content), nil)
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{rc}}, nil
	}

	// mcp.AddResource panics if the URI is invalid or not absolute. Recover so
	// a misdeclared resource surfaces as a Dockyard error, never a panic across
	// the boundary (AGENTS.md §13).
	if err := s.addResourceSafe(&mcpsdk.Resource{
		URI:         def.URI,
		Name:        def.Name,
		Title:       def.Title,
		Description: def.Description,
		MIMEType:    def.MIMEType,
		Meta:        cloneMeta(def.Meta),
	}, handler); err != nil {
		return fmt.Errorf("dockyard/runtime/server: register resource %q: %w", def.URI, err)
	}

	s.resources = append(s.resources, def.URI)
	return nil
}

func (s *Server) addResourceSafe(
	r *mcpsdk.Resource,
	h mcpsdk.ResourceHandler,
) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("invalid resource URI: %v", rec)
		}
	}()
	s.mcp.AddResource(r, h)
	return nil
}

// ResourceTemplateDef describes a resource template to register on the server.
// A resource template serves a *family* of resources addressed by an RFC 6570
// URI template rather than a single fixed URI — the shape the Apps layer
// (Phase 10) uses to register a `ui://` family without enumerating every member
// (RFC §5.1, brief 03 §2.2). URITemplate and Name are required; the rest are
// hints surfaced to MCP hosts (RFC §5.3).
type ResourceTemplateDef struct {
	// URITemplate is the RFC 6570 URI template the family is addressed by, e.g.
	// "ui://customer-health/{view}". Required, and must be absolute (carry a
	// scheme), as the MCP spec requires.
	URITemplate string
	// Name is the programmatic template identifier. Required.
	Name string
	// Title is the human-readable display name. Optional.
	Title string
	// Description is a hint surfaced to the model. Optional.
	Description string
	// MIMEType is the media type shared by every resource the template matches,
	// when they all share one. Optional.
	MIMEType string
	// Meta is the template declaration's `_meta` object — the metadata a host
	// sees in resources/templates/list. The Apps layer supplies `_meta.ui` here;
	// the runtime copies it verbatim and never inspects it (P3, RFC §5.4).
	Meta map[string]any
}

func (d ResourceTemplateDef) validate() error {
	if d.URITemplate == "" {
		return errors.New("dockyard/runtime/server: ResourceTemplateDef.URITemplate is required")
	}
	if d.Name == "" {
		return errors.New("dockyard/runtime/server: ResourceTemplateDef.Name is required")
	}
	// The SDK panics on a URI template that is not absolute (empty scheme); the
	// scheme prefix is the same in a template, so the scheme is checkable
	// without a full RFC 6570 parse. Reject a scheme-less template here so a
	// misdeclared template surfaces as a Dockyard error, never a panic.
	scheme := d.URITemplate
	if i := strings.IndexByte(scheme, ':'); i >= 0 {
		scheme = scheme[:i]
	} else {
		scheme = ""
	}
	if scheme == "" {
		return fmt.Errorf(
			"dockyard/runtime/server: ResourceTemplateDef.URITemplate %q must be absolute (carry a scheme)",
			d.URITemplate)
	}
	return nil
}

// AddResourceTemplate registers a resource template on the server — a family of
// resources addressed by an RFC 6570 URI template. It must be called before
// Run. The URI template must be absolute (carry a scheme); a duplicate template
// is rejected.
//
// It is consistent with AddResource: a typed ResourceTemplateDef, the same
// ResourceFunc handler shape (the handler receives the concrete URI a host
// requested, not the template), the same panic-recovered handler invocation,
// and no raw SDK struct on the surface (P3, RFC §5.4).
func (s *Server) AddResourceTemplate(def ResourceTemplateDef, fn ResourceFunc) error {
	if s == nil {
		return errors.New("dockyard/runtime/server: AddResourceTemplate on nil server")
	}
	if err := def.validate(); err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("dockyard/runtime/server: resource template %q has a nil handler", def.URITemplate)
	}
	for _, existing := range s.resourceTemplates {
		if existing == def.URITemplate {
			return fmt.Errorf(
				"dockyard/runtime/server: resource template %q already registered", def.URITemplate)
		}
	}

	// The handler receives the concrete URI the host requested — a template
	// serves a family, so def.URITemplate is not itself a readable URI. The
	// invocation is panic-recovered, exactly as AddResource's (D-053).
	handler := func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		var uri string
		if req != nil && req.Params != nil {
			uri = req.Params.URI
		}
		// Emit the obs/v1 resource.read lifecycle (RFC §11.2, P2).
		endObs := s.rec.ResourceRead(ctx, obs.NewTrace(), uri)
		var content ResourceContent
		err := guardHandler(ctx, s.log, "resource", uri, func() error {
			var herr error
			content, herr = fn(ctx, uri)
			return herr
		})
		if err != nil {
			endObs("", 0, err)
			return nil, err
		}
		mime := content.MIMEType
		if mime == "" {
			mime = def.MIMEType
		}
		rc := &mcpsdk.ResourceContents{URI: uri, MIMEType: mime, Meta: cloneMeta(content.Meta)}
		if len(content.Blob) > 0 {
			rc.Blob = content.Blob
		} else {
			rc.Text = content.Text
		}
		endObs(mime, resourceBytes(content), nil)
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{rc}}, nil
	}

	// mcp.AddResourceTemplate panics if the URI template is invalid or not
	// absolute. Recover so a misdeclared template surfaces as a Dockyard error,
	// never a panic across the boundary (AGENTS.md §13).
	if err := s.addResourceTemplateSafe(&mcpsdk.ResourceTemplate{
		URITemplate: def.URITemplate,
		Name:        def.Name,
		Title:       def.Title,
		Description: def.Description,
		MIMEType:    def.MIMEType,
		Meta:        cloneMeta(def.Meta),
	}, handler); err != nil {
		return fmt.Errorf(
			"dockyard/runtime/server: register resource template %q: %w", def.URITemplate, err)
	}

	s.resourceTemplates = append(s.resourceTemplates, def.URITemplate)
	return nil
}

func (s *Server) addResourceTemplateSafe(
	t *mcpsdk.ResourceTemplate,
	h mcpsdk.ResourceHandler,
) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("invalid resource URI template: %v", rec)
		}
	}()
	s.mcp.AddResourceTemplate(t, h)
	return nil
}

// ResourceTemplates returns the URI templates of registered resource templates,
// in registration order. The returned slice is a copy and safe for the caller
// to retain.
func (s *Server) ResourceTemplates() []string {
	out := make([]string, len(s.resourceTemplates))
	copy(out, s.resourceTemplates)
	return out
}

// Resources returns the URIs of registered resources, in registration order.
// The returned slice is a copy and safe for the caller to retain.
func (s *Server) Resources() []string {
	out := make([]string, len(s.resources))
	copy(out, s.resources)
	return out
}
