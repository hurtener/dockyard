package apps_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
)

func TestDerivedDomain_EmptyLabel(t *testing.T) {
	t.Parallel()
	got, err := apps.DerivedDomain("claude", "", "https://x.example.com/mcp")
	if err != nil || got != "" {
		t.Errorf("DerivedDomain(empty label) = %q, %v; want \"\", nil", got, err)
	}
}

func TestDerivedDomain_Generic(t *testing.T) {
	t.Parallel()
	got, err := apps.DerivedDomain("", "verbatim-label", "https://x.example.com/mcp")
	if err != nil {
		t.Fatalf("DerivedDomain generic: %v", err)
	}
	if got != "verbatim-label" {
		t.Errorf("DerivedDomain generic = %q, want verbatim", got)
	}
}

func TestDerivedDomain_Claude(t *testing.T) {
	t.Parallel()
	got, err := apps.DerivedDomain("claude", "main", "https://x.example.com/mcp")
	if err != nil {
		t.Fatalf("DerivedDomain claude: %v", err)
	}
	if !strings.HasSuffix(got, ".claudemcpcontent.com") {
		t.Errorf("DerivedDomain claude = %q, want claudemcpcontent.com origin", got)
	}
}

func TestDerivedDomain_UnknownHost(t *testing.T) {
	t.Parallel()
	_, err := apps.DerivedDomain("not-a-host", "main", "https://x.example.com/mcp")
	if !errors.Is(err, apps.ErrUnknownHost) {
		t.Fatalf("DerivedDomain(unknown host) error = %v, want ErrUnknownHost", err)
	}
}

// TestDerivedDomain_Golden pins the exact Claude-derived origin for a fixed
// (server URL, label) pair, so any change to the derivation algorithm is caught
// (AGENTS.md §11 golden test). The golden value is
// hex(SHA-256("<serverURL>\x00<label>")[:16]) + ".claudemcpcontent.com" — the
// D-063 derivation. Regenerate it only alongside a deliberate, decision-logged
// algorithm change.
func TestDerivedDomain_Golden(t *testing.T) {
	t.Parallel()
	const (
		serverURL = "https://weather.example.com/mcp"
		label     = "dashboard"
		want      = "6ce853ac0d7e58efa4841bc48c4dc14a.claudemcpcontent.com"
	)
	got, err := apps.DerivedDomain("claude", label, serverURL)
	if err != nil {
		t.Fatalf("DerivedDomain: %v", err)
	}
	if got != want {
		t.Errorf("DerivedDomain golden = %q, want %q", got, want)
	}
}

// TestResourceMeta_DomainDerived is the integration check: a registered App
// with HostProfile "claude" is read over a real in-memory MCP session and its
// resources/read _meta.ui.domain is the derived signed origin — proving the
// derivation seam is wired into the resource-read choke point (AGENTS.md §17).
func TestResourceMeta_DomainDerived(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://derived/main"
	if err := apps.Register(s, apps.App{
		URI:         uri,
		Name:        "derived",
		HTML:        []byte("<html><body>derived</body></html>"),
		Domain:      "dashboard",
		HostProfile: "claude",
		ServerURL:   "https://weather.example.com/mcp",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui, ok := read.Contents[0].Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui missing or wrong type: %#v", read.Contents[0].Meta)
	}
	domain, _ := ui["domain"].(string)
	if !strings.HasSuffix(domain, ".claudemcpcontent.com") {
		t.Errorf("_meta.ui.domain = %q, want a derived claudemcpcontent.com origin", domain)
	}
	if domain == "dashboard" {
		t.Error("_meta.ui.domain was carried verbatim, not derived")
	}
}

// TestResourceMeta_NoDomainOmitted proves an App with no domain label still
// omits _meta.ui entirely — Phase 09's deny-by-default omission is preserved.
func TestResourceMeta_NoDomainOmitted(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://nodomain/main"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "nodomain",
		HTML: []byte("<html><body>no domain</body></html>"),
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if _, present := read.Contents[0].Meta["ui"]; present {
		t.Errorf("_meta.ui present for an App declaring no CSP/domain/permissions: %#v",
			read.Contents[0].Meta)
	}
}

// TestResourceMeta_GenericVerbatim proves the default profile still carries a
// declared label verbatim — the Phase 09 behaviour (D-049) is the default.
func TestResourceMeta_GenericVerbatim(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const uri = "ui://generic/main"
	if err := apps.Register(s, apps.App{
		URI:    uri,
		Name:   "generic",
		HTML:   []byte("<html><body>generic</body></html>"),
		Domain: "plain-label",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui := read.Contents[0].Meta["ui"].(map[string]any)
	if ui["domain"] != "plain-label" {
		t.Errorf("_meta.ui.domain = %v, want verbatim plain-label", ui["domain"])
	}
}

// TestResourceMeta_ClaudeMissingServerURL proves Register fails cleanly (typed
// error, no panic) when a Claude-profile App omits the server URL.
func TestResourceMeta_ClaudeMissingServerURL(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)
	err := apps.Register(s, apps.App{
		URI:         "ui://bad/main",
		Name:        "bad",
		HTML:        []byte("<html></html>"),
		Domain:      "main",
		HostProfile: "claude",
	})
	if err == nil {
		t.Fatal("Register with claude profile and no ServerURL = nil, want error")
	}
}
