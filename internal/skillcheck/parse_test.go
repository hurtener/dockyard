package skillcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_ValidFixture verifies that a known-good SKILL.md fixture
// passes the validator with no diagnostics.
func TestValidate_ValidFixture(t *testing.T) {
	t.Parallel()
	report, err := Validate(filepath.Join("testdata", "valid"))
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if !report.Ok() {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatalf("valid fixture reported %d issues, want 0", len(report.Issues))
	}
	if len(report.Skills) != 1 {
		t.Fatalf("want 1 skill parsed, got %d", len(report.Skills))
	}
	s := report.Skills[0]
	if s.Name != "valid" {
		t.Errorf("Name = %q, want %q", s.Name, "valid")
	}
	if s.Description == "" {
		t.Error("Description is empty")
	}
	if s.Body == "" {
		t.Error("Body is empty")
	}
}

// TestValidate_InvalidName covers the spec's name constraints by sweeping
// every failure mode against a per-failure fixture. Each fixture mounts a
// directory whose name matches the (intentionally broken) `name` field so
// the test exercises the rule under test in isolation; the parent-dir
// match rule is exercised separately by TestValidate_NameDirMismatch.
func TestValidate_InvalidName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dir          string
		wantContains string
	}{
		{"invalid-name-uppercase", "lowercase letters"},
		{"invalid-name-leading-hyphen", "start with a hyphen"},
		{"invalid-name-trailing-hyphen", "end with a hyphen"},
		{"invalid-name-double-hyphen", "consecutive hyphens"},
	}
	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			_, report := ValidateFile(filepath.Join("testdata", tc.dir, "SKILL.md"))
			if report.Ok() {
				t.Fatalf("%s: want diagnostics, got none", tc.dir)
			}
			if !hasIssue(report.Issues, "name", tc.wantContains) {
				for _, i := range report.Issues {
					t.Logf("issue: %s", i)
				}
				t.Fatalf("%s: missing expected name diagnostic %q", tc.dir, tc.wantContains)
			}
		})
	}
}

// TestValidate_NameDirMismatch exercises the spec's "name matches parent
// directory name" rule. The fixture's `name` field is internally valid but
// does not match the parent directory.
func TestValidate_NameDirMismatch(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-name-mismatch", "SKILL.md"))
	if report.Ok() {
		t.Fatal("want diagnostics, got none")
	}
	if !hasIssue(report.Issues, "name", "match the parent directory") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected name-mismatch diagnostic")
	}
}

// TestValidate_MissingDescription enforces the description-required rule.
func TestValidate_MissingDescription(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-description-missing", "SKILL.md"))
	if !hasIssue(report.Issues, "description", "required") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected description-required diagnostic")
	}
}

// TestValidate_EmptyBody covers the "body must be non-empty" rule — an
// empty SKILL.md body is a stub, not a skill.
func TestValidate_EmptyBody(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-empty-body", "SKILL.md"))
	if !hasIssue(report.Issues, "body", "non-empty") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected empty-body diagnostic")
	}
}

// TestValidate_MissingFrontmatter rejects a file with no `---` fence.
func TestValidate_MissingFrontmatter(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-no-frontmatter", "SKILL.md"))
	if !hasIssue(report.Issues, "frontmatter", "must begin with") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected no-frontmatter diagnostic")
	}
}

// TestValidate_UnterminatedFrontmatter rejects a file whose `---` fence is
// not closed.
func TestValidate_UnterminatedFrontmatter(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-unterminated-frontmatter", "SKILL.md"))
	if !hasIssue(report.Issues, "frontmatter", "not terminated") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected unterminated-frontmatter diagnostic")
	}
}

// TestValidate_NameTooLong covers the ≤64-character limit on `name`.
func TestValidate_NameTooLong(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-name-too-long", "SKILL.md"))
	if !hasIssue(report.Issues, "name", "≤ 64") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected name-too-long diagnostic")
	}
}

// TestValidate_DescriptionTooLong covers the ≤1024-character limit on
// `description`.
func TestValidate_DescriptionTooLong(t *testing.T) {
	t.Parallel()
	_, report := ValidateFile(filepath.Join("testdata", "invalid-description-too-long", "SKILL.md"))
	if !hasIssue(report.Issues, "description", "≤ 1024") {
		for _, i := range report.Issues {
			t.Logf("issue: %s", i)
		}
		t.Fatal("missing expected description-too-long diagnostic")
	}
}

// TestValidate_MissingRoot reports an error (not a clean Report) when the
// directory does not exist.
func TestValidate_MissingRoot(t *testing.T) {
	t.Parallel()
	_, err := Validate(filepath.Join("testdata", "does-not-exist"))
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestValidate_RealSkillsTree exercises the validator against the actual
// skills/ directory shipped by the repo. Phase 29's acceptance bar
// requires every shipped skill to validate; a regression that breaks one
// fails this test.
func TestValidate_RealSkillsTree(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "skills")
	report, err := Validate(root)
	if err != nil {
		// Before Phase 29 lands the skills/ directory may not exist; that
		// is fine and tested by TestValidate_MissingRoot. Skip rather
		// than fail so this test stays useful across the lifetime of
		// the repo.
		t.Skipf("skills/ not present yet: %v", err)
	}
	if !report.Ok() {
		for _, i := range report.Issues {
			t.Errorf("skills/ issue: %s", i)
		}
		t.Fatalf("real skills/ tree reported %d issues, want 0", len(report.Issues))
	}
	if len(report.Skills) < 1 {
		t.Skipf("skills/ exists but contains no SKILL.md yet — Phase 29 has not authored them")
	}
}

// hasIssue is a small helper: did the report carry an issue against the
// named field whose message contains `substr`?
func hasIssue(issues []Issue, field, substr string) bool {
	for _, i := range issues {
		if i.Field == field && strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}
