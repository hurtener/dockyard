package inspector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// appReadTimeout bounds one App-discovery round trip — a connect, a
// resources/list, and the resources/read of every ui:// resource. The
// inspector is a dev surface; a server that does not answer promptly degrades
// to the App-frame's honest empty state rather than hanging the UI.
const appReadTimeout = 10 * time.Second

// uiScheme is the URI scheme of an MCP App resource (RFC §7.1). The inspector's
// App-discovery reads only resources under this scheme — never a tool, never a
// mutating call.
const uiScheme = "ui://"

// AppPreview is one MCP App the inspector can render — its ui:// resource URI,
// a display name, and the App's HTML document. It is the inspector's own type:
// no raw MCP SDK struct leaks through it (P3). The frontend's App-frame sets
// HTML as the sandboxed iframe's srcdoc.
type AppPreview struct {
	// URI is the App's ui:// resource URI.
	URI string `json:"uri"`
	// Name is the App's display name (the resource Name, falling back to URI).
	Name string `json:"name"`
	// HTML is the App's HTML document, read from the server's ui:// resource.
	HTML string `json:"html"`
}

// AppSource produces the MCP Apps the inspector can render, on demand. The
// inspector calls it per `GET /api/apps` request. It is read-only: it performs
// a resources/list and a resources/read against the attached server and never
// a mutating call (RFC §12, P4). When the source cannot reach a server, or the
// server registers no ui:// resource, it returns an empty slice and a nil
// error — the App-frame renders its honest "No App attached" empty state.
type AppSource func(ctx context.Context) ([]AppPreview, error)

// AppsFromServer adapts a running MCP server, named by its base URL, into an
// [AppSource]. It is the App-render path RFC §12 line 711 makes binding: the
// inspector "renders the server's Apps". Obtaining an App's UI is a read-only
// resources/read of the server's ui:// resource(s) — within P4 (the inspector
// is the lone client-shaped surface, a dev surface; a read-only resource read
// is not a production MCP client and not arbitrary execution). See D-103, which
// extends D-099: the inspector additionally performs read-only resources/list
// and resources/read to render Apps — still no mutation, still dev-gated and
// localhost-only.
//
// The returned source opens a fresh, short-lived MCP client session per call,
// reads every ui:// resource, and closes the session — it holds no long-lived
// client. An empty baseURL yields a source that returns no Apps (the inspector
// is detached). A connect or read failure is returned as a typed error; the
// `/api/apps` handler maps it to the App-frame's error state.
func AppsFromServer(baseURL string) AppSource {
	return func(ctx context.Context) ([]AppPreview, error) {
		if baseURL == "" {
			return []AppPreview{}, nil
		}
		return readServerApps(ctx, baseURL)
	}
}

// readServerApps connects a read-only MCP client to baseURL, lists the
// server's resources, and reads every ui:// resource into an [AppPreview]. The
// session is closed before return. Apps are returned sorted by URI so the
// inspector's App picker is deterministic.
func readServerApps(ctx context.Context, baseURL string) ([]AppPreview, error) {
	ctx, cancel := context.WithTimeout(ctx, appReadTimeout)
	defer cancel()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "dockyard-inspector", Version: "0.1.0"},
		nil,
	)
	httpClient := modernFirstHTTPClient(appReadTimeout, nil, true)
	session, err := client.Connect(ctx,
		&mcpsdk.StreamableClientTransport{Endpoint: baseURL, HTTPClient: httpClient}, nil)
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: connect %q: %w", baseURL, err)
	}
	defer func() { _ = session.Close() }()

	list, err := session.ListResources(ctx, &mcpsdk.ListResourcesParams{})
	if err != nil {
		return nil, fmt.Errorf("dockyard/internal/inspector: resources/list: %w", err)
	}
	if list == nil {
		return nil, errors.New("dockyard/internal/inspector: resources/list returned no result")
	}

	apps := make([]AppPreview, 0)
	for _, res := range list.Resources {
		if res == nil || !strings.HasPrefix(res.URI, uiScheme) {
			continue
		}
		html, err := readResourceHTML(ctx, session, res.URI)
		if err != nil {
			return nil, err
		}
		name := res.Name
		if name == "" {
			name = res.URI
		}
		apps = append(apps, AppPreview{URI: res.URI, Name: name, HTML: html})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].URI < apps[j].URI })
	return apps, nil
}

// readResourceHTML reads one ui:// resource and returns its HTML document. An
// MCP resources/read answers with one or more content parts; the inspector
// uses the first text part (an App bundle is an HTML document, served as
// text/html — RFC §7.4). A resource that carries no text content is a typed
// error so the App-frame surfaces an honest failure rather than a blank frame.
func readResourceHTML(
	ctx context.Context,
	session *mcpsdk.ClientSession,
	uri string,
) (string, error) {
	res, err := session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		return "", fmt.Errorf("dockyard/internal/inspector: resources/read %q: %w", uri, err)
	}
	if res == nil {
		return "", fmt.Errorf("dockyard/internal/inspector: resources/read %q returned no result", uri)
	}
	for _, c := range res.Contents {
		if c != nil && c.Text != "" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf(
		"dockyard/internal/inspector: ui:// resource %q carried no HTML text content", uri)
}
