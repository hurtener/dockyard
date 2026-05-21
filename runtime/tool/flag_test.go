package tool_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/runtime/tool"
)

// TestLooksLikeJSONPayload covers the misroute heuristic: a JSON object or
// array is UI-shaped data, while prose and bare JSON scalars are legitimate
// model-facing text.
func TestLooksLikeJSONPayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want bool
	}{
		{`{"headline":"Revenue"}`, true},
		{`[1,2,3]`, true},
		{"  \n {\"a\":1}\t ", true},
		{"revenue for 2026-Q1", false},
		{"", false},
		{"{", false},
		{`"a bare json string"`, false},
		{"42", false},
		{"true", false},
		{`{not valid json}`, false},
	}
	for _, tc := range cases {
		if got := tool.LooksLikeJSONPayloadForTest(tc.text); got != tc.want {
			t.Errorf("looksLikeJSONPayload(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// TestDetectFlagsOversize proves an oversized structuredContent payload raises
// FlagOversizeOutput, and a within-budget payload raises none.
func TestDetectFlagsOversize(t *testing.T) {
	t.Parallel()
	big := make([]byte, 2048)
	for i := range big {
		big[i] = 'x'
	}
	flags := tool.DetectFlagsForTest("report", "fine prose", big, 1024)
	if len(flags) != 1 {
		t.Fatalf("flags = %+v, want exactly one oversize flag", flags)
	}
	if flags[0].Kind != tool.FlagOversizeOutput {
		t.Errorf("flag kind = %v, want FlagOversizeOutput", flags[0].Kind)
	}
	if flags[0].Tool != "report" {
		t.Errorf("flag tool = %q, want report", flags[0].Tool)
	}
	if flags[0].SizeBytes != len(big) {
		t.Errorf("flag SizeBytes = %d, want %d", flags[0].SizeBytes, len(big))
	}
	if !strings.Contains(flags[0].String(), "oversize-output") {
		t.Errorf("flag String() = %q, want it to name the kind", flags[0].String())
	}

	if got := tool.DetectFlagsForTest("report", "fine", big, 0); len(got) != 0 {
		t.Errorf("a zero budget disables the oversize check, got %+v", got)
	}
	if got := tool.DetectFlagsForTest("report", "fine", []byte("small"), 1024); len(got) != 0 {
		t.Errorf("a within-budget payload should raise no flag, got %+v", got)
	}
}

// TestDetectFlagsMisroute proves UI-shaped data in the model-facing Text raises
// FlagMisroutedContent.
func TestDetectFlagsMisroute(t *testing.T) {
	t.Parallel()
	flags := tool.DetectFlagsForTest("report", `{"headline":"Revenue","total":1200}`, []byte("{}"), 1024)
	if len(flags) != 1 {
		t.Fatalf("flags = %+v, want exactly one misroute flag", flags)
	}
	if flags[0].Kind != tool.FlagMisroutedContent {
		t.Errorf("flag kind = %v, want FlagMisroutedContent", flags[0].Kind)
	}
	if flags[0].SizeBytes != 0 {
		t.Errorf("misroute flag SizeBytes = %d, want 0", flags[0].SizeBytes)
	}
}

// TestDetectFlagsBoth proves the two flags are independent and can co-occur.
func TestDetectFlagsBoth(t *testing.T) {
	t.Parallel()
	big := make([]byte, 4096)
	flags := tool.DetectFlagsForTest("report", `[1,2,3]`, big, 1024)
	if len(flags) != 2 {
		t.Fatalf("flags = %+v, want both an oversize and a misroute flag", flags)
	}
}

// TestFlagKindString covers the FlagKind stringer, including the default arm.
func TestFlagKindString(t *testing.T) {
	t.Parallel()
	if got := tool.FlagOversizeOutput.String(); got != "oversize-output" {
		t.Errorf("FlagOversizeOutput.String() = %q", got)
	}
	if got := tool.FlagMisroutedContent.String(); got != "misrouted-content" {
		t.Errorf("FlagMisroutedContent.String() = %q", got)
	}
	if got := tool.FlagKind(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown FlagKind.String() = %q, want it to show the number", got)
	}
}

// TestBuilderFlagsAccessor proves Builder.Flags surfaces flags raised by a
// registered tool's handler — an oversized output here — and that Flags returns
// nil before Register.
func TestBuilderFlagsAccessor(t *testing.T) {
	t.Parallel()

	// Before Register, Flags is nil.
	b := tool.New[revenueInput, revenueOutput]("pre").Handler(revenueHandler)
	if got := b.Flags(); got != nil {
		t.Errorf("Flags() before Register = %+v, want nil", got)
	}

	// A handler whose Text is JSON-shaped raises a misroute flag.
	s := newServer(t)
	misrouter := tool.New[revenueInput, revenueOutput]("misrouter").
		Handler(func(_ context.Context, _ revenueInput) (tool.Result[revenueOutput], error) {
			return tool.Result[revenueOutput]{
				Text:       `{"headline":"leaked into content"}`,
				Structured: revenueOutput{Headline: "ok"},
			}, nil
		})
	if err := misrouter.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := misrouter.Flags(); got != nil {
		t.Errorf("Flags() before any call = %+v, want nil", got)
	}

	res := callWithRawArgs(t, s, "misrouter", `{"period":"2026-Q1"}`)
	if res.IsError {
		t.Fatalf("CallTool IsError: %+v", res.Content)
	}
	flags := misrouter.Flags()
	if len(flags) != 1 || flags[0].Kind != tool.FlagMisroutedContent {
		t.Fatalf("Flags() = %+v, want one FlagMisroutedContent", flags)
	}
	if flags[0].Tool != "misrouter" {
		t.Errorf("flag tool = %q, want misrouter", flags[0].Tool)
	}
}
