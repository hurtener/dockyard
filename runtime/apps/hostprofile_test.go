package apps_test

import (
	"errors"
	"regexp"
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
func (fakeProfile) RequiresServerURL() bool                      { return false }

// signingProfile is a test-only host profile that declares RequiresServerURL —
// the signing-host shape the seam keeps a home for after the Claude derivation
// was retired (D-176). It proves the interface's RequiresServerURL branch and
// the testgate's empty-URL exemption stay exercised without shipping a
// synthesising built-in.
type signingProfile struct{ id string }

func (s signingProfile) ID() string { return s.id }
func (signingProfile) DeriveDomain(label, serverURL string) (string, error) {
	if label == "" {
		return "", nil
	}
	if serverURL == "" {
		return "", errors.New("signing profile requires a server URL")
	}
	return label + ".signed.example", nil
}
func (signingProfile) RequiresServerURL() bool { return true }

func TestHostProfileFor_BuiltIns(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"generic"} {
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
	if err := apps.RegisterHostProfile(fakeProfile{id: "generic"}); err == nil {
		t.Error("RegisterHostProfile(duplicate \"generic\") = nil, want error")
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

// TestSigningProfile_SeamRetained proves the host-profile seam still carries a
// signing-shaped profile after the Claude derivation was retired (D-176): a
// registered profile that declares RequiresServerURL derives a stable origin
// with a server URL and refuses one without it. The built-in registry ships
// only "generic" (verbatim); a host-blessed transform lives behind the seam
// exactly like this.
func TestSigningProfile_SeamRetained(t *testing.T) {
	t.Parallel()
	const id = "test-signing-seam-retained"
	if err := apps.RegisterHostProfile(signingProfile{id: id}); err != nil {
		t.Fatalf("RegisterHostProfile(%q): %v", id, err)
	}
	p, err := apps.HostProfileFor(id)
	if err != nil {
		t.Fatalf("HostProfileFor(%q): %v", id, err)
	}
	got, err := p.DeriveDomain("dashboard", "https://weather.example.com/mcp")
	if err != nil {
		t.Fatalf("signing DeriveDomain: %v", err)
	}
	if !dnsLabel.MatchString("dashboard") || got != "dashboard.signed.example" {
		t.Errorf("signing DeriveDomain = %q, want dashboard.signed.example", got)
	}
	if _, err := p.DeriveDomain("dashboard", ""); err == nil {
		t.Error("signing DeriveDomain with empty serverURL = nil error, want error")
	}
	if got, err := p.DeriveDomain("", "https://weather.example.com/mcp"); err != nil || got != "" {
		t.Errorf("signing DeriveDomain(empty label) = %q, %v; want \"\", nil", got, err)
	}
}

// TestHostProfile_RequiresServerURL is the table-driven assertion of the
// HostProfile method (D-165): a profile declares whether its derivation depends
// on a non-empty server URL. The testgate capability category consults this to
// exercise every profile honestly without a synthetic placeholder URL. The
// built-in "generic" profile requires none; a test signing profile stands in
// for the signing-host shape the seam retains (D-176).
func TestHostProfile_RequiresServerURL(t *testing.T) {
	t.Parallel()
	const signingID = "test-requires-server-url-signing"
	if err := apps.RegisterHostProfile(signingProfile{id: signingID}); err != nil {
		t.Fatalf("RegisterHostProfile(%q): %v", signingID, err)
	}
	tests := []struct {
		id   string
		want bool
	}{
		{"generic", false},
		{signingID, true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			p, err := apps.HostProfileFor(tt.id)
			if err != nil {
				t.Fatalf("HostProfileFor(%q): %v", tt.id, err)
			}
			if got := p.RequiresServerURL(); got != tt.want {
				t.Errorf("HostProfileFor(%q).RequiresServerURL() = %v, want %v",
					tt.id, got, tt.want)
			}
		})
	}
}
