package handlers

import (
	"testing"

	"github.com/hurtener/dockyard/runtime/tasks"
)

// TestRequestApprovalSummary covers every branch of the summary text the
// request_approval handler writes alongside its CallToolResult — declined,
// approved, rejected, and the defensive "error decoding reply" fall-through
// for a corrupt InputResponse payload. The summary is part of the
// scaffolded server's user-facing text; it must not panic on bad input.
func TestRequestApprovalSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		reply tasks.InputResponse
		want  string
	}{
		{
			name:  "declined wins regardless of any data",
			reply: tasks.InputResponse{Declined: true, Data: []byte(`{"approved":true}`)},
			want:  "declined",
		},
		{
			name:  "approved when the wire reply says so",
			reply: tasks.InputResponse{Data: []byte(`{"approved":true}`)},
			want:  "approved",
		},
		{
			name:  "rejected when approved is false",
			reply: tasks.InputResponse{Data: []byte(`{"approved":false}`)},
			want:  "rejected",
		},
		{
			name:  "empty data is treated as approved (default reply)",
			reply: tasks.InputResponse{},
			want:  "approved",
		},
		{
			name:  "malformed data yields the defensive fall-through",
			reply: tasks.InputResponse{Data: []byte(`not-json`)},
			want:  "error decoding reply",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := requestApprovalSummary(c.reply); got != c.want {
				t.Errorf("requestApprovalSummary = %q, want %q", got, c.want)
			}
		})
	}
}

// TestProposeWithEditsSummary covers the propose_with_edits variant of the
// same helper — the only behavioural difference vs requestApprovalSummary
// is the "approved with edits" wording on the approve path.
func TestProposeWithEditsSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		reply tasks.InputResponse
		want  string
	}{
		{
			name:  "declined wins",
			reply: tasks.InputResponse{Declined: true},
			want:  "declined",
		},
		{
			name:  "approved with edits when the wire reply says so",
			reply: tasks.InputResponse{Data: []byte(`{"approved":true}`)},
			want:  "approved with edits",
		},
		{
			name:  "rejected when approved is false",
			reply: tasks.InputResponse{Data: []byte(`{"approved":false}`)},
			want:  "rejected",
		},
		{
			name:  "empty data is approved",
			reply: tasks.InputResponse{},
			want:  "approved with edits",
		},
		{
			name:  "malformed data yields the defensive fall-through",
			reply: tasks.InputResponse{Data: []byte(`{"`)},
			want:  "error decoding reply",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := proposeWithEditsSummary(c.reply); got != c.want {
				t.Errorf("proposeWithEditsSummary = %q, want %q", got, c.want)
			}
		})
	}
}
