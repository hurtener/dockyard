package server

import (
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// IconTheme indicates the background an icon is designed for (SEP-973). The
// empty value means the icon is theme-agnostic.
type IconTheme string

const (
	// IconThemeLight marks an icon designed for a light background.
	IconThemeLight IconTheme = "light"
	// IconThemeDark marks an icon designed for a dark background.
	IconThemeDark IconTheme = "dark"
)

// Icon is a server branding icon advertised in the MCP handshake's serverInfo
// (SEP-973). A host MAY render it next to the server's name; rendering is
// client-dependent, so an icon is a hint, never a guarantee. Dockyard keeps its
// own Icon type rather than exposing the SDK's mcp.Icon so the runtime-facing
// API never leaks a raw protocol struct (RFC §5.4, P3).
type Icon struct {
	// Src is a URI pointing at the icon: an https:// URL to an image, or a
	// data:image/ URI with base64-encoded image bytes. Required; validated on the
	// raw value, so surrounding whitespace and non-image data: URIs are rejected.
	// A host that cannot reach the URL (auth-gated, hotlink-blocked, or
	// non-public) will fall back to a generic icon, so prefer a publicly
	// reachable https:// URL or an inline data:image/ URI.
	Src string
	// MIMEType is the icon's media type (for example "image/png"). Optional but
	// recommended when Src is an https:// URL whose type may be generic. Prefer
	// PNG or SVG.
	MIMEType string
	// Sizes lists the pixel sizes the icon is available in — for example
	// {"48x48"}, {"48x48","96x96"}, or {"any"} for a scalable format like SVG.
	// Optional.
	Sizes []string
	// Theme declares the background the icon suits (IconThemeLight or
	// IconThemeDark). Optional; empty means theme-agnostic. Provide a light and a
	// dark icon as two entries so a host can pick per its active theme.
	Theme IconTheme
}

func (i Icon) validate() error {
	if strings.TrimSpace(i.Src) == "" {
		return fmt.Errorf("dockyard/runtime/server: Icon.Src is required")
	}
	// A src is either an https:// URL or a data:image/ URI, checked on the raw
	// value (not trimmed) so a whitespace-padded src — which would ship a broken
	// URL — is rejected rather than silently accepted. http:// and other schemes
	// are rejected (a mixed-content icon is dropped by most hosts); a non-image
	// data: URI is rejected because an icon is an image.
	if !validIconSrc(i.Src) {
		return fmt.Errorf("dockyard/runtime/server: Icon.Src %q must be an https:// URL or a data:image/ URI", i.Src)
	}
	switch i.Theme {
	case "", IconThemeLight, IconThemeDark:
	default:
		return fmt.Errorf("dockyard/runtime/server: Icon.Theme %q must be %q or %q", i.Theme, IconThemeLight, IconThemeDark)
	}
	return nil
}

// validIconSrc reports whether src is an acceptable icon source: an https:// URL
// or a data:image/ URI. It checks the raw value so surrounding whitespace is
// rejected, and matches the data scheme case-insensitively (RFC 2397 media types
// are case-insensitive).
func validIconSrc(src string) bool {
	if strings.HasPrefix(src, "https://") {
		return true
	}
	const dataImg = "data:image/"
	return len(src) >= len(dataImg) && strings.EqualFold(src[:len(dataImg)], dataImg)
}

// sdkIcons maps Dockyard's Icons onto the SDK's mcp.Icon for the legacy
// lifecycle, where the SDK serialises Implementation.Icons into initialize's
// serverInfo automatically. It returns nil for an empty slice so serverInfo
// omits the icons key entirely.
func sdkIcons(icons []Icon) []mcpsdk.Icon {
	if len(icons) == 0 {
		return nil
	}
	out := make([]mcpsdk.Icon, len(icons))
	for i, ic := range icons {
		out[i] = mcpsdk.Icon{
			Source:   ic.Src,
			MIMEType: ic.MIMEType,
			Sizes:    ic.Sizes,
			Theme:    mcpsdk.IconTheme(ic.Theme),
		}
	}
	return out
}

// codecIcons maps Dockyard's Icons onto the protocolcodec wire shape for the
// modern 2026-07-28 lifecycle, where Dockyard hand-builds serverInfo in the
// response _meta (protocolcodec is the only package that owns the wire shape —
// RFC §5.4, P3).
func codecIcons(icons []Icon) []protocolcodec.Icon {
	if len(icons) == 0 {
		return nil
	}
	out := make([]protocolcodec.Icon, len(icons))
	for i, ic := range icons {
		out[i] = protocolcodec.Icon{
			Src:      ic.Src,
			MIMEType: ic.MIMEType,
			Sizes:    ic.Sizes,
			Theme:    string(ic.Theme),
		}
	}
	return out
}
