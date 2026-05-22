package inspector

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/dockyard/internal/validate"
)

// TestVerdictsEndpoint_NoSource — with no Verdicts source configured, the
// `/api/verdicts` endpoint answers with an empty JSON array so the UI's
// four-state empty state renders cleanly.
func TestVerdictsEndpoint_NoSource(t *testing.T) {
	t.Parallel()
	insp, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")

	body := httpGet(t, insp.URL()+"/api/verdicts")
	if body != "[]\n" && body != "[]" {
		t.Fatalf("/api/verdicts with no source: got %q, want empty array", body)
	}
}

// TestVerdictsEndpoint_WithSource — a configured Verdicts source is surfaced
// verbatim through the `/api/verdicts` endpoint, re-run per request.
func TestVerdictsEndpoint_WithSource(t *testing.T) {
	t.Parallel()
	calls := 0
	src := func() []Verdict {
		calls++
		return []Verdict{
			{Check: "stale-codegen", Severity: verdictError, Message: "schema is stale"},
			{Check: "spec-compliance", Severity: verdictOK, Message: "spec OK"},
		}
	}
	insp, err := New(Options{Verdicts: src})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = insp.Serve(ctx) }()
	waitReady(t, insp.URL()+"/api/info")

	body := httpGet(t, insp.URL()+"/api/verdicts")
	if !contains(body, "stale-codegen") || !contains(body, "schema is stale") {
		t.Fatalf("/api/verdicts did not surface the source: %q", body)
	}
	// A second request re-runs the source — verdicts are never a stale cache.
	_ = httpGet(t, insp.URL()+"/api/verdicts")
	if calls < 2 {
		t.Fatalf("verdict source invoked %d times, want it re-run per request", calls)
	}
}

// TestContractsEndpoint exercises the `/api/contracts` source — the fixture
// switcher's generated-contract feed.
func TestContractsEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("no source yields an empty array", func(t *testing.T) {
		t.Parallel()
		insp, err := New(Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")
		if body := httpGet(t, insp.URL()+"/api/contracts"); body != "[]" {
			t.Fatalf("/api/contracts no source: got %q, want []", body)
		}
	})

	t.Run("a source is surfaced verbatim", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`[{"name":"report","outputSchema":{"type":"object"}}]`)
		insp, err := New(Options{
			Contracts: func() json.RawMessage { return raw },
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = insp.Serve(ctx) }()
		waitReady(t, insp.URL()+"/api/info")
		if body := httpGet(t, insp.URL()+"/api/contracts"); !contains(body, "report") {
			t.Fatalf("/api/contracts did not surface the source: %q", body)
		}
	})
}

// TestVerdictsFromReport maps validate Reports onto verdict rows: a clean
// report synthesises one "ok" verdict; a report with diagnostics maps each
// onto a severity-classified row.
func TestVerdictsFromReport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		report     *validate.Report
		wantLen    int
		wantFirst  string // expected first verdict severity
		wantChecks []string
	}{
		{
			name:      "nil report yields one ok verdict",
			report:    nil,
			wantLen:   1,
			wantFirst: verdictOK,
		},
		{
			name:      "empty report yields one ok verdict",
			report:    &validate.Report{},
			wantLen:   1,
			wantFirst: verdictOK,
		},
		{
			name: "blocker maps to an error verdict",
			report: &validate.Report{Diagnostics: []validate.Diagnostic{
				{Check: validate.CheckStaleCodegen, Severity: validate.Blocker,
					Message: "generated schema is stale"},
			}},
			wantLen:    1,
			wantFirst:  verdictError,
			wantChecks: []string{"stale-codegen"},
		},
		{
			name: "warning maps to a warn verdict",
			report: &validate.Report{Diagnostics: []validate.Diagnostic{
				{Check: validate.CheckSpec, Severity: validate.Warning,
					Message: "vague tool description"},
			}},
			wantLen:   1,
			wantFirst: verdictWarn,
		},
		{
			name: "mixed diagnostics map row-for-row",
			report: &validate.Report{Diagnostics: []validate.Diagnostic{
				{Check: validate.CheckSchema, Severity: validate.Blocker, Message: "bad schema"},
				{Check: validate.CheckUIStates, Severity: validate.Warning, Message: "missing empty state"},
			}},
			wantLen:    2,
			wantFirst:  verdictError,
			wantChecks: []string{"schema", "ui-states"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verdictsFromReport(tc.report)
			if len(got) != tc.wantLen {
				t.Fatalf("verdictsFromReport: got %d rows, want %d: %+v",
					len(got), tc.wantLen, got)
			}
			if got[0].Severity != tc.wantFirst {
				t.Fatalf("first verdict severity: got %q, want %q",
					got[0].Severity, tc.wantFirst)
			}
			for i, want := range tc.wantChecks {
				if got[i].Check != want {
					t.Fatalf("verdict %d check: got %q, want %q", i, got[i].Check, want)
				}
			}
		})
	}
}

// TestSeverityString maps every validate Severity onto its verdict string.
func TestSeverityString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   validate.Severity
		want string
	}{
		{validate.Blocker, verdictError},
		{validate.Warning, verdictWarn},
		{validate.Severity(99), verdictWarn},
	}
	for _, tc := range cases {
		if got := severityString(tc.in); got != tc.want {
			t.Fatalf("severityString(%v): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestVerdictsFromValidate_MissingProject — when validation cannot run at all
// (an empty ProjectDir), the source returns one "error" verdict, never a
// panic or an empty set: the panel degrades gracefully.
func TestVerdictsFromValidate_MissingProject(t *testing.T) {
	t.Parallel()
	src := VerdictsFromValidate("")
	got := src()
	if len(got) != 1 {
		t.Fatalf("VerdictsFromValidate(empty): got %d verdicts, want 1: %+v", len(got), got)
	}
	if got[0].Severity != verdictError {
		t.Fatalf("VerdictsFromValidate(empty): got severity %q, want %q",
			got[0].Severity, verdictError)
	}
}
