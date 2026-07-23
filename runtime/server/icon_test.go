package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInfoValidateBranding covers the Info branding validation gates.
func TestInfoValidateBranding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		info    Info
		wantErr bool
	}{
		{"no branding", Info{Name: "n", Version: "1"}, false},
		{"valid https icon", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "https://x/logo.png", MIMEType: "image/png"}}}, false},
		{"valid data icon", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "data:image/png;base64,AAAA"}}}, false},
		{"valid dark theme", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "https://x/l.png", Theme: IconThemeDark}}}, false},
		{"valid website", Info{Name: "n", Version: "1", WebsiteURL: "https://x.example"}, false},
		{"empty icon src", Info{Name: "n", Version: "1", Icons: []Icon{{Src: ""}}}, true},
		{"http icon rejected", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "http://x/l.png"}}}, true},
		{"ftp icon rejected", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "ftp://x/l.png"}}}, true},
		{"whitespace-padded src rejected", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "  https://x/l.png"}}}, true},
		{"data non-image rejected", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "data:text/html;base64,AAAA"}}}, true},
		{"data image uppercase accepted", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "data:IMAGE/PNG;base64,AAAA"}}}, false},
		{"bad theme", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "https://x/l.png", Theme: "blue"}}}, true},
		{"http website rejected", Info{Name: "n", Version: "1", WebsiteURL: "http://x.example"}, true},
		{"second icon invalid reported", Info{Name: "n", Version: "1", Icons: []Icon{{Src: "https://x/a.png"}, {Src: "nope"}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.info.validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate(%+v) err = %v, wantErr = %v", tc.info, err, tc.wantErr)
			}
		})
	}
}

// TestNewRejectsInvalidIcon proves New surfaces an icon validation failure.
func TestNewRejectsInvalidIcon(t *testing.T) {
	if _, err := New(Info{Name: "n", Version: "1", Icons: []Icon{{Src: "http://insecure/logo.png"}}}, nil); err == nil {
		t.Fatal("New accepted an insecure icon src")
	}
}

// brandingInfo is a fully-branded Info used by the emission tests.
func brandingInfo() Info {
	return Info{
		Name: "acme", Title: "Acme", Version: "1.2.3",
		Description: "Acme MCP",
		WebsiteURL:  "https://acme.example",
		Icons: []Icon{
			{Src: "https://acme.example/logo.png", MIMEType: "image/png", Sizes: []string{"48x48", "96x96"}, Theme: IconThemeLight},
			{Src: "https://acme.example/logo-dark.png", MIMEType: "image/png", Theme: IconThemeDark},
		},
	}
}

// TestLegacyInitializeAdvertisesIcons drives a real HTTP initialize on the
// legacy lifecycle and asserts the SDK serialises the branding into serverInfo.
func TestLegacyInitializeAdvertisesIcons(t *testing.T) {
	s, err := New(brandingInfo(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&HTTPOptions{ProtocolMode: Legacy})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := decodeJSONRPCResult(t, rr)

	var res struct {
		ServerInfo struct {
			Name        string           `json:"name"`
			Title       string           `json:"title"`
			Description string           `json:"description"`
			WebsiteURL  string           `json:"websiteUrl"`
			Icons       []map[string]any `json:"icons"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode initialize result %s: %v", body, err)
	}
	si := res.ServerInfo
	if si.Name != "acme" || si.Title != "Acme" || si.Description != "Acme MCP" || si.WebsiteURL != "https://acme.example" {
		t.Fatalf("serverInfo = %+v", si)
	}
	if len(si.Icons) != 2 {
		t.Fatalf("serverInfo.icons = %+v, want 2", si.Icons)
	}
	if si.Icons[0]["src"] != "https://acme.example/logo.png" || si.Icons[0]["mimeType"] != "image/png" || si.Icons[0]["theme"] != "light" {
		t.Fatalf("icon[0] = %+v", si.Icons[0])
	}
}

// TestServerInfoOmitsBrandingWhenUnset proves an unbranded server advertises no
// icons/websiteUrl/description key (omitempty), so existing servers are
// byte-unchanged.
func TestServerInfoOmitsBrandingWhenUnset(t *testing.T) {
	s, err := New(Info{Name: "plain", Version: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.HTTPHandler(&HTTPOptions{ProtocolMode: Legacy})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://resource.example/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := decodeJSONRPCResult(t, rr)
	for _, key := range []string{`"icons"`, `"websiteUrl"`, `"description"`} {
		if strings.Contains(string(body), key) {
			t.Fatalf("unbranded serverInfo leaked %s: %s", key, body)
		}
	}
}

// decodeJSONRPCResult extracts the JSON-RPC result object from an initialize
// response, handling both a bare JSON body and an SSE (text/event-stream) frame.
func decodeJSONRPCResult(t *testing.T, rr *httptest.ResponseRecorder) json.RawMessage {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	// SSE framing: the JSON-RPC message follows a "data: " prefix.
	if strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(raw, "\n") {
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				raw = strings.TrimSpace(after)
				break
			}
		}
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode JSON-RPC envelope %q: %v", raw, err)
	}
	if len(env.Error) != 0 {
		t.Fatalf("initialize returned error: %s", env.Error)
	}
	return env.Result
}
