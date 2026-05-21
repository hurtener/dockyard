package apps

import "embed"

// distFS is the //go:embed all:dist pipeline made concrete (RFC §14, brief 06
// §2.2). In a generated Dockyard project a `web/embed.go` file carries exactly
// this directive — `//go:embed all:dist` — populating an embed.FS that
// NewBundle wraps; the same embed.FS then backs both the ui:// MCP resource
// handler and (Phase 22) the inspector's HTTP preview.
//
// runtime/apps demonstrates and tests the pattern against its own committed
// fixture tree (testdata/dist). The `all:` prefix is load-bearing: without it
// `//go:embed` skips files whose names start with `_` or `.` (brief 06 §2.2),
// which a multi-file Vite build can emit as hashed chunk names. The directive
// points at a path that exists in-repo, so `runtime/apps` always builds; a
// generated project whose `vite build` step has not run yet fails the build at
// its own directive — the clean, build-time failure RFC §14 calls for. The
// runtime-side analogue, an embed target that resolved but holds nothing, is
// Bundle.Validate / ErrEmptyBundle.
//
//go:embed all:testdata/dist
var distFS embed.FS

// EmbeddedBundle returns the Bundle backed by the in-repo embedded dist/ tree.
// It is the reference wiring a generated project mirrors: build the Bundle once
// from the //go:embed FS, Validate it, then back every ui:// resource from it.
func EmbeddedBundle() Bundle {
	return NewBundle(distFS, "testdata/dist")
}
