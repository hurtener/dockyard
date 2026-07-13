package testgate

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCoreConformanceGolden(t *testing.T) {
	t.Parallel()
	detail, passed := renderCoreConformance(runCoreConformance(coreConformanceFixtures))
	if !passed {
		t.Fatalf("core conformance failed:\n%s", detail)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "core-conformance.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if detail != strings.TrimSpace(string(want)) {
		t.Fatalf("detail mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, detail)
	}
}

func TestCoreConformancePartialFailure(t *testing.T) {
	t.Parallel()
	fixtureFS := fstest.MapFS{}
	for _, version := range []string{legacyCoreVersion, modernCoreVersion} {
		path := "testdata/conformance/core-" + version + ".json"
		raw, err := fs.ReadFile(coreConformanceFixtures, path)
		if err != nil {
			t.Fatal(err)
		}
		fixtureFS[path] = &fstest.MapFile{Data: raw}
	}
	modern := fixtureFS["testdata/conformance/core-2026-07-28.json"]
	modern.Data = []byte(strings.Replace(string(modern.Data), `"method": "server/discover"`, `"method": "initialize"`, 1))

	outcomes := runCoreConformance(fixtureFS)
	detail, passed := renderCoreConformance(outcomes)
	if passed {
		t.Fatal("partial fixture failure passed")
	}
	if !strings.Contains(detail, "core 2025-11-25 [PASS]") ||
		!strings.Contains(detail, "core 2026-07-28 [FAIL]") {
		t.Fatalf("partial outcomes not preserved:\n%s", detail)
	}
}

func TestCoreConformanceRejectsAppsDialectInBaseDiscovery(t *testing.T) {
	t.Parallel()
	fixtureFS := fstest.MapFS{}
	path := "testdata/conformance/core-2025-11-25.json"
	raw, err := fs.ReadFile(coreConformanceFixtures, path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"lifecycle": "initialize",`,
		`"lifecycle": "initialize", "appsDialect": "ui/initialize",`, 1))
	fixtureFS[path] = &fstest.MapFile{Data: raw}
	outcome := checkCoreFixture(fixtureFS, legacyCoreVersion)
	if outcome.passed || !strings.Contains(outcome.detail, "separate") {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestCoreConformanceRejectsLegacyFieldsInModernDiscovery(t *testing.T) {
	t.Parallel()
	path := "testdata/conformance/core-" + modernCoreVersion + ".json"
	raw, err := fs.ReadFile(coreConformanceFixtures, path)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"request protocolVersion": func(s string) string {
			return strings.Replace(s, `"_meta": {`, `"protocolVersion": "2026-07-28", "_meta": {`, 1)
		},
		"request capabilities": func(s string) string {
			return strings.Replace(s, `"_meta": {`, `"capabilities": {}, "_meta": {`, 1)
		},
		"request clientInfo": func(s string) string {
			return strings.Replace(s, `"_meta": {`, `"clientInfo": {}, "_meta": {`, 1)
		},
		"response protocolVersion": func(s string) string {
			return strings.Replace(s, `"supportedVersions": ["2026-07-28"],`, `"protocolVersion": "2026-07-28", "supportedVersions": ["2026-07-28"],`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			outcome := checkCoreFixture(fstest.MapFS{path: &fstest.MapFile{Data: []byte(mutate(string(raw)))}}, modernCoreVersion)
			if outcome.passed || !strings.Contains(outcome.detail, "legacy") {
				t.Fatalf("outcome = %#v", outcome)
			}
		})
	}
}

func TestCoreConformanceRequiresModernResultFieldsIndependently(t *testing.T) {
	t.Parallel()
	path := "testdata/conformance/core-" + modernCoreVersion + ".json"
	raw, err := fs.ReadFile(coreConformanceFixtures, path)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"resultType": func(s string) string {
			return strings.Replace(s, "      \"resultType\": \"complete\",\n", "", 1)
		},
		"ttlMs": func(s string) string {
			return strings.Replace(s, "      \"ttlMs\": 3600000,\n", "", 1)
		},
		"cacheScope": func(s string) string {
			return strings.Replace(s, ",\n      \"cacheScope\": \"public\"\n", "\n", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := mutate(string(raw))
			outcome := checkCoreFixture(fstest.MapFS{path: &fstest.MapFile{Data: []byte(mutated)}}, modernCoreVersion)
			if outcome.passed || !strings.Contains(outcome.detail, name) {
				t.Fatalf("outcome = %#v", outcome)
			}
		})
	}
}

func TestCoreConformanceTracksPinnedSDKDiscoverResult(t *testing.T) {
	t.Parallel()
	path := "testdata/conformance/core-" + modernCoreVersion + ".json"
	raw, err := fs.ReadFile(coreConformanceFixtures, path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture coreFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	// The pinned SDK models every discovery-specific field and both required
	// cache fields, but its prerelease DiscoverResult omits the core Result
	// discriminator required by the vendored schema.
	sdkResult := &mcpsdk.DiscoverResult{
		Cacheable:         mcpsdk.Cacheable{TTLMs: 3_600_000, CacheScope: "public"},
		SupportedVersions: []string{modernCoreVersion},
		Capabilities:      &mcpsdk.ServerCapabilities{Tools: &mcpsdk.ToolCapabilities{}},
		ServerInfo:        &mcpsdk.Implementation{Name: "fixture-server", Version: "1.0.0"},
	}
	sdkRaw, err := json.Marshal(sdkResult)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(sdkRaw, &result); err != nil {
		t.Fatal(err)
	}
	if _, present := result["resultType"]; present {
		t.Fatal("pinned SDK DiscoverResult unexpectedly gained resultType; reconcile the conformance adapter")
	}
	if result["ttlMs"] != float64(3_600_000) || result["cacheScope"] != "public" {
		t.Fatalf("pinned SDK discovery cache fields = ttlMs %v, cacheScope %v", result["ttlMs"], result["cacheScope"])
	}

	setResponse := func(result any) {
		fixture.Response, err = json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	setResponse(sdkResult)
	fixtureRaw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	outcome := checkCoreFixture(fstest.MapFS{path: &fstest.MapFile{Data: fixtureRaw}}, modernCoreVersion)
	if outcome.passed || !strings.Contains(outcome.detail, "resultType") {
		t.Fatalf("unadapted pinned SDK outcome = %#v", outcome)
	}

	result["resultType"] = "complete"
	setResponse(result)
	fixtureRaw, err = json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	outcome = checkCoreFixture(fstest.MapFS{path: &fstest.MapFile{Data: fixtureRaw}}, modernCoreVersion)
	if !outcome.passed {
		t.Fatalf("spec-complete pinned SDK outcome = %#v", outcome)
	}
}
