package obs

import "encoding/json"

// CapturePolicy controls how much of a tool's input/output an emitted event
// carries. The default is shape + size only — never full content (CLAUDE.md §7,
// RFC §11.2): tool arguments and results can carry secrets and PII, so obs/v1
// captures the structural fingerprint and byte size by default.
//
// CapturePolicyFull is the designed-but-deferred opt-in hook for full-content
// capture. Phase 15 ships the shape+size default fully wired; full-content
// capture is opt-in and MUST be redaction-aware before any content is captured
// — that redaction pipeline is out of Phase 15 scope (see the Phase 15 plan's
// scope boundary). An emitter that receives CapturePolicyFull without a
// configured redactor falls back to shape+size — full capture is never the
// silent default.
type CapturePolicy int

const (
	// CapturePolicyShape captures only the structural shape and byte size of a
	// value — the safe, content-free default.
	CapturePolicyShape CapturePolicy = iota
	// CapturePolicyFull is the opt-in full-content capture mode. It is honoured
	// only when a redaction-aware [Redactor] is configured; otherwise an
	// emitter degrades to CapturePolicyShape. Phase 15 defines the hook; the
	// redaction pipeline is later scope.
	CapturePolicyFull
)

// Redactor is the redaction-aware hook full-content capture requires. A
// Redactor receives a raw JSON value and returns a redaction-applied copy safe
// to embed in an event. Phase 15 defines the interface so [CapturePolicyFull]
// has a contract to bind to; the concrete redactor implementation is later
// scope. Until one is supplied, full-content capture degrades to shape+size.
type Redactor interface {
	// Redact returns a redacted copy of raw safe for capture in an obs/v1
	// event. It must never panic and must never return more data than raw.
	Redact(raw json.RawMessage) json.RawMessage
}

// ValueKind is the JSON structural category of a captured value (see [Shape]).
type ValueKind string

const (
	// KindNull is a JSON null or an absent value.
	KindNull ValueKind = "null"
	// KindBool is a JSON boolean.
	KindBool ValueKind = "bool"
	// KindNumber is a JSON number.
	KindNumber ValueKind = "number"
	// KindString is a JSON string.
	KindString ValueKind = "string"
	// KindArray is a JSON array.
	KindArray ValueKind = "array"
	// KindObject is a JSON object.
	KindObject ValueKind = "object"
)

// ValueShape is the content-free structural fingerprint of a JSON value: its
// kind, byte size, and — for objects and arrays — its top-level field names or
// element count. It deliberately carries NO values, only structure, so it is
// always safe to emit even under the default capture policy (CLAUDE.md §7).
type ValueShape struct {
	// Kind is the JSON structural category.
	Kind ValueKind `json:"kind"`
	// Bytes is the size of the value's JSON encoding — the size guardrail
	// signal the inspector surfaces.
	Bytes int `json:"bytes"`
	// Fields is the sorted set of top-level keys, for an object value. It is
	// the field NAMES only — never the values. Nil for non-objects.
	Fields []string `json:"fields,omitempty"`
	// Len is the element count, for an array value. Nil for non-arrays.
	Len *int `json:"len,omitempty"`
}

// Shape computes the content-free [ValueShape] of a raw JSON value. A nil or
// empty input yields a null shape. Malformed JSON yields a shape whose Kind is
// the best-effort guess from the leading byte and whose Bytes is the raw
// length — Shape never fails: observability must not fail a request (P2).
func Shape(raw json.RawMessage) ValueShape {
	if len(raw) == 0 {
		return ValueShape{Kind: KindNull, Bytes: 0}
	}
	s := ValueShape{Bytes: len(raw)}

	// A JSON null unmarshals into a map/slice as a nil value with no error, so
	// it must be ruled out before the object/array probes below.
	if isJSONNull(raw) {
		s.Kind = KindNull
		return s
	}

	// Object: capture the top-level field NAMES (never values).
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		s.Kind = KindObject
		s.Fields = make([]string, 0, len(obj))
		for k := range obj {
			s.Fields = append(s.Fields, k)
		}
		sortStrings(s.Fields)
		return s
	}

	// Array: capture the element COUNT only.
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil && arr != nil {
		s.Kind = KindArray
		n := len(arr)
		s.Len = &n
		return s
	}

	// Scalars.
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		s.Kind = KindBool
		return s
	}
	var num float64
	if json.Unmarshal(raw, &num) == nil {
		s.Kind = KindNumber
		return s
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		s.Kind = KindString
		return s
	}
	// Explicit JSON null or unparseable input.
	s.Kind = KindNull
	return s
}

// captureValue applies a [CapturePolicy] to a raw JSON value, returning the
// always-safe [ValueShape] and the full content only when the policy is
// [CapturePolicyFull] AND a redactor is supplied. It is the single chokepoint
// every emitter routes tool input/output through, so the shape+size default —
// and the redaction-aware gating of full capture — is enforced in one place.
func captureValue(raw json.RawMessage, policy CapturePolicy, r Redactor) (ValueShape, json.RawMessage) {
	shape := Shape(raw)
	if policy == CapturePolicyFull && r != nil && len(raw) > 0 {
		return shape, r.Redact(raw)
	}
	return shape, nil
}

// isJSONNull reports whether raw is the JSON literal null (ignoring surrounding
// whitespace). A JSON null decodes into a map or slice as a nil value with no
// error, so Shape must detect it explicitly.
func isJSONNull(raw json.RawMessage) bool {
	s := trimJSONSpace(string(raw))
	return s == "null"
}

// trimJSONSpace trims the JSON insignificant-whitespace set from both ends.
func trimJSONSpace(s string) string {
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	for len(s) > 0 && isSpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

// sortStrings is a tiny insertion sort kept local so the package's only
// stdlib-sort dependency is not pulled in for one short slice.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
