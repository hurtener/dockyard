package apps_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/apps"
)

// dnsLabel matches a single valid DNS label: lowercase alphanumerics and
// hyphens, 1–63 characters, no leading/trailing hyphen.
var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// fakeProfile is a test-only host profile used to exercise RegisterHostProfile.
type fakeProfile struct{ id string }

func (f fakeProfile) ID() string                                 { return f.id }
func (fakeProfile) DeriveDomain(label, _ string) (string, error) { return label, nil }

func TestHostProfileFor_BuiltIns(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"generic", "claude"} {
		p, err := apps.HostProfileFor(id)
		if err != nil {
			t.Fatalf("HostProfileFor(%q): %v", id, err)
		}
		if p.ID() != id {
			t.Errorf("HostProfileFor(%q).ID() = %q", id, p.ID())
		}
	}
}

func TestHostProfileFor_EmptyIsGeneric(t *testing.T) {
	t.Parallel()
	p, err := apps.HostProfileFor("")
	if err != nil {
		t.Fatalf("HostProfileFor(\"\"): %v", err)
	}
	if p.ID() != "generic" {
		t.Errorf("empty id resolved to %q, want generic", p.ID())
	}
	if apps.DefaultHostProfile().ID() != "generic" {
		t.Errorf("DefaultHostProfile().ID() = %q, want generic", apps.DefaultHostProfile().ID())
	}
}

func TestHostProfileFor_Unknown(t *testing.T) {
	t.Parallel()
	_, err := apps.HostProfileFor("nope-not-a-host")
	if !errors.Is(err, apps.ErrUnknownHost) {
		t.Fatalf("HostProfileFor(unknown) error = %v, want ErrUnknownHost", err)
	}
}

func TestRegisterHostProfile_Validation(t *testing.T) {
	t.Parallel()
	if err := apps.RegisterHostProfile(nil); err == nil {
		t.Error("RegisterHostProfile(nil) = nil, want error")
	}
	if err := apps.RegisterHostProfile(fakeProfile{id: ""}); err == nil {
		t.Error("RegisterHostProfile(empty id) = nil, want error")
	}
	// A duplicate of a built-in id must be rejected — drivers cannot shadow.
	if err := apps.RegisterHostProfile(fakeProfile{id: "claude"}); err == nil {
		t.Error("RegisterHostProfile(duplicate \"claude\") = nil, want error")
	}
}

func TestRegisterHostProfile_NewDriver(t *testing.T) {
	t.Parallel()
	const id = "test-register-new-driver"
	if err := apps.RegisterHostProfile(fakeProfile{id: id}); err != nil {
		t.Fatalf("RegisterHostProfile(%q): %v", id, err)
	}
	p, err := apps.HostProfileFor(id)
	if err != nil {
		t.Fatalf("HostProfileFor(%q) after register: %v", id, err)
	}
	if p.ID() != id {
		t.Errorf("registered profile ID = %q, want %q", p.ID(), id)
	}
	// Re-registering the same id is rejected.
	if err := apps.RegisterHostProfile(fakeProfile{id: id}); err == nil {
		t.Error("re-registering same id = nil, want error")
	}
}

func TestGenericProfile_VerbatimPassthrough(t *testing.T) {
	t.Parallel()
	p := apps.DefaultHostProfile()
	got, err := p.DeriveDomain("my-label", "https://example.com/mcp")
	if err != nil {
		t.Fatalf("generic DeriveDomain: %v", err)
	}
	if got != "my-label" {
		t.Errorf("generic DeriveDomain = %q, want verbatim %q", got, "my-label")
	}
	// Empty label => empty origin.
	got, err = p.DeriveDomain("", "https://example.com/mcp")
	if err != nil || got != "" {
		t.Errorf("generic DeriveDomain(empty) = %q, %v; want \"\", nil", got, err)
	}
}

func TestClaudeProfile_SignedOriginForm(t *testing.T) {
	t.Parallel()
	p, err := apps.HostProfileFor("claude")
	if err != nil {
		t.Fatalf("HostProfileFor(claude): %v", err)
	}
	const serverURL = "https://weather.example.com/mcp"
	got, err := p.DeriveDomain("dashboard", serverURL)
	if err != nil {
		t.Fatalf("claude DeriveDomain: %v", err)
	}
	if !strings.HasSuffix(got, ".claudemcpcontent.com") {
		t.Fatalf("claude origin %q is not under claudemcpcontent.com", got)
	}
	label, _, _ := strings.Cut(got, ".")
	if len(label) != 32 {
		t.Errorf("claude hash label %q length = %d, want 32", label, len(label))
	}
	if !dnsLabel.MatchString(label) {
		t.Errorf("claude hash label %q is not a valid DNS label", label)
	}
	if label != strings.ToLower(label) {
		t.Errorf("claude hash label %q is not lowercase hex", label)
	}
}

func TestClaudeProfile_Deterministic(t *testing.T) {
	t.Parallel()
	p, _ := apps.HostProfileFor("claude")
	const url = "https://a.example.com/mcp"
	a, _ := p.DeriveDomain("main", url)
	b, _ := p.DeriveDomain("main", url)
	if a != b {
		t.Errorf("claude derivation not deterministic: %q != %q", a, b)
	}
}

func TestClaudeProfile_VariesWithInput(t *testing.T) {
	t.Parallel()
	p, _ := apps.HostProfileFor("claude")
	base, _ := p.DeriveDomain("main", "https://a.example.com/mcp")
	otherURL, _ := p.DeriveDomain("main", "https://b.example.com/mcp")
	otherLabel, _ := p.DeriveDomain("other", "https://a.example.com/mcp")
	if base == otherURL {
		t.Error("claude origin did not vary with server URL")
	}
	if base == otherLabel {
		t.Error("claude origin did not vary with domain label")
	}
}

func TestClaudeProfile_EmptyLabel(t *testing.T) {
	t.Parallel()
	p, _ := apps.HostProfileFor("claude")
	got, err := p.DeriveDomain("", "https://a.example.com/mcp")
	if err != nil || got != "" {
		t.Errorf("claude DeriveDomain(empty label) = %q, %v; want \"\", nil", got, err)
	}
}

func TestClaudeProfile_MissingServerURL(t *testing.T) {
	t.Parallel()
	p, _ := apps.HostProfileFor("claude")
	if _, err := p.DeriveDomain("main", ""); err == nil {
		t.Error("claude DeriveDomain with empty serverURL = nil error, want error")
	}
}
