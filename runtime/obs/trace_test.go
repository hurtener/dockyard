package obs

import "testing"

func TestNewTrace_WellFormed(t *testing.T) {
	t.Parallel()
	sc := NewTrace()
	if !isTraceID(sc.TraceID) {
		t.Errorf("TraceID %q is not a W3C trace-id (32 lowercase hex)", sc.TraceID)
	}
	if !isSpanID(sc.SpanID) {
		t.Errorf("SpanID %q is not a W3C span-id (16 lowercase hex)", sc.SpanID)
	}
	if sc.ParentID != "" {
		t.Errorf("a root trace must have no parent, got %q", sc.ParentID)
	}
}

func TestNewTrace_Unique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		sc := NewTrace()
		key := sc.TraceID + sc.SpanID
		if seen[key] {
			t.Fatalf("NewTrace produced a duplicate trace/span pair")
		}
		seen[key] = true
	}
}

func TestSpanContext_Child(t *testing.T) {
	t.Parallel()
	parent := NewTrace()
	child := parent.Child()
	if child.TraceID != parent.TraceID {
		t.Errorf("child trace-id %q != parent %q — a child stays in the same trace", child.TraceID, parent.TraceID)
	}
	if child.SpanID == parent.SpanID {
		t.Error("child must have a fresh span-id")
	}
	if child.ParentID != parent.SpanID {
		t.Errorf("child ParentID %q != parent SpanID %q", child.ParentID, parent.SpanID)
	}
	if !isSpanID(child.SpanID) {
		t.Errorf("child SpanID %q malformed", child.SpanID)
	}
}

func TestSpanContext_Child_PromotesZeroValue(t *testing.T) {
	t.Parallel()
	// A zero-value receiver (no trace yet) is promoted to a fresh root trace.
	child := SpanContext{}.Child()
	if !isTraceID(child.TraceID) || !isSpanID(child.SpanID) {
		t.Errorf("Child of a zero SpanContext must be a fresh root trace, got %+v", child)
	}
	if child.ParentID != "" {
		t.Errorf("promoted root trace must have no parent, got %q", child.ParentID)
	}
}

func TestSpanContext_IsZero(t *testing.T) {
	t.Parallel()
	if !(SpanContext{}).IsZero() {
		t.Error("a zero SpanContext must report IsZero")
	}
	if NewTrace().IsZero() {
		t.Error("a fresh trace must not report IsZero")
	}
}

func TestIsHex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    string
		n    int // expected character count, not byte count
		want bool
	}{
		{"0123456789abcdef", 16, true},
		{"0123456789ABCDEF", 16, false},  // uppercase rejected
		{"0123456789abcde", 16, false},   // too short
		{"0123456789abcdeff", 16, false}, // too long
		{"0123456789abcdeg", 16, false},  // non-hex
		{"", 0, true},
	}
	for _, tc := range tests {
		if got := isHex(tc.s, tc.n); got != tc.want {
			t.Errorf("isHex(%q,%d) = %v, want %v", tc.s, tc.n, got, tc.want)
		}
	}
}

func TestNewEventID_WellFormedAndUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := newEventID()
		if !isHex(id, eventIDBytes*2) {
			t.Fatalf("event id %q is not 32 lowercase hex", id)
		}
		if seen[id] {
			t.Fatalf("newEventID produced a duplicate")
		}
		seen[id] = true
	}
}
