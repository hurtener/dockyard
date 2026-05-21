package apps_test

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hurtener/dockyard/runtime/apps"
)

// TestCSPDomainsThreaded proves every declared CSP list reaches _meta.ui.csp on
// the resources/read response (brief 01 §2.5, RFC §7.4).
func TestCSPDomainsThreaded(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)
	const uri = "ui://wide/main"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "wide",
		HTML: []byte("<html></html>"),
		CSP: apps.CSP{
			Connect:  []string{"https://api.example.com"},
			Resource: []string{"https://cdn.example.com"},
			Frame:    []string{"https://frame.example.com"},
			BaseURI:  []string{"https://base.example.com"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui := uiMeta(t, read.Contents[0].Meta)
	csp, ok := ui["csp"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui.csp missing: %#v", ui)
	}
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"connectDomains", "https://api.example.com"},
		{"resourceDomains", "https://cdn.example.com"},
		{"frameDomains", "https://frame.example.com"},
		{"baseUriDomains", "https://base.example.com"},
	} {
		list, ok := csp[tc.key].([]any)
		if !ok || len(list) != 1 || list[0] != tc.want {
			t.Fatalf("_meta.ui.csp.%s = %#v, want [%q]", tc.key, csp[tc.key], tc.want)
		}
	}
}

// TestPermissionsThreaded proves each declared sandbox permission reaches
// _meta.ui.permissions as a presence-keyed object (brief 01 §2.5).
func TestPermissionsThreaded(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)
	const uri = "ui://perms/main"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "perms",
		HTML: []byte("<html></html>"),
		Permissions: apps.Permissions{
			Camera:         true,
			Microphone:     true,
			Geolocation:    true,
			ClipboardWrite: true,
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui := uiMeta(t, read.Contents[0].Meta)
	perms, ok := ui["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("_meta.ui.permissions missing: %#v", ui)
	}
	for _, key := range []string{"camera", "microphone", "geolocation", "clipboardWrite"} {
		if _, ok := perms[key]; !ok {
			t.Fatalf("_meta.ui.permissions.%s missing: %#v", key, perms)
		}
	}
}

// TestNoPermissionsNoObject proves an App requesting no permissions emits no
// permissions object — the secure default.
func TestNoPermissionsNoObject(t *testing.T) {
	t.Parallel()
	s := newAppsServer(t)
	const uri = "ui://noperms/main"
	if err := apps.Register(s, apps.App{
		URI:  uri,
		Name: "noperms",
		HTML: []byte("<html></html>"),
		// CSP declared so a _meta.ui object exists, but no permissions.
		CSP: apps.CSP{Connect: []string{"https://api.example.com"}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	session := connect(t, s)
	read, err := session.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	ui := uiMeta(t, read.Contents[0].Meta)
	if _, present := ui["permissions"]; present {
		t.Fatalf("App with no permissions emitted a permissions object: %#v", ui)
	}
}
