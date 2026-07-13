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
	if err := validateLifecycleShape(version, request, response); err != nil {
		return versionOutcome{version: version, detail: err.Error()}
	}

	for path, want := range fixture.Expect {
		got, ok := lookupJSONPath(response, path)
		if !ok || fmt.Sprint(got) != want {
			return versionOutcome{version: version, detail: fmt.Sprintf("response %s = %q, want %q", path, got, want)}
		}
	}
	return versionOutcome{version: version, passed: true, detail: wantLifecycle + " fixture conforms"}
}

func validateLifecycleShape(version string, request, response map[string]any) error {
	params, ok := request["params"].(map[string]any)
	if !ok {
		return fmt.Errorf("request params must be an object")
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		return fmt.Errorf("response result must be an object")
	}
	if version == legacyCoreVersion {
		if params["protocolVersion"] != legacyCoreVersion || result["protocolVersion"] != legacyCoreVersion {
			return fmt.Errorf("legacy initialize must declare protocol version %s", legacyCoreVersion)
		}
		return nil
	}
	for _, legacy := range []string{"protocolVersion", "capabilities", "clientInfo"} {
		if _, present := params[legacy]; present {
			return fmt.Errorf("modern discovery request must not contain legacy params.%s", legacy)
		}
	}
	if _, present := result["protocolVersion"]; present {
		return fmt.Errorf("modern discovery result must not contain legacy protocolVersion")
	}
	meta, ok := params["_meta"].(map[string]any)
	if !ok {
		return fmt.Errorf("modern discovery request params._meta must be an object")
	}
	if meta["io.modelcontextprotocol/protocolVersion"] != modernCoreVersion {
		return fmt.Errorf("modern discovery request metadata must declare protocol version %s", modernCoreVersion)
	}
	clientInfo, ok := meta["io.modelcontextprotocol/clientInfo"].(map[string]any)
	if !ok {
		return fmt.Errorf("modern discovery request metadata must contain clientInfo")
	}
	if clientInfo["name"] == "" || clientInfo["version"] == "" {
		return fmt.Errorf("modern discovery request clientInfo must contain name and version")
	}
	if _, ok := meta["io.modelcontextprotocol/clientCapabilities"].(map[string]any); !ok {
		return fmt.Errorf("modern discovery request metadata must contain clientCapabilities")
	}
	// DiscoverResult extends the pinned core Result and CacheableResult schemas,
	// so all three fields are required even though the prerelease Go SDK's
	// DiscoverResult currently omits the common resultType discriminator.
	if result["resultType"] != "complete" {
		return fmt.Errorf("modern discovery resultType must be complete")
	}
	ttl, ok := result["ttlMs"].(float64)
	if !ok || ttl < 0 {
		return fmt.Errorf("modern discovery result ttlMs must be a non-negative number")
	}
	if scope := result["cacheScope"]; scope != "public" && scope != "private" {
		return fmt.Errorf("modern discovery result cacheScope must be public or private")
	}
	if _, ok := result["capabilities"].(map[string]any); !ok {
		return fmt.Errorf("modern discovery result must contain capabilities")
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] == "" || serverInfo["version"] == "" {
		return fmt.Errorf("modern discovery result serverInfo must contain name and version")
	}
	versions, ok := result["supportedVersions"].([]any)
	if !ok || len(versions) == 0 {
		return fmt.Errorf("modern discovery result must contain supportedVersions")
	}
	for _, supported := range versions {
		if supported == modernCoreVersion {
			return nil
		}
	}
	return fmt.Errorf("modern discovery result does not support %s", modernCoreVersion)
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
