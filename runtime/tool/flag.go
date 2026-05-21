package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultOutputSizeBudget is the conservative size budget, in bytes, the handler
// runtime uses to flag an oversized tool output. It is a heuristic, not an MCP
// protocol limit: a structuredContent payload larger than this is *flagged*, not
// rejected — a large-but-legitimate payload stays observable rather than blocked
// (RFC §6.3, the braindump's "oversized output payloads" caution; D-045).
const DefaultOutputSizeBudget = 256 * 1024

// FlagKind classifies a handler-runtime payload-routing defect.
type FlagKind int

const (
	// FlagOversizeOutput marks a tool output whose serialized structuredContent
	// exceeds the size budget (DefaultOutputSizeBudget).
	FlagOversizeOutput FlagKind = iota + 1
	// FlagMisroutedContent marks model-facing Text that is itself UI-shaped
	// data — a JSON object or array — and so pollutes and inflates the model
	// context instead of being routed to structuredContent (RFC §6.3).
	FlagMisroutedContent
)

// String renders a FlagKind for logs and diagnostics.
func (k FlagKind) String() string {
	switch k {
	case FlagOversizeOutput:
		return "oversize-output"
	case FlagMisroutedContent:
		return "misrouted-content"
	default:
		return fmt.Sprintf("FlagKind(%d)", int(k))
	}
}

// Flag is a typed, non-fatal handler-runtime signal: an oversized output or a
// misrouted payload. A Flag never fails the tool call — it is recorded so the
// defect is observable in Dockyard's own surfaces before a host ever sees the
// result (brief 03 R7, the same principle as typed _meta accessors). A future
// obs/v1 bridge consumes these; Phase 08 exposes them through Builder.Flags.
type Flag struct {
	// Kind classifies the defect.
	Kind FlagKind
	// Tool is the wire name of the tool whose call raised the flag.
	Tool string
	// Detail is a human-readable explanation.
	Detail string
	// SizeBytes is the serialized payload size in bytes for FlagOversizeOutput;
	// zero for kinds where size is not meaningful.
	SizeBytes int
}

// String renders a Flag for logs and diagnostics.
func (f Flag) String() string {
	if f.SizeBytes > 0 {
		return fmt.Sprintf("[%s] tool %q: %s (%d bytes)", f.Kind, f.Tool, f.Detail, f.SizeBytes)
	}
	return fmt.Sprintf("[%s] tool %q: %s", f.Kind, f.Tool, f.Detail)
}

// detectFlags inspects a single tool call's split result and returns the
// routing flags it raises, if any. It is pure: same inputs, same flags — which
// keeps it safe to call from concurrent tool handlers (the caller owns the
// accumulation). text is the model-facing Result.Text; structuredJSON is the
// serialized structuredContent payload.
func detectFlags(toolName, text string, structuredJSON []byte, budget int) []Flag {
	var flags []Flag
	if budget > 0 && len(structuredJSON) > budget {
		flags = append(flags, Flag{
			Kind:      FlagOversizeOutput,
			Tool:      toolName,
			Detail:    fmt.Sprintf("structuredContent is %d bytes, over the %d-byte budget", len(structuredJSON), budget),
			SizeBytes: len(structuredJSON),
		})
	}
	if looksLikeJSONPayload(text) {
		flags = append(flags, Flag{
			Kind:   FlagMisroutedContent,
			Tool:   toolName,
			Detail: "model-facing Text is JSON-shaped data — route UI payloads to structuredContent, not content[] (RFC §6.3)",
		})
	}
	return flags
}

// looksLikeJSONPayload reports whether s is UI-shaped data — a JSON object or
// array — rather than model-facing prose. A bare JSON string, number, or
// boolean is not flagged: those are legitimate model-facing text. Detection is
// deliberately high-confidence: it only fires when the whole trimmed string
// parses as a JSON object or array.
func looksLikeJSONPayload(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 2 {
		return false
	}
	switch t[0] {
	case '{', '[':
	default:
		return false
	}
	return json.Valid([]byte(t))
}
