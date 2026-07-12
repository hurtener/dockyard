package testgate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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
