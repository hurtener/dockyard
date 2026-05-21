package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	handler := func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := def.URI
		if req != nil && req.Params != nil && req.Params.URI != "" {
			uri = req.Params.URI
		}
		content, err := fn(ctx, uri)
		if err != nil {
			return nil, err
		}
		mime := content.MIMEType
		if mime == "" {
			mime = def.MIMEType
		}
		rc := &mcpsdk.ResourceContents{URI: uri, MIMEType: mime}
		if len(content.Blob) > 0 {
			rc.Blob = content.Blob
		} else {
			rc.Text = content.Text
		}
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

// Resources returns the URIs of registered resources, in registration order.
// The returned slice is a copy and safe for the caller to retain.
func (s *Server) Resources() []string {
	out := make([]string, len(s.resources))
	copy(out, s.resources)
	return out
}
