package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"

	"github.com/hurtener/dockyard/templates/approval-flows/pkg/contracts"
)

// TestRequestApproval_NoEngine_DegradesGracefully covers the
// capability-degradation path: when the host did not negotiate Tasks
// (Engine is nil), the handler returns the prompt verbatim with a
// non-error message so the App renders a static prompt.
func TestRequestApproval_NoEngine_DegradesGracefully(t *testing.T) {
	t.Parallel()
	h := CreateRequestApproval{Engine: nil}
	in := contracts.RequestApprovalInput{
		Title:       "Send the weekly digest?",
		Description: "Send to 1,247 subscribers.",
	}
	res, err := h.Handler(context.Background(), in)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.Structured.Kind != "approval" {
		t.Errorf("Kind = %q, want approval", res.Structured.Kind)
	}
	if res.Structured.State != "awaiting" {
		t.Errorf("State = %q, want awaiting", res.Structured.State)
	}
	if res.Structured.Message == "" {
		t.Error("expected a degradation Message — got empty")
	}
}

// TestRequestApproval_EmptyPrompt drives the empty-state edge.
func TestRequestApproval_EmptyPrompt(t *testing.T) {
	t.Parallel()
	h := CreateRequestApproval{Engine: nil}
	res, err := h.Handler(context.Background(), contracts.RequestApprovalInput{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.Structured.State != "empty" {
		t.Errorf("State = %q, want empty", res.Structured.State)
	}
}

// TestProposeWithEdits_NoFields drives the empty-state edge.
func TestProposeWithEdits_NoFields(t *testing.T) {
	t.Parallel()
	h := CreateProposeWithEdits{Engine: nil}
	res, err := h.Handler(context.Background(), tool.Call[contracts.ProposeWithEditsInput]{Input: contracts.ProposeWithEditsInput{
		Title:       "Send email",
		Description: "Body update",
		Fields:      []contracts.Field{},
	}})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.Structured.State != "empty" {
		t.Errorf("State = %q, want empty", res.Structured.State)
	}
}

func TestProposeWithEdits_CoreMRTRThenCreatesTask(t *testing.T) {
	engine, err := tasks.NewEngine(tasks.NewInMemoryStore(), &tasks.Options{
		GenerateID: func() (string, error) { return "proposal-task", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h := CreateProposeWithEdits{Engine: engine}
	input := contracts.ProposeWithEditsInput{Title: "Review email", Fields: []contracts.Field{{Key: "subject", Proposed: "Hello"}}}
	first, err := h.Handler(context.Background(), tool.Call[contracts.ProposeWithEditsInput]{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedTask != nil || first.InputRequests[approvalInputKey] == nil {
		t.Fatalf("first result = %+v, want core MRTR input request before task creation", first)
	}
	second, err := h.Handler(context.Background(), tool.Call[contracts.ProposeWithEditsInput]{
		Input: input,
		InputResponses: map[string]tool.InputResponse{approvalInputKey: tool.ElicitationResponse{
			Action: "accept", Content: map[string]any{"approved": true, "subject": "Hi"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedTask == nil || second.CreatedTask.ID != "proposal-task" || !second.CreatedTask.Required {
		t.Fatalf("retry result CreatedTask = %+v", second.CreatedTask)
	}
}

// TestBuildRequestApprovalTerminal_Approved verifies the terminal payload
// is built from an Approved reply.
func TestBuildRequestApprovalTerminal_Approved(t *testing.T) {
	t.Parallel()
	prompt := contracts.RequestApprovalOutput{
		Kind:        "approval",
		Title:       "Send the weekly digest?",
		Description: "Send to 1,247 subscribers.",
		State:       "awaiting",
	}
	replyData, _ := json.Marshal(contracts.ApprovalReply{
		Approved: true,
		Reason:   "Looks fine, ship it.",
	})
	out := buildRequestApprovalTerminal(prompt, tasks.InputResponse{Data: replyData})
	if out.State != "approved" {
		t.Errorf("State = %q, want approved", out.State)
	}
	if out.Approved == nil || !*out.Approved {
		t.Errorf("Approved = %v, want true", out.Approved)
	}
	if out.Reason != "Looks fine, ship it." {
		t.Errorf("Reason = %q", out.Reason)
	}
	if out.DecidedAt.IsZero() {
		t.Error("DecidedAt should be set")
	}
}

// TestBuildRequestApprovalTerminal_Rejected covers the rejected path.
func TestBuildRequestApprovalTerminal_Rejected(t *testing.T) {
	t.Parallel()
	prompt := contracts.RequestApprovalOutput{Title: "x"}
	replyData, _ := json.Marshal(contracts.ApprovalReply{
		Approved: false,
		Reason:   "Audience is too broad.",
	})
	out := buildRequestApprovalTerminal(prompt, tasks.InputResponse{Data: replyData})
	if out.State != "rejected" {
		t.Errorf("State = %q, want rejected", out.State)
	}
	if out.Approved == nil || *out.Approved {
		t.Errorf("Approved = %v, want false", out.Approved)
	}
	if out.Reason != "Audience is too broad." {
		t.Errorf("Reason = %q", out.Reason)
	}
}

// TestBuildRequestApprovalTerminal_Declined covers the declined path.
func TestBuildRequestApprovalTerminal_Declined(t *testing.T) {
	t.Parallel()
	out := buildRequestApprovalTerminal(
		contracts.RequestApprovalOutput{Title: "x"},
		tasks.InputResponse{Declined: true},
	)
	if out.State != "rejected" {
		t.Errorf("State = %q, want rejected (declined)", out.State)
	}
	if out.Approved == nil || *out.Approved {
		t.Error("Approved should be false when declined")
	}
}

// TestBuildProposeWithEditsTerminal_ApprovedFinalisesEdits covers the
// edit-merging path: the App posts only the edited fields; the handler
// fills in the proposed defaults for everything else.
func TestBuildProposeWithEditsTerminal_ApprovedFinalisesEdits(t *testing.T) {
	t.Parallel()
	prompt := contracts.ProposeWithEditsOutput{
		Kind: "proposal",
		Fields: []contracts.Field{
			{Key: "recipient", Type: "string", Current: "old", Proposed: "new"},
			{Key: "subject", Type: "string", Current: "Hello", Proposed: "Hi"},
			{Key: "body", Type: "text", Current: "Body v1", Proposed: "Body v2"},
		},
	}
	replyData, _ := json.Marshal(contracts.ApprovalReply{
		Approved: true,
		Edits: map[string]any{
			"subject": "Hi (edited)",
			// body is left unedited — should default to the proposed value.
		},
	})
	out := buildProposeWithEditsTerminal(prompt, tasks.InputResponse{Data: replyData})
	if out.State != "approved" {
		t.Fatalf("State = %q, want approved", out.State)
	}
	if got := out.Edits["recipient"]; got != "new" {
		t.Errorf("recipient final = %v, want \"new\"", got)
	}
	if got := out.Edits["subject"]; got != "Hi (edited)" {
		t.Errorf("subject final = %v, want \"Hi (edited)\"", got)
	}
	if got := out.Edits["body"]; got != "Body v2" {
		t.Errorf("body final = %v, want \"Body v2\"", got)
	}
}

// TestBuildProposeWithEditsTerminal_Rejected sets Approved=false and a
// rejection reason; Edits should not be populated.
func TestBuildProposeWithEditsTerminal_Rejected(t *testing.T) {
	t.Parallel()
	prompt := contracts.ProposeWithEditsOutput{Fields: []contracts.Field{{Key: "x"}}}
	replyData, _ := json.Marshal(contracts.ApprovalReply{
		Approved: false,
		Reason:   "Body is off-message.",
	})
	out := buildProposeWithEditsTerminal(prompt, tasks.InputResponse{Data: replyData})
	if out.State != "rejected" {
		t.Errorf("State = %q, want rejected", out.State)
	}
	if len(out.Edits) != 0 {
		t.Errorf("Edits = %v, want empty on rejection", out.Edits)
	}
	if out.Reason != "Body is off-message." {
		t.Errorf("Reason = %q", out.Reason)
	}
}

// TestDecodeApprovalReply_EmptyMeansApprove covers the lenient empty-
// body path: the App may post `{}` for a bare-bones approve.
func TestDecodeApprovalReply_EmptyMeansApprove(t *testing.T) {
	t.Parallel()
	reply, err := decodeApprovalReply(nil)
	if err != nil {
		t.Fatalf("decodeApprovalReply: %v", err)
	}
	if !reply.Approved {
		t.Error("empty reply should default to Approved=true")
	}
}

// TestMarshalCallToolResult shapes the wire envelope correctly.
func TestMarshalCallToolResult(t *testing.T) {
	t.Parallel()
	structured := contracts.RequestApprovalOutput{
		Kind:      "approval",
		State:     "approved",
		Title:     "x",
		DecidedAt: time.Date(2026, 5, 23, 18, 0, 0, 0, time.UTC),
	}
	raw, err := marshalCallToolResult(structured, "approval: x — approved")
	if err != nil {
		t.Fatalf("marshalCallToolResult: %v", err)
	}
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent contracts.RequestApprovalOutput `json:"structuredContent"`
		IsError           bool                            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.StructuredContent.Kind != "approval" {
		t.Errorf("Kind = %q, want approval", decoded.StructuredContent.Kind)
	}
	if decoded.StructuredContent.State != "approved" {
		t.Errorf("State = %q, want approved", decoded.StructuredContent.State)
	}
	if decoded.IsError {
		t.Error("IsError = true, want false")
	}
	if len(decoded.Content) != 1 || decoded.Content[0].Type != "text" {
		t.Errorf("Content shape unexpected: %+v", decoded.Content)
	}
}

// TestFinaliseEdits_RespectsProposedDefaults covers the helper.
func TestFinaliseEdits_RespectsProposedDefaults(t *testing.T) {
	t.Parallel()
	fields := []contracts.Field{
		{Key: "a", Proposed: 1},
		{Key: "b", Proposed: "two"},
	}
	final := finaliseEdits(fields, map[string]any{"a": 9})
	if final["a"] != 9 {
		t.Errorf("a = %v, want 9", final["a"])
	}
	if final["b"] != "two" {
		t.Errorf("b = %v, want \"two\"", final["b"])
	}
}
