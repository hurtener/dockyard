package apps_test

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
)

func TestDerivedDomain_EmptyLabel(t *testing.T) {
	t.Parallel()
	got, err := apps.DerivedDomain("generic", "", "https://x.example.com/mcp")
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

func TestDerivedDomain_UnknownHost(t *testing.T) {
	t.Parallel()
	_, err := apps.DerivedDomain("not-a-host", "main", "https://x.example.com/mcp")
	if !errors.Is(err, apps.ErrUnknownHost) {
		t.Fatalf("DerivedDomain(unknown host) error = %v, want ErrUnknownHost", err)
	}
}

// TestResourceMeta_DomainVerbatim is the integration check (re-pointed from the
// retired derivation test): a registered App with a Domain — and even a
// now-deprecated HostProfile — is read over a real in-memory MCP session and
// its resources/read _meta.ui.domain is the Domain string VERBATIM, never a
// synthesised host origin (D-176, supersedes D-062/D-063; AGENTS.md §17). This
// proves "any HostProfile emits verbatim": the deprecated HostProfile /
// ServerURL fields no longer rewrite the value.
func TestResourceMeta_DomainVerbatim(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)

	const (
		uri  = "ui://derived/main"
		want = "a904794854a047f6.claudemcpcontent.com"
	)
	if err := apps.Register(s, apps.App{
		URI:         uri,
		Name:        "derived",
		HTML:        []byte("<html><body>derived</body></html>"),
		Domain:      want,
		HostProfile: "claude", // deprecated + ignored — proves no rewrite
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
	if domain, _ := ui["domain"].(string); domain != want {
		t.Errorf("_meta.ui.domain = %q, want verbatim %q", domain, want)
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
