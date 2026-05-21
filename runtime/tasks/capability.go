package tasks

import (
	"encoding/json"
	"fmt"

	"github.com/hurtener/dockyard/internal/protocolcodec"
)

// ExtensionID is the MCP Tasks extension identifier, exactly as registered in
// the MCP capability registry (SEP-1686/2663).
const ExtensionID = protocolcodec.ExtensionTasks

// CapabilityKey is the initialize-handshake key the Tasks capability block is
// advertised under. Per the vendored schema the Tasks capability is the
// top-level `capabilities.tasks` object — NOT an entry under
// `capabilities.extensions`. The MCP Tasks extension predates and sits
// alongside the generic extensions mechanism (vendored spec, "Capabilities").
const CapabilityKey = "tasks"

// CapabilityJSON returns the JSON value of the engine's `capabilities.tasks`
// block — the object a receiver advertises during the initialize handshake so
// a requestor knows which Tasks operations are supported (vendored spec,
// "Capabilities"; brief 02 §2.6).
//
// The shape is produced through internal/protocolcodec; this package never
// hand-builds the wire JSON (P3). It is capability-driven — the block reflects
// the engine's actual configuration, never a hardcoded per-host matrix
// (AGENTS.md §6).
//
// Wiring it into the handshake: the go-sdk has no native `capabilities.tasks`
// field (the extension is experimental — RFC §8.2), so the Phase 14 transport
// mount injects this block into the initialize result alongside routing
// tasks/* into [Engine.Dispatch]. Phase 13 produces the value and proves it
// correct; Phase 13 does not itself mutate the SDK handshake.
func (e *Engine) CapabilityJSON() (json.RawMessage, error) {
	raw, err := e.codec.EncodeTasksServerCapability(e.Capability())
	if err != nil {
		return nil, fmt.Errorf("dockyard/runtime/tasks: encode tasks capability: %w", err)
	}
	return raw, nil
}
