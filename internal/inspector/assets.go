package inspector

import (
	"context"
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

	// /api/verdicts — the read-only Verdicts panel source (contract-drift,
	// schema-validation, spec-compliance). When no source is configured the
	// endpoint answers with an empty array so the UI's four-state empty state
	// renders cleanly. The handler re-runs the checks per request — the
	// verdicts are never a stale cache.
	mux.HandleFunc("GET /api/verdicts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		verdicts := []Verdict{}
		if opts.Verdicts != nil {
			if v := opts.Verdicts(); v != nil {
				verdicts = v
			}
		}
		_ = json.NewEncoder(w).Encode(verdicts)
	})

	// /api/contracts — the read-only generated-contract source the fixture
	// switcher derives its fixtures from (RFC §12, §6 — P1). When no source is
	// configured the endpoint answers with an empty array so the Fixtures /
	// Tools panels render their four-state empty state.
	mux.HandleFunc("GET /api/contracts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		if opts.Contracts != nil {
			if raw := opts.Contracts(); len(raw) > 0 {
				_, _ = w.Write(raw)
				return
			}
		}
		_, _ = w.Write([]byte("[]"))
	})

	// /api/fixtures — the read-only on-disk fixture loader (Phase 24, D-126).
	// When the inspector was attached with --dir <project>, the loader reads
	// <project>/fixtures/<tool>/<kind>.json and surfaces them to the FixturesPanel.
	// The frontend prefers these real-data fixtures over the schema-derived
	// synthetic ones the Phase 23 switcher ships; an unattached or fixture-less
	// project answers with an empty array and the switcher degrades cleanly.
	mux.HandleFunc("GET /api/fixtures", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		fixtures := []ProjectFixture{}
		if opts.Fixtures != nil {
			loaded, err := opts.Fixtures()
			if err != nil {
				log.WarnContext(context.Background(), "dockyard inspector: fixtures load failed",
					slog.String("error", err.Error()))
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if len(loaded) > 0 {
				fixtures = loaded
			}
		}
		_ = json.NewEncoder(w).Encode(fixtures)
	})

	// /api/tools/invoke — the operator-initiated tools/call surface (RFC §12,
	// P4; D-131). The inspector frontend POSTs `{tool, arguments}`; this
	// handler opens a short-lived MCP client session against the attached
	// server, calls `tools/call`, and returns the result. The endpoint is
	// localhost-only via the listener's [requireLoopback] gate; the operator
	// is the one driving the write through the UI. A detached inspector (no
	// Invoker configured) answers 503 so the frontend surfaces an honest
	// "no server attached" error. A transport-level failure (connect, RPC)
	// answers 502 with a typed JSON message. A *tool-level* error (the tool
	// returned `isError: true`) is a successful RPC: HTTP 200 with the
	// response carrying IsError=true, so the inspector renders the error
	// surface without conflating it with a transport failure.
	mux.HandleFunc("POST /api/tools/invoke", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		if opts.Invoker == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "inspector is detached — no MCP server attached to invoke tools against",
			})
			return
		}

		var req InvokeRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid tools/invoke body: " + err.Error(),
			})
			return
		}
		if req.Tool == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "tools/invoke: `tool` is required",
			})
			return
		}

		resp, err := opts.Invoker(r.Context(), req)
		if err != nil {
			log.WarnContext(r.Context(), "dockyard inspector: tools/invoke failed",
				slog.String("tool", req.Tool),
				slog.String("error", err.Error()))
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// /api/tasks/elicitation — the operator-initiated elicitation-response
	// surface (Phase 25 / D-134). The inspector frontend POSTs the App's
	// elicitation-response (the user's "Approve" / "Reject" reply); this
	// handler dispatches to the configured Elicitor, which opens a
	// short-lived MCP-client-style POST and calls `tasks/result` against the
	// attached server. The endpoint is localhost-only via the listener's
	// [requireLoopback] gate; the operator is the one driving the write
	// through the App's button. A detached inspector (no Elicitor
	// configured) answers 503. A transport-level failure answers 502 with a
	// typed JSON message. A server-side refusal (the JSON-RPC envelope
	// carries an `error` block) is a successful RPC: HTTP 200 with
	// Delivered=false + the server's error message, mirroring the
	// `IsError`-as-200 pattern D-131 set for tools/invoke.
	mux.HandleFunc("POST /api/tasks/elicitation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		if opts.Elicitor == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "inspector is detached — no MCP server attached to deliver elicitations to",
			})
			return
		}

		var req ElicitationRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid tasks/elicitation body: " + err.Error(),
			})
			return
		}
		if req.TaskID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "tasks/elicitation: `taskId` is required",
			})
			return
		}

		resp, err := opts.Elicitor(r.Context(), req)
		if err != nil {
			log.WarnContext(r.Context(), "dockyard inspector: tasks/elicitation failed",
				slog.String("taskId", req.TaskID),
				slog.String("error", err.Error()))
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// /api/apps — the read-only App-preview source. It performs a read-only
	// resources/list + resources/read of the attached server's ui:// resources
	// (RFC §12 line 711 — the inspector renders the server's Apps; D-103). When
	// no source is configured the endpoint answers with an empty array so the
	// App-frame renders its "No App attached" empty state. A discovery failure
	// (the server is unreachable, or a ui:// resource carried no HTML) answers
	// 502 with a typed message so the App-frame surfaces an honest error state.
	mux.HandleFunc("GET /api/apps", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		apps, err := loadApps(r.Context(), opts.Apps)
		if err != nil {
			log.WarnContext(r.Context(), "dockyard inspector: App discovery failed",
				slog.String("error", err.Error()))
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(apps)
	})

	// The web/inspector frontend (its built dist/ tree), or a placeholder.
	mux.Handle("/", frontendHandler(opts.Assets, log))

	return mux
}

// loadApps invokes the configured App source, tolerating a nil source. A nil
// source yields an empty slice (the inspector is detached) — never an error,
// so a detached inspector still renders the App-frame's empty state cleanly.
func loadApps(ctx context.Context, src AppSource) ([]AppPreview, error) {
	if src == nil {
		return []AppPreview{}, nil
	}
	apps, err := src(ctx)
	if err != nil {
		return nil, err
	}
	if apps == nil {
		return []AppPreview{}, nil
	}
	return apps, nil
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
