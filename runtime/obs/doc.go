// Package obs is Dockyard's observability protocol — obs/v1 (RFC §11).
//
// obs/v1 is a headless, canonical, versioned event stream. The app runtime
// EMITS obs/v1 events; the inspector (RFC §12) and the post-V1 multi-server
// console are pure CLIENTS of that contract and never read runtime internals
// (P2, CLAUDE.md §1, §6). If a subsystem needs a signal observed, it adds an
// obs/v1 event — never a back channel.
//
// # The contract
//
// obs/v1 is a public, documented, third-party-consumable contract (RFC §11.3,
// CLAUDE.md §8). The serialized shape of [Event] is stable from V1: a change to
// the JSON shape is a versioned change ([SchemaVersion] bumps), documented,
// never silent. The wire shape is pinned by golden tests.
//
// # The emitter seam
//
// The runtime depends only on the [Emitter] interface. obs follows the
// interface + factory + driver pattern mandated by CLAUDE.md §4.4: a driver
// registers a factory in its init block via [RegisterDriver], and [Open]
// constructs an Emitter by driver name. Phase 15 ships the ring-buffer driver
// ([RingBuffer], driver name "ringbuffer"); Phase 16 adds the out-of-band SSE
// sink and the optional OTel adapter behind the same seam, and bridges the MCP
// logging capability into obs/v1 log events.
//
// # Non-blocking
//
// Emit paths are non-blocking: the runtime never blocks on a slow consumer
// (CLAUDE.md §8). The ring-buffer driver is a bounded ring — a full buffer
// drops its oldest event, it never stalls an emitter. A multi-driver [FanOut]
// is likewise bounded and drop-on-pressure. Every emitter and the ring buffer
// are reusable concurrent artifacts: safe for concurrent Emit from many
// goroutines and concurrent reads (CLAUDE.md §5).
//
// # Capture policy
//
// Tool input/output capture defaults to shape + size only — never full content
// (CLAUDE.md §7). [Shape] computes the structural fingerprint and byte size of
// a JSON value; full-content capture is opt-in and redaction-aware and is left
// as a designed-but-deferred hook ([CapturePolicy]) — see RFC §11.2 and the
// Phase 15 plan's scope boundary.
package obs
