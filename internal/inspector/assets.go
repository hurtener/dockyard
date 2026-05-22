package inspector

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// placeholderHTML is served at "/" when no built web/inspector frontend is
// embedded. It keeps the Go backend usable before `vite build` has run — the Go
// build never depends on the frontend being built (mirroring runtime/apps's
// Bundle, which tolerates an unbuilt dist/ tree).
const placeholderHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Dockyard Inspector</title></head>
<body><main>
<h1>Dockyard Inspector</h1>
<p>The inspector frontend has not been built yet.
Run <code>make web</code> (it builds <code>web/inspector</code>),
then rebuild the inspector.</p>
</main></body></html>`

// newMux builds the inspector's HTTP handler. The routes are all read-only —
// the inspector serves its UI, relays the obs/v1 stream and the JSON-RPC log,
// and exposes the attached server's identity. There is no mutating route: the
// inspector is never an arbitrary-execution proxy (RFC §12).
func newMux(opts Options, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// /api/info — the attached MCP server's read-only identity, for the
	// inspector PageHeader. JSON, content-free of any runtime internal.
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_ = json.NewEncoder(w).Encode(opts.ServerInfo)
	})

	// /api/obs/stream and /api/rpc/log — the obs/v1 relay and the JSON-RPC log.
	// When no Relay is configured the inspector still answers, with an empty
	// stream / empty log, so the UI's four-state empty state renders cleanly.
	relay := opts.Relay
	if relay == nil {
		relay = NewRelay("")
	}
	mux.Handle("GET /api/obs/stream", relay.streamHandler())
	mux.Handle("GET /api/rpc/log", relay.rpcLogHandler())

	// The web/inspector frontend (its built dist/ tree), or a placeholder.
	mux.Handle("/", frontendHandler(opts.Assets, log))

	return mux
}

// frontendHandler serves the embedded web/inspector frontend. When assets is
// nil or empty, it serves [placeholderHTML] so the backend is usable before the
// frontend is built. The handler is an SPA fallback: an unknown non-asset path
// serves index.html so client-side routing works.
func frontendHandler(assets fs.FS, log *slog.Logger) http.Handler {
	if !hasIndex(assets) {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(placeholderHTML))
		})
	}
	fileServer := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A request for an extant asset is served directly; anything else falls
		// back to index.html (SPA routing).
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" && fileExists(assets, clean) {
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, assets, log)
	})
}

// hasIndex reports whether assets carries a usable built frontend (an
// index.html at its root).
func hasIndex(assets fs.FS) bool {
	if assets == nil {
		return false
	}
	return fileExists(assets, "index.html")
}

// fileExists reports whether name is a regular file in assets.
func fileExists(assets fs.FS, name string) bool {
	if assets == nil {
		return false
	}
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

// serveIndex writes the frontend's index.html — the SPA entry document.
func serveIndex(w http.ResponseWriter, assets fs.FS, log *slog.Logger) {
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		log.Error("dockyard inspector: index.html read failed",
			slog.String("error", err.Error()))
		http.Error(w, "inspector frontend unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
