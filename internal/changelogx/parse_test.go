package changelogx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads the in-repo testdata CHANGELOG. Failing to find it is a
// test setup error rather than an assertion failure.
func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read testdata CHANGELOG: %v", err)
	}
	return b
}

// TestExtractSection_Version1 asserts the v1.0.0 body is extracted and
// excludes both the H2 heading itself and the trailing reference-link
// block.
func TestExtractSection_Version1(t *testing.T) {
	t.Parallel()
	got, err := ExtractSection(loadFixture(t), "1.0.0")
	if err != nil {
		t.Fatalf("ExtractSection(1.0.0): %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "First stable release.") {
		t.Errorf("body missing introduction line; got:\n%s", body)
	}
	if !strings.Contains(body, "### Highlights") {
		t.Errorf("body missing Highlights subheading; got:\n%s", body)
	}
	if strings.Contains(body, "## [1.0.0]") {
		t.Errorf("body should not include the H2 heading; got:\n%s", body)
	}
	if strings.Contains(body, "## [0.9.0]") {
		t.Errorf("body should not include the next section; got:\n%s", body)
	}
	if strings.Contains(body, "[1.0.0]: https://") {
		t.Errorf("body should not include the reference-link footer; got:\n%s", body)
	}
	if strings.HasPrefix(body, "\n") || strings.HasSuffix(body, "\n\n") {
		t.Errorf("body should be blank-line-trimmed; got: %q", body)
	}
}

// TestExtractSection_VPrefix accepts both "1.0.0" and "v1.0.0".
func TestExtractSection_VPrefix(t *testing.T) {
	t.Parallel()
	got, err := ExtractSection(loadFixture(t), "v1.0.0")
	if err != nil {
		t.Fatalf("ExtractSection(v1.0.0): %v", err)
	}
	if !strings.Contains(string(got), "First stable release.") {
		t.Errorf("v-prefix lookup did not match bare-semver entry")
	}
}

// TestExtractSection_Unreleased exercises the Unreleased entry — the
// release workflow uses it for pre-tag dry-runs ("what's coming next").
func TestExtractSection_Unreleased(t *testing.T) {
	t.Parallel()
	got, err := ExtractSection(loadFixture(t), "Unreleased")
	if err != nil {
		t.Fatalf("ExtractSection(Unreleased): %v", err)
	}
	if !strings.Contains(string(got), "Nothing yet.") {
		t.Errorf("Unreleased section content unexpected; got:\n%s", string(got))
	}
}

// TestExtractSection_Earlier confirms the parser does not stop at the first
// section — given v0.9.0, the second body in the file is returned, not
// either of the prior two.
func TestExtractSection_Earlier(t *testing.T) {
	t.Parallel()
	got, err := ExtractSection(loadFixture(t), "0.9.0")
	if err != nil {
		t.Fatalf("ExtractSection(0.9.0): %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "Beta cut.") {
		t.Errorf("body missing the v0.9.0 introduction; got:\n%s", body)
	}
	if strings.Contains(body, "First stable release.") {
		t.Errorf("body leaked content from the v1.0.0 section; got:\n%s", body)
	}
}

// TestExtractSection_Missing returns ErrSectionNotFound for a version not
// in the file.
func TestExtractSection_Missing(t *testing.T) {
	t.Parallel()
	_, err := ExtractSection(loadFixture(t), "2.0.0")
	if !errors.Is(err, ErrSectionNotFound) {
		t.Errorf("expected ErrSectionNotFound; got %v", err)
	}
}

// TestExtractSection_EmptyInputs covers the two empty-arg defensive cases.
func TestExtractSection_EmptyInputs(t *testing.T) {
	t.Parallel()
	if _, err := ExtractSection(nil, "1.0.0"); !errors.Is(err, ErrMalformed) {
		t.Errorf("empty content: expected ErrMalformed; got %v", err)
	}
	if _, err := ExtractSection(loadFixture(t), ""); !errors.Is(err, ErrSectionNotFound) {
		t.Errorf("empty version: expected ErrSectionNotFound; got %v", err)
	}
}

// TestExtractSection_MalformedNoH2 returns ErrMalformed when the file has
// no H2 release headings at all.
func TestExtractSection_MalformedNoH2(t *testing.T) {
	t.Parallel()
	bare := []byte("# Changelog\n\nNo releases yet.\n")
	if _, err := ExtractSection(bare, "1.0.0"); !errors.Is(err, ErrMalformed) {
		t.Errorf("expected ErrMalformed for a file without H2 headings; got %v", err)
	}
}

// TestExtractSection_InRepoCHANGELOG runs the parser against the actual
// in-repo CHANGELOG.md to keep the release pipeline honest — a future
// authoring change that breaks the section shape fails this test
// immediately rather than at release time.
func TestExtractSection_InRepoCHANGELOG(t *testing.T) {
	t.Parallel()
	// The test file lives at internal/changelogx/parse_test.go; the
	// repo root is two levels up.
	repoRoot := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(repoRoot, "CHANGELOG.md")) //nolint:gosec // fixed in-repo path
	if err != nil {
		t.Skipf("skip in-repo CHANGELOG check: %v", err)
	}
	got, err := ExtractSection(b, "1.0.0")
	if err != nil {
		t.Fatalf("ExtractSection(1.0.0) on in-repo CHANGELOG: %v", err)
	}
	if len(got) == 0 {
		t.Errorf("in-repo CHANGELOG v1.0.0 section is empty")
	}
	// The four binding properties are the load-bearing framing — the
	// CHANGELOG entry without them is the wrong release notes.
	for _, marker := range []string{"P1", "P2", "P3", "P4"} {
		if !strings.Contains(string(got), marker) {
			t.Errorf("in-repo v1.0.0 section is missing the %s framing", marker)
		}
	}
}
