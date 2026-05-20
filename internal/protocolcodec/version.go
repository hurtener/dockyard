package protocolcodec

import (
	"errors"
	"fmt"
)

// ProtocolVersion is a negotiated MCP protocol-version string, e.g.
// "2025-11-25" or "2026-01-26". It is the key on which [CodecFor] selects a
// [Codec] (RFC §16 item 3).
type ProtocolVersion string

// Known MCP protocol versions Dockyard recognises. The list is deliberately
// small: V1 targets the spec version the pinned go-sdk supports (RFC §16
// item 6), and the Apps spec revision adds one more.
const (
	// VersionMCP20251125 is the current stable MCP spec version and the one
	// the pinned go-sdk targets (RFC §16 item 6).
	VersionMCP20251125 ProtocolVersion = "2025-11-25"
	// VersionApps20260126 is the MCP Apps spec revision (SEP-1865); the Apps
	// extension negotiates against it independently of the core spec version.
	VersionApps20260126 ProtocolVersion = "2026-01-26"
)

// DefaultVersion is the protocol version assumed when a peer negotiated none
// or negotiated an unrecognised one. Dockyard targets the stable core spec by
// default and degrades gracefully (RFC §16 items 6/7).
const DefaultVersion = VersionMCP20251125

// ErrUnknownVersion is returned (wrapped) by [CodecForStrict] when no codec is
// registered for a requested protocol version.
var ErrUnknownVersion = errors.New("protocolcodec: no codec registered for protocol version")

// codecRegistry maps a recognised protocol version to its codec. The wire
// shapes of the Apps (2026-01-26) and Tasks (experimental) extensions are
// stable across the versions Dockyard V1 supports, so every known version maps
// to the same [v1Codec] today. The registry exists so that the day a spec bump
// changes a shape, a NEW codec is registered for the new version and old peers
// keep their old codec — the forward-compatibility mechanism, made concrete.
var codecRegistry = map[ProtocolVersion]Codec{
	VersionMCP20251125:  v1Codec{},
	VersionApps20260126: v1Codec{},
}

// CodecFor returns the [Codec] for a negotiated protocol version, falling back
// to the [DefaultVersion] codec when version is empty or unrecognised. It never
// returns nil — graceful degradation is mandatory (RFC §16 item 7), so callers
// in the hot path use this. Use [CodecForStrict] when an unknown version must
// be surfaced as an error (e.g. in `dockyard validate`).
func CodecFor(version ProtocolVersion) Codec {
	if c, ok := codecRegistry[version]; ok {
		return c
	}
	return codecRegistry[DefaultVersion]
}

// CodecForStrict is like [CodecFor] but returns a wrapped [ErrUnknownVersion]
// instead of falling back, so tooling can flag an unrecognised protocol
// version rather than silently degrading.
func CodecForStrict(version ProtocolVersion) (Codec, error) {
	if c, ok := codecRegistry[version]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownVersion, version)
}

// KnownVersions returns the protocol versions protocolcodec has a codec for,
// in no particular order. Intended for diagnostics and tests.
func KnownVersions() []ProtocolVersion {
	out := make([]ProtocolVersion, 0, len(codecRegistry))
	for v := range codecRegistry {
		out = append(out, v)
	}
	return out
}
