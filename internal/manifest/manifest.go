package manifest

// Manifest is the typed form of dockyard.app.yaml — Dockyard's control plane
// (RFC §4.2). It is produced by Load / LoadFile and is an immutable value after
// loading: its accessor methods (Tool, App, ResolveContracts) only read, so a
// loaded Manifest is safe for concurrent use.
type Manifest struct {
	// Name is the app's wire identifier — a short, kebab-or-snake-case token.
	Name string `yaml:"name"`
	// Title is the human-facing display name.
	Title string `yaml:"title"`
	// Version is the app's semantic version (MAJOR.MINOR.PATCH).
	Version string `yaml:"version"`
	// Description is a short human-readable summary advertised in the handshake's
	// serverInfo. Optional.
	Description string `yaml:"description"`
	// WebsiteURL is the app's homepage, advertised in serverInfo. Optional; must
	// be an absolute https:// URL when set.
	WebsiteURL string `yaml:"website_url"`
	// Icons are the app's branding icons (SEP-973), advertised in serverInfo so a
	// host can show the server's logo. Optional. This is the declarative record;
	// the live server emits from server.Info (the runtime library never reads the
	// manifest), so keep the two in step — see the branding guide.
	Icons []Icon `yaml:"icons"`
	// Runtime declares which deployment modes and UI framework the app supports.
	Runtime Runtime `yaml:"runtime"`
	// Tools is the app's MCP tools. At least one is required.
	Tools []Tool `yaml:"tools"`
	// Apps is the app's ui:// resources. Optional — a plain MCP server has none
	// (RFC §4.1: a UI resource is additive, not a requirement).
	Apps []App `yaml:"apps"`
	// Tasks holds the app's MCP Tasks lifecycle limits (RFC §8.5). Optional —
	// an app with no task-supporting tool omits it and the zero value disables
	// every limit.
	Tasks Tasks `yaml:"tasks"`
	// Quality holds the quality.* gates dockyard validate enforces (RFC §9.4).
	Quality Quality `yaml:"quality"`
}

// Icon is one SEP-973 server branding icon in the manifest. src is required and
// must be an https:// URL or a data: URI; the rest are optional. It mirrors
// runtime/server.Icon (the declarative record of what the server advertises).
type Icon struct {
	// Src is a URI to the icon: an https:// URL or a data:image/ URI. Required.
	Src string `yaml:"src"`
	// MIMEType is the icon's media type (e.g. "image/png"). Optional.
	MIMEType string `yaml:"mime_type"`
	// Sizes lists available pixel sizes (e.g. ["48x48"], ["any"]). Optional.
	Sizes []string `yaml:"sizes"`
	// Theme is "light" or "dark" (the background the icon suits). Optional.
	Theme string `yaml:"theme"`
}

// Tasks is the manifest tasks block: the manifest-tunable MCP Tasks lifecycle
// limits (RFC §8.5). Every field defaults to zero — "no limit" — when omitted,
// which is the correct posture for an ephemeral single-user stdio app. A
// durable HTTP/Portico app sets explicit limits so a task leak cannot grow the
// store unbounded (brief 02 §4.6). The runtime maps these onto the Tasks
// engine's lifecycle controls.
type Tasks struct {
	// MaxTTLMillis is the largest task retention duration the runtime honours,
	// in milliseconds. A requestor asking for more is clamped down. Zero means
	// unlimited retention (no clamp).
	MaxTTLMillis int64 `yaml:"max_ttl_millis"`
	// DefaultTTLMillis is the retention applied to a task whose requestor
	// expressed no TTL preference, in milliseconds. Zero means unlimited.
	DefaultTTLMillis int64 `yaml:"default_ttl_millis"`
	// PurgeIntervalMillis is how often the background TTL purge sweep runs, in
	// milliseconds. Zero disables the sweep.
	PurgeIntervalMillis int64 `yaml:"purge_interval_millis"`
	// MaxConcurrentPerRequestor caps the number of non-terminal tasks one
	// authorization context may hold at once. Zero means no cap.
	MaxConcurrentPerRequestor int `yaml:"max_concurrent_per_requestor"`
}

// Runtime is the manifest runtime block: which transports the app is deployed
// over and, for an App, how its UI is built (RFC §4.2, §7.4).
type Runtime struct {
	// Transports is the set of deployment modes the app supports. V1: stdio,
	// http (RFC §5.2). At least one is required.
	Transports []Transport `yaml:"transports"`
	// UI configures the App UI build. Optional — a plain MCP server omits it.
	UI *RuntimeUI `yaml:"ui"`
}

