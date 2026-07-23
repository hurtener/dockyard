# Server branding (icons & logos)

A Dockyard server can advertise a **logo, homepage, and description** in the MCP
handshake's `serverInfo` (SEP-973). A host MAY render your icon beside the
server's name in its UI. Rendering is **client-dependent** — `serverInfo.icons`
is a hint every host is free to ignore or fall back to a generic icon — so
providing icons is necessary but not sufficient for a logo to appear.

## Set branding on the server

Branding lives on `server.Info`. The live server emits it in `serverInfo` on
both the legacy `2025-11-25` and stateless `2026-07-28` lifecycles.

```go
import "github.com/hurtener/dockyard/runtime/server"

srv, err := server.New(server.Info{
	Name:        "customer-health",
	Title:       "Customer Health",
	Version:     "0.1.0",
	Description: "Customer health scores and account signals.",
	WebsiteURL:  "https://acme.example/customer-health",
	Icons: []server.Icon{
		{
			Src:      "https://acme.example/logo.png",
			MIMEType: "image/png",
			Sizes:    []string{"48x48", "96x96"},
			Theme:    server.IconThemeLight,
		},
		{
			// A data: URI inlines the bytes — no separate hosting or auth to
			// worry about.
			Src:   "data:image/svg+xml;base64,PHN2Zz4uLi48L3N2Zz4=",
			Theme: server.IconThemeDark,
		},
	},
}, opts)
```

Field rules (enforced by `server.New`):

- **`Icon.Src` is required** and must be an `https://` URL or a `data:image/…`
  URI (checked on the raw value, so no surrounding whitespace). An `http://`,
  otherwise-schemed, or non-image `data:` src is rejected — most hosts drop a
  mixed-content icon anyway, and an icon is an image.
- **`Icon.MIMEType`** is optional but recommended for an `https://` src (prefer
  PNG or SVG). **`Icon.Sizes`** lists available sizes (`["48x48"]`, `["any"]`
  for SVG). **`Icon.Theme`** is `IconThemeLight` or `IconThemeDark`; provide both
  as two entries so a host can pick per its active theme.
- **`WebsiteURL`** must be an absolute `https://` URL when set.

Set no branding and `serverInfo` is byte-for-byte what it was — the keys are
omitted entirely.

## Declare branding in the manifest too

`dockyard.app.yaml` carries the same branding declaratively, so tooling has a
record of it:

```yaml
name: customer-health
title: Customer Health
version: 0.1.0
description: Customer health scores and account signals.
website_url: https://acme.example/customer-health
icons:
  - src: https://acme.example/logo.png
    mime_type: image/png
    sizes: ["48x48", "96x96"]
    theme: light
  - src: https://acme.example/logo-dark.png
    mime_type: image/png
    theme: dark
```

`dockyard validate` checks the same rules (src scheme, theme, website URL).

::: warning The manifest is declarative — the server emits from `server.Info`
The runtime library never reads `dockyard.app.yaml` (it can't — the manifest
package is internal to Dockyard's own module). The manifest `icons` block is the
declarative record for tooling and review; the **live server advertises from
`server.Info`**. Keep the two in step. (D-203)
:::

## Why the logo still might not show

If your icon is valid and reachable but the host shows a generic icon, the gap
is on the client:

- **The host UI hasn't implemented `serverInfo.icons`.** SEP-973 is recent and
  rendering is optional — many clients ignore it today. This is not a server bug.
- **The icon URL isn't publicly reachable from the client** — behind auth, a
  hotlink block, or a private network. Prefer a public `https://` URL or a
  `data:` URI.
- **A generic/missing MIME type** — set `MIMEType` explicitly.
- **No `Title`** — some hosts label with `Title` (falling back to `Name`); set it
  so the branding has text alongside the icon.

Your `/favicon.ico` is unrelated: that's plain HTTP static serving, not the MCP
`serverInfo` channel a host reads.

## See also

- [Scaffold a server](/guides/contracts)
- [Validate + test](/guides/validate)
- [OAuth protected resource](/guides/oauth-protected-resource)
