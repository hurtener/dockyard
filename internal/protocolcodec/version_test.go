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
	c := CodecFor("9999-99-99")
	if c == nil {
		t.Fatal("CodecFor should never return nil")
	}
	def := CodecFor(DefaultVersion)
	if c.Version() != def.Version() {
		t.Errorf("unknown version did not fall back to default codec")
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
