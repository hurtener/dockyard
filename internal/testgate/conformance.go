package testgate

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

const (
	legacyCoreVersion = "2025-11-25"
	modernCoreVersion = "2026-07-28"
)

//go:embed testdata/conformance/*.json
var coreConformanceFixtures embed.FS

type coreFixture struct {
	Version     string            `json:"version"`
	Lifecycle   string            `json:"lifecycle"`
	Request     json.RawMessage   `json:"request"`
	Response    json.RawMessage   `json:"response"`
	Expect      map[string]string `json:"expect"`
	AppsDialect string            `json:"appsDialect,omitempty"`
}

type versionOutcome struct {
	version string
	passed  bool
	detail  string
}

func runCoreConformance(fsys fs.FS) []versionOutcome {
	versions := []string{legacyCoreVersion, modernCoreVersion}
	outcomes := make([]versionOutcome, 0, len(versions))
	for _, version := range versions {
		outcomes = append(outcomes, checkCoreFixture(fsys, version))
	}
	return outcomes
}

func checkCoreFixture(fsys fs.FS, version string) versionOutcome {
	path := "testdata/conformance/core-" + version + ".json"
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return versionOutcome{version: version, detail: fmt.Sprintf("fixture unavailable: %v", err)}
	}
	var fixture coreFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return versionOutcome{version: version, detail: fmt.Sprintf("invalid fixture: %v", err)}
	}
	if fixture.Version != version {
		return versionOutcome{version: version, detail: fmt.Sprintf("fixture declares version %q", fixture.Version)}
	}

	wantLifecycle := "initialize"
	if version == modernCoreVersion {
		wantLifecycle = "server/discover"
	}
	if fixture.Lifecycle != wantLifecycle {
		return versionOutcome{version: version, detail: fmt.Sprintf("lifecycle is %q, want %q", fixture.Lifecycle, wantLifecycle)}
	}
	if fixture.AppsDialect != "" {
		return versionOutcome{version: version, detail: "Apps ui/initialize is a separate iframe dialect and must not appear in a base-core fixture"}
	}

	request, err := decodeObject(fixture.Request)
	if err != nil {
		return versionOutcome{version: version, detail: "request: " + err.Error()}
	}
	response, err := decodeObject(fixture.Response)
	if err != nil {
		return versionOutcome{version: version, detail: "response: " + err.Error()}
	}
	if request["jsonrpc"] != "2.0" || response["jsonrpc"] != "2.0" {
		return versionOutcome{version: version, detail: "request and response must use JSON-RPC 2.0"}
	}
	if request["method"] != wantLifecycle {
		return versionOutcome{version: version, detail: fmt.Sprintf("request method is %q, want %q", request["method"], wantLifecycle)}
	}
	if fmt.Sprint(request["id"]) != fmt.Sprint(response["id"]) {
		return versionOutcome{version: version, detail: "response id does not match request id"}
	}

	for path, want := range fixture.Expect {
		got, ok := lookupJSONPath(response, path)
		if !ok || fmt.Sprint(got) != want {
			return versionOutcome{version: version, detail: fmt.Sprintf("response %s = %q, want %q", path, got, want)}
		}
	}
	return versionOutcome{version: version, passed: true, detail: wantLifecycle + " fixture conforms"}
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	var object map[string]any
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing JSON object")
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("must be a JSON object")
	}
	return object, nil
}

func lookupJSONPath(object map[string]any, path string) (any, bool) {
	var current any = object
	for _, part := range strings.Split(path, ".") {
		next, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = next[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func renderCoreConformance(outcomes []versionOutcome) (string, bool) {
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].version < outcomes[j].version })
	passed := true
	lines := make([]string, 0, len(outcomes)+1)
	for _, outcome := range outcomes {
		verdict := "PASS"
		if !outcome.passed {
			verdict = "FAIL"
			passed = false
		}
		lines = append(lines, fmt.Sprintf("core %s [%s]: %s", outcome.version, verdict, outcome.detail))
	}
	lines = append(lines, "Apps 2026-01-26 ui/initialize is a separate iframe dialect, not base MCP discovery")
	return strings.Join(lines, "\n"), passed
}
