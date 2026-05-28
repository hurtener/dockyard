package changelogx

import (
	"os"
	"path/filepath"
	"testing"
)

// supplementFixture is the fixed commit set the golden test pins. It mixes
// every classification path: feat (Added), fix (Fixed), a dropped noise type
// (docs/chore), perf + refactor + a non-conventional subject (all Changed),
// scoped and unscoped, with and without a trailing PR number. The order is
// newest-first, the order git-log yields — the supplement must preserve it
// within each group.
var supplementFixture = []Commit{
	{Type: "feat", Scope: "apps", Subject: "add the prompts panel", PR: 55},
	{Type: "fix", Scope: "codegen", Subject: "handle empty structs", PR: 54},
	{Type: "docs", Subject: "update README", PR: 53},
	{Type: "perf", Subject: "speed up the ring buffer"},
	{Type: "chore", Subject: "bump deps"},
	{Type: "feat", Subject: "add --no-postgen flag", PR: 60},
	{Type: "", Subject: "Tweak something directly"},
	{Type: "refactor", Scope: "server", Subject: "extract helper"},
}

// TestSupplement_Golden pins Supplement's rendered output for the fixed
// fixture against testdata/supplement.golden (D-167; the D-157 golden
// discipline). A change to the render format is a loud unit-test failure in
// PR, never a quiet release-time surprise.
func TestSupplement_Golden(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(filepath.Join("testdata", "supplement.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got := Supplement(supplementFixture)
	if got != string(want) {
		t.Errorf("Supplement output drifted from golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestSupplement_Empty asserts that an empty input — and an input of only
// dropped noise-type commits — yields the empty string (the caller appends
// nothing).
func TestSupplement_Empty(t *testing.T) {
	t.Parallel()
	if got := Supplement(nil); got != "" {
		t.Errorf("Supplement(nil) = %q, want empty", got)
	}
	noiseOnly := []Commit{
		{Type: "docs", Subject: "tidy"},
		{Type: "chore", Subject: "bump"},
		{Type: "ci", Subject: "pin action"},
	}
	if got := Supplement(noiseOnly); got != "" {
		t.Errorf("Supplement(noise only) = %q, want empty", got)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := map[string]changelogGroup{
		"feat":     groupAdded,
		"fix":      groupFixed,
		"perf":     groupChanged,
		"refactor": groupChanged,
		"":         groupChanged, // non-conventional → catch-all
		"weird":    groupChanged, // unknown prefix → catch-all
		"docs":     groupNone,
		"chore":    groupNone,
		"test":     groupNone,
		"ci":       groupNone,
		"build":    groupNone,
		"style":    groupNone,
	}
	for in, want := range cases {
		if got := classify(in); got != want {
			t.Errorf("classify(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseCommit(t *testing.T) {
	t.Parallel()
	cases := map[string]Commit{
		"feat: add a thing": {Type: "feat", Subject: "add a thing"},
		"fix(codegen): handle empty structs (#54)": {
			Type: "fix", Scope: "codegen", Subject: "handle empty structs", PR: 54,
		},
		"feat(apps)!: breaking change":           {Type: "feat", Scope: "apps", Subject: "breaking change"},
		"Merge branch 'main' into feat":          {Subject: "Merge branch 'main' into feat"},
		"just a plain subject (#7)":              {Subject: "just a plain subject", PR: 7},
		"  chore(deps): bump go-sdk to v1.6.0  ": {Type: "chore", Scope: "deps", Subject: "bump go-sdk to v1.6.0"},
	}
	for in, want := range cases {
		got := ParseCommit(in)
		if got != want {
			t.Errorf("ParseCommit(%q) = %+v, want %+v", in, got, want)
		}
	}
}
