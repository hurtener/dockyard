package obs

import (
	"encoding/json"
	"testing"
)

func TestShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		raw       string
		wantKind  ValueKind
		wantBytes int
		wantField []string
		wantLen   *int
	}{
		{"null", `null`, KindNull, 4, nil, nil},
		{"empty", ``, KindNull, 0, nil, nil},
		{"bool", `true`, KindBool, 4, nil, nil},
		{"number", `3.14`, KindNumber, 4, nil, nil},
		{"string", `"hi"`, KindString, 4, nil, nil},
		{"object", `{"b":2,"a":1}`, KindObject, 13, []string{"a", "b"}, nil},
		{"array", `[1,2,3]`, KindArray, 7, nil, intp(3)},
		{"empty object", `{}`, KindObject, 2, []string{}, nil},
		{"empty array", `[]`, KindArray, 2, nil, intp(0)},
		{"malformed", `{not json`, KindNull, 9, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Shape(json.RawMessage(tc.raw))
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Bytes != tc.wantBytes {
				t.Errorf("Bytes = %d, want %d", got.Bytes, tc.wantBytes)
			}
			if !strSliceEq(got.Fields, tc.wantField) {
				t.Errorf("Fields = %v, want %v", got.Fields, tc.wantField)
			}
			if (got.Len == nil) != (tc.wantLen == nil) {
				t.Fatalf("Len presence mismatch: got %v want %v", got.Len, tc.wantLen)
			}
			if got.Len != nil && *got.Len != *tc.wantLen {
				t.Errorf("Len = %d, want %d", *got.Len, *tc.wantLen)
			}
		})
	}
}

// TestShape_NeverCapturesValues is the CLAUDE.md §7 guard: a ValueShape carries
// the field NAMES of an object, never the values — no content leaks.
func TestShape_NeverCapturesValues(t *testing.T) {
	t.Parallel()
	secret := `{"password":"hunter2","token":"abc123"}`
	s := Shape(json.RawMessage(secret))
	b, _ := json.Marshal(s)
	for _, leak := range []string{"hunter2", "abc123"} {
		if containsSub(string(b), leak) {
			t.Fatalf("ValueShape leaked a value %q: %s", leak, b)
		}
	}
	// The field NAMES are present — that is the intended structural fingerprint.
	if len(s.Fields) != 2 {
		t.Fatalf("expected 2 field names, got %v", s.Fields)
	}
}

func TestCaptureValue_ShapeDefault(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"secret":"value"}`)
	// Default policy: shape only, no full content captured.
	shape, full := captureValue(raw, CapturePolicyShape, nil)
	if shape.Kind != KindObject {
		t.Errorf("shape Kind = %q, want object", shape.Kind)
	}
	if full != nil {
		t.Errorf("CapturePolicyShape must NOT capture full content, got %s", full)
	}
}

func TestCaptureValue_FullWithoutRedactorDegrades(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"secret":"value"}`)
	// CapturePolicyFull WITHOUT a redactor must degrade to shape only —
	// full-content capture is never the silent default (CLAUDE.md §7).
	_, full := captureValue(raw, CapturePolicyFull, nil)
	if full != nil {
		t.Errorf("CapturePolicyFull without a redactor must degrade to shape, got %s", full)
	}
}

// fakeRedactor replaces every value with "***" — a stand-in proving the
// CapturePolicyFull hook is wired; the real redaction pipeline is later scope.
type fakeRedactor struct{}

func (fakeRedactor) Redact(json.RawMessage) json.RawMessage {
	return json.RawMessage(`{"redacted":true}`)
}

func TestCaptureValue_FullWithRedactor(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"secret":"value"}`)
	shape, full := captureValue(raw, CapturePolicyFull, fakeRedactor{})
	if shape.Kind != KindObject {
		t.Errorf("shape still computed: Kind = %q", shape.Kind)
	}
	if string(full) != `{"redacted":true}` {
		t.Errorf("full capture must pass through the redactor, got %s", full)
	}
}

func intp(n int) *int { return &n }

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
