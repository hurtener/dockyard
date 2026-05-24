// Package conformance is Dockyard's MCP spec-compliance conformance suite
// (Phase 27, RFC §16). It rounds-trips every Apps + Tasks wire shape
// through internal/protocolcodec against fixtures derived from the
// vendored spec snapshots under docs/specifications/, asserting:
//
//   - the codec accepts every shape the spec mandates (decode succeeds);
//   - the codec re-emits a canonical wire shape that the spec accepts;
//   - the codec tolerates deprecated shapes on read and never emits them;
//   - codecs are versioned + keyed on the negotiated protocolVersion: an
//     unknown version surfaces a typed error from CodecForStrict, and an
//     advertised capability is served (the capability-negotiation
//     invariant).
//
// Each test cites the vendored spec snapshot it derives its fixtures from
// — the snapshot's pinned commit SHA + date are recorded in the file
// header in docs/specifications/. A spec bump that changes a wire shape
// is therefore visible as a diff in BOTH the snapshot and the conformance
// fixture, never silent.
//
// The conformance suite differs from internal/protocolcodec's golden tests
// (which assert one expected wire string for one constructed input): the
// conformance suite drives the codec from the OUTSIDE — given a spec-
// canonical input, decode it, re-encode it, and assert the round-trip is
// byte-stable against a fixture. Where the spec allows optional fields,
// the suite exercises both presence and absence; where it allows
// deprecated shapes, the suite exercises the toleration + the emission
// policy.
//
// Conformance fixtures live under fixtures/ as JSON files whose top-level
// comment field (the convention `_meta._cite`) names the vendored spec
// snapshot section the fixture is derived from. The fixture loader
// strips the _meta._cite marker before comparison so the comparison is
// against the wire shape only.
package conformance
