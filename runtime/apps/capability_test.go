package apps_test

import (
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/runtime/apps"
)

// TestExtensionCapabilityShape proves ExtensionCapability produces the
// io.modelcontextprotocol/ui block with the single MVP MIME type (RFC §7.1).
func TestExtensionCapabilityShape(t *testing.T) {
	t.Parallel()
	extCap, err := apps.ExtensionCapability()
	if err != nil {
		t.Fatalf("ExtensionCapability: %v", err)
	}
	if extCap.Name != apps.ExtensionID {
		t.Fatalf("capability Name = %q, want %q", extCap.Name, apps.ExtensionID)
	}
	if extCap.Name != "io.modelcontextprotocol/ui" {
		t.Fatalf("ExtensionID = %q, want io.modelcontextprotocol/ui", extCap.Name)
	}
	var settings struct {
		MIMETypes []string `json:"mimeTypes"`
	}
	if err := json.Unmarshal(extCap.Settings, &settings); err != nil {
		t.Fatalf("unmarshal capability settings: %v", err)
	}
	if len(settings.MIMETypes) != 1 || settings.MIMETypes[0] != apps.MIMETypeApp {
		t.Fatalf("capability mimeTypes = %v, want [%q]", settings.MIMETypes, apps.MIMETypeApp)
	}
	if apps.MIMETypeApp != "text/html;profile=mcp-app" {
		t.Fatalf("MIMETypeApp = %q, want text/html;profile=mcp-app", apps.MIMETypeApp)
	}
}