// RuntimeUI is the runtime.ui block: the UI framework and bundle strategy.
type RuntimeUI struct {
	// Framework is the UI framework. V1: svelte only.
	Framework UIFramework `yaml:"framework"`
	// Bundle is the bundle strategy. single-file is the default and yields a
	// zero-external-origin App so the deny-by-default CSP just works (RFC §7.4).
	Bundle BundleStrategy `yaml:"bundle"`
}

// Tool is one entry of the manifest tools list (RFC §4.2). A tool's input and
// output are Go type references resolved by the codegen pipeline (RFC §6.1).
type Tool struct {
	// Name is the tool's MCP wire name. Required and unique within the manifest.
	Name string `yaml:"name"`
	// Description is the model-facing hint.
	Description string `yaml:"description"`
	// Input is the Go type reference for the tool input contract in the
	// canonical internal/contracts package. Required.
	Input string `yaml:"input"`
	// Output is the Go type reference for the tool output contract in the
	// canonical internal/contracts package. Required.
	Output string `yaml:"output"`
	// UI links the tool to an apps[] entry by its id (RFC §4.2). Optional — a
	// tool without a UI is a plain MCP tool.
	UI string `yaml:"ui"`
	// TaskSupport declares the tool's relationship to the Tasks extension
	// (RFC §8.4). Defaults to forbidden when omitted.
	TaskSupport TaskSupport `yaml:"task_support"`
}

// App is one entry of the manifest apps list — a ui:// resource and its
// host-facing metadata (RFC §4.2, §7).
type App struct {
	// ID is the app's manifest-local identifier; tools[].ui references it.
	// Required and unique within the manifest.
	ID string `yaml:"id"`
	// URI is the ui:// resource URI the App is served under. Required, and must
	// be a well-formed ui:// URI (RFC §7.1).
	URI string `yaml:"uri"`
	// Entry is the path to the App's UI entrypoint, relative to the project
	// root (e.g. web/src/apps/customer-health.svelte). Required.
	Entry string `yaml:"entry"`
	// DisplayModes is the subset of inline|fullscreen|pip the App supports
	// (RFC §7.2). At least one is required.
	DisplayModes []DisplayMode `yaml:"display_modes"`
	// CSP is the App's Content-Security-Policy opt-outs. An empty CSP is the
	// secure default — a single-file bundle with no external origins (RFC §7.4).
	CSP CSP `yaml:"csp"`
	// Visibility is the surfaces the App is exposed to (RFC §7.1 _meta.ui).
	Visibility []Visibility `yaml:"visibility"`
}

// CSP is the apps[].csp block: explicit Content-Security-Policy opt-outs. Each
// list is a set of origins the App's deny-by-default CSP is widened to allow
// (RFC §7.4). Both lists empty is the secure default.
type CSP struct {
	// Connect is the set of origins the App may open network connections to
	// (CSP connect-src).
	Connect []string `yaml:"connect"`
	// Resource is the set of origins the App may load passive resources from
	// (images, fonts, styles).
	Resource []string `yaml:"resource"`
}

// Quality holds the quality.* gates dockyard validate enforces (RFC §9.4). Every
// field defaults to false when omitted; the manifest opts each gate in.
type Quality struct {
	RequireLoadingState    bool `yaml:"require_loading_state"`
	RequireEmptyState      bool `yaml:"require_empty_state"`
	RequireErrorState      bool `yaml:"require_error_state"`
	RequirePermissionState bool `yaml:"require_permission_state"`
	RequireFixtures        bool `yaml:"require_fixtures"`
	RequireContractTests   bool `yaml:"require_contract_tests"`
	RequireSpecCompliance  bool `yaml:"require_spec_compliance"`
}

// Tool returns the named tool and true, or a zero value and false. The returned
// pointer aliases the Manifest's slice element — treat it as read-only.
func (m *Manifest) Tool(name string) (*Tool, bool) {
	for i := range m.Tools {
		if m.Tools[i].Name == name {
			return &m.Tools[i], true
		}
	}
	return nil, false
}

// App returns the app with the given id and true, or a zero value and false.
func (m *Manifest) App(id string) (*App, bool) {
	for i := range m.Apps {
		if m.Apps[i].ID == id {
			return &m.Apps[i], true
		}
	}
	return nil, false
}
