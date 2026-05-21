package protocolcodec

import (
	"errors"
	"testing"
)

func TestCodecFor_KnownVersions(t *testing.T) {
	for _, v := range KnownVersions() {
		c := CodecFor(v)
		if c == nil {
			t.Fatalf("CodecFor(%q) returned nil", v)
		}
	}
}

func TestCodecFor_UnknownFallsBackToDefault(t *testing.T) {
	const unknown ProtocolVersion = "9999-99-99"

	// Guard the premise: the version must genuinely be unregistered, otherwise
	// this test would exercise the hit path, not the fallback path.
	if _, registered := codecRegistry[unknown]; registered {
		t.Fatalf("test premise broken: %q is registered", unknown)
	}

	c := CodecFor(unknown)
	if c == nil {
		t.Fatal("CodecFor should never return nil")
	}
	// The fallback must hand back the exact codec the registry holds for the
	// default version. Comparing against the registry entry proves the fallback
	// branch in CodecFor actually selected DefaultVersion.
	want := codecRegistry[DefaultVersion]
	if c != want {
		t.Errorf("unknown version did not fall back to the default-registry codec: got %#v want %#v", c, want)
	}
	// And the fallback codec reports the default version, not the unknown one.
	if got := c.Version(); got != DefaultVersion {
		t.Errorf("fallback codec Version() = %q, want %q", got, DefaultVersion)
	}
}

// TestCodecFor_VersionReportsSelectedKey proves the D-055 fix: the codec
// CodecFor returns reports the protocol version it was selected as — the
// registry key — not a hardcoded DefaultVersion.
func TestCodecFor_VersionReportsSelectedKey(t *testing.T) {
	for _, v := range KnownVersions() {
		if got := CodecFor(v).Version(); got != v {
			t.Errorf("CodecFor(%q).Version() = %q, want %q", v, got, v)
		}
		strict, err := CodecForStrict(v)
		if err != nil {
			t.Fatalf("CodecForStrict(%q): %v", v, err)
		}
		if got := strict.Version(); got != v {
			t.Errorf("CodecForStrict(%q).Version() = %q, want %q", v, got, v)
		}
	}
}

func TestCodecFor_EmptyFallsBackToDefault(t *testing.T) {
	if CodecFor("") == nil {
		t.Fatal("CodecFor(\"\") returned nil")
	}
}

func TestCodecForStrict(t *testing.T) {
	if _, err := CodecForStrict(VersionMCP20251125); err != nil {
		t.Fatalf("known version: %v", err)
	}
	_, err := CodecForStrict("not-a-version")
	if !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("want ErrUnknownVersion, got %v", err)
	}
}

func TestKnownVersionsIncludesBothSpecs(t *testing.T) {
	var hasCore, hasApps bool
	for _, v := range KnownVersions() {
		switch v {
		case VersionMCP20251125:
			hasCore = true
		case VersionApps20260126:
			hasApps = true
		}
	}
	if !hasCore || !hasApps {
		t.Errorf("KnownVersions missing a spec: core=%v apps=%v", hasCore, hasApps)
	}
}
