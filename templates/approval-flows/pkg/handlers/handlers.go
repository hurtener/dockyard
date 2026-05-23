// Package handlers implements the two approval-flows tool handlers.
//
// Each handler is a thin pure function over a typed contract: it builds a
// prompt from the model's input, creates a task that pauses at
// input_required carrying that prompt, and returns the task identity to the
// host. When the user supplies a reply through the bridge's
// `sendElicitationResponse` notification (Phase 25 / D-134), the host
// forwards it to the attached server's `tasks/result` endpoint; the
// handler decodes it against the `ApprovalReply` contract and returns the
// terminal CallToolResult (the structuredContent the App's terminal
// renderer consumes).
//
// Swap to a real approval store by replacing the body of each handler
// with a call into your own queue (Slack, a webhook, a database table)
// and returning the user's decision in the same shape — the contract
// is the integration surface, not the handler internals.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"

	"github.com/hurtener/dockyard/templates/approval-flows/pkg/contracts"
)

// TaskCreatorOutput is the synchronous tools/call body the host receives
// for a task-augmented tool: it carries the prompt the App renders in
// the `awaiting` state, plus the created task's id and status. When the
// host advertises Tasks, the runtime substitutes a CreateTaskResult for
// this body on the wire (RFC §8.3); a host that does not advertise
// Tasks still gets the prompt verbatim and degrades to a "this host
// can't approve interactively" UX.
//
// The Phase 25 inspector renders the `awaiting` payload through its
// Fixtures switcher; once the user decides the bridge posts the reply
// and the task's terminal CallToolResult re-renders the App with the
// `approved` / `rejected` state.

// CreateRequestApproval is the synchronous half of the request_approval
// tool: it builds a prompt, creates a task that pauses at input_required
// carrying that prompt, and returns the prompt body verbatim so the
// host (or the inspector's Fixtures switcher) can immediately render
// the `awaiting` state. The task's terminal CallToolResult — what
// flows through `tasks/result` — carries the user's decision.
type CreateRequestApproval struct{ Engine *tasks.Engine }

// Handler is the contract-first tool handler for create_request_approval.
// It is wired into the runtime/tool builder.
func (c CreateRequestApproval) Handler(ctx context.Context, in contracts.RequestApprovalInput) (tool.Result[contracts.RequestApprovalOutput], error) {
	prompt := contracts.RequestApprovalOutput{
		Kind:        "approval",
		Title:       in.Title,
		Description: in.Description,
		Category:    in.Category,
		Metadata:    in.Metadata,
		State:       "awaiting",
	}
	if in.Title == "" {
		// An empty prompt drives the App's empty state — no task is
		// created.
		prompt.State = "empty"
		prompt.Message = "The approval request was empty."
		return tool.Result[contracts.RequestApprovalOutput]{
			Text:       "request_approval: empty prompt — nothing to approve.",
			Structured: prompt,
		}, nil
	}

	// Capability degradation (RFC §7.5, CLAUDE.md §6): a host that did
	// not negotiate Tasks gets a synchronous "requires interactive
	// host" response — the App degrades to a static prompt that
	// explains the user must run with a Tasks-capable host.
	if c.Engine == nil {
		prompt.Message = "This host did not negotiate the MCP Tasks extension — interactive approval is unavailable. The model received the prompt verbatim."
		return tool.Result[contracts.RequestApprovalOutput]{
			Text:       fmt.Sprintf("request_approval: %s", in.Title),
			Structured: prompt,
		}, nil
	}

	// Create the task. The handle runs on a background goroutine and
	// blocks at RequireInput until the user replies through the bridge.
	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return tool.Result[contracts.RequestApprovalOutput]{}, fmt.Errorf(
			"marshal approval prompt: %w", err)
	}
	// `runPrompt` is the snapshot the background goroutine sees; we copy
	// it to avoid a data race with the post-Create mutation that stamps
	// the task id into the synchronous return value.
	runPrompt := prompt
	raw, err := c.Engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "request_approval",
		Handle: func(rc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			return runRequestApproval(rc, h, runPrompt, promptJSON)
		},
	})
	if err != nil {
		return tool.Result[contracts.RequestApprovalOutput]{}, fmt.Errorf(
			"create approval task: %w", err)
	}
	// Extract the created task's id so the App can post the
	// elicitation-response against the right task. The runtime's
	// CreateTaskResult is `{task: {taskId, status, ...}}` —
	// shallow-decode it. The goroutine has its own snapshot
	// (`runPrompt`) so writing to `prompt.TaskID` here is race-free.
	prompt.TaskID = decodeCreatedTaskID(raw)
	// Return the prompt verbatim. The runtime substitutes a
	// CreateTaskResult on the wire for a Tasks-capable host; the App
	// receives the prompt as the first `tool-result` push and renders
	// the `awaiting` state.
	return tool.Result[contracts.RequestApprovalOutput]{
		Text:       fmt.Sprintf("request_approval: %s — awaiting decision", in.Title),
		Structured: prompt,
	}, nil
}

// runRequestApproval is the task's run body. It posts the prompt as the
// task's status message (so an obs/v1 task.progress event carries it),
// pauses at input_required, decodes the user's reply against the
// ApprovalReply contract, and returns the terminal CallToolResult JSON
// (the structuredContent the App's `approved` / `rejected` renderer
// consumes).
func runRequestApproval(
	ctx context.Context, h tasks.TaskHandle,
	prompt contracts.RequestApprovalOutput, promptJSON []byte,
) (json.RawMessage, error) {
	// Status message — surfaces in the inspector's Tasks panel and any
	// obs/v1 consumer.
	if err := h.Status(ctx, "awaiting approval: "+prompt.Title); err != nil {
		// A status write that fails is not fatal — the task continues.
		_ = err
	}
	reply, err := h.RequireInput(ctx, tasks.InputPrompt{
		Message: "approval-flows.request_approval: " + prompt.Title,
		Schema:  promptJSON,
	})
	if err != nil {
		return nil, err
	}
	if h.Cancelled() {
		return nil, errors.New("approval cancelled before a decision was reached")
	}
	terminal := buildRequestApprovalTerminal(prompt, reply)
	return marshalCallToolResult(terminal,
		fmt.Sprintf("request_approval: %s — %s",
			prompt.Title, requestApprovalSummary(reply)))
}

// buildRequestApprovalTerminal builds the terminal structuredContent
// from the user's reply (or the declined signal).
func buildRequestApprovalTerminal(
	prompt contracts.RequestApprovalOutput, reply tasks.InputResponse,
) contracts.RequestApprovalOutput {
	out := contracts.RequestApprovalOutput{
		Kind:        prompt.Kind,
		Title:       prompt.Title,
		Description: prompt.Description,
		Category:    prompt.Category,
		Metadata:    prompt.Metadata,
		DecidedAt:   time.Now().UTC(),
	}
	if reply.Declined {
		out.State = "rejected"
		approved := false
		out.Approved = &approved
		out.Reason = "User declined to provide a decision."
		return out
	}
	parsed, err := decodeApprovalReply(reply.Data)
	if err != nil {
		out.State = "error"
		out.Message = "Could not decode the user's reply: " + err.Error()
		return out
	}
	out.Approved = &parsed.Approved
	out.Reason = parsed.Reason
	if parsed.Approved {
		out.State = "approved"
	} else {
		out.State = "rejected"
	}
	return out
}

// requestApprovalSummary builds the human-readable one-liner for the
// terminal CallToolResult's text content (the model's view).
func requestApprovalSummary(reply tasks.InputResponse) string {
	if reply.Declined {
		return "declined"
	}
	parsed, err := decodeApprovalReply(reply.Data)
	if err != nil {
		return "error decoding reply"
	}
	if parsed.Approved {
		return "approved"
	}
	return "rejected"
}

// ---------------------------------------------------------------------------
// propose_with_edits — propose a structured change for review.
// ---------------------------------------------------------------------------

// CreateProposeWithEdits is the synchronous half of the
// propose_with_edits tool. It mirrors CreateRequestApproval's shape.
type CreateProposeWithEdits struct{ Engine *tasks.Engine }

// Handler is the contract-first tool handler for create_propose_with_edits.
func (c CreateProposeWithEdits) Handler(ctx context.Context, in contracts.ProposeWithEditsInput) (tool.Result[contracts.ProposeWithEditsOutput], error) {
	prompt := contracts.ProposeWithEditsOutput{
		Kind:        "proposal",
		Title:       in.Title,
		Description: in.Description,
		Fields:      in.Fields,
		Category:    in.Category,
		State:       "awaiting",
	}
	if len(in.Fields) == 0 {
		prompt.State = "empty"
		prompt.Message = "The proposal carries no fields — nothing to review."
		return tool.Result[contracts.ProposeWithEditsOutput]{
			Text:       "propose_with_edits: no fields — nothing to review.",
			Structured: prompt,
		}, nil
	}
	if c.Engine == nil {
		prompt.Message = "This host did not negotiate the MCP Tasks extension — interactive edits are unavailable."
		return tool.Result[contracts.ProposeWithEditsOutput]{
			Text:       fmt.Sprintf("propose_with_edits: %s", in.Title),
			Structured: prompt,
		}, nil
	}
	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return tool.Result[contracts.ProposeWithEditsOutput]{}, fmt.Errorf(
			"marshal proposal prompt: %w", err)
	}
	runPrompt := prompt
	raw, err := c.Engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "propose_with_edits",
		Handle: func(rc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			return runProposeWithEdits(rc, h, runPrompt, promptJSON)
		},
	})
	if err != nil {
		return tool.Result[contracts.ProposeWithEditsOutput]{}, fmt.Errorf(
			"create proposal task: %w", err)
	}
	prompt.TaskID = decodeCreatedTaskID(raw)
	return tool.Result[contracts.ProposeWithEditsOutput]{
		Text:       fmt.Sprintf("propose_with_edits: %s — awaiting decision", in.Title),
		Structured: prompt,
	}, nil
}

// decodeCreatedTaskID shallow-decodes a CreateTaskResult to pull out the
// task id. The CreateTaskResult JSON is `{task: {taskId, ...}}`. Returns
// "" when the shape is unexpected — the App still renders, the
// elicitation-response just lacks a useful task id (and the inspector
// surfaces a "task not found" error which the developer can spot).
func decodeCreatedTaskID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var envelope struct {
		Task struct {
			TaskID string `json:"taskId"`
		} `json:"task"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	return envelope.Task.TaskID
}

// runProposeWithEdits is the task body for propose_with_edits.
func runProposeWithEdits(
	ctx context.Context, h tasks.TaskHandle,
	prompt contracts.ProposeWithEditsOutput, promptJSON []byte,
) (json.RawMessage, error) {
	if err := h.Status(ctx, "awaiting proposal review: "+prompt.Title); err != nil {
		_ = err
	}
	reply, err := h.RequireInput(ctx, tasks.InputPrompt{
		Message: "approval-flows.propose_with_edits: " + prompt.Title,
		Schema:  promptJSON,
	})
	if err != nil {
		return nil, err
	}
	if h.Cancelled() {
		return nil, errors.New("review cancelled before a decision was reached")
	}
	terminal := buildProposeWithEditsTerminal(prompt, reply)
	return marshalCallToolResult(terminal,
		fmt.Sprintf("propose_with_edits: %s — %s",
			prompt.Title, proposeWithEditsSummary(reply)))
}

// buildProposeWithEditsTerminal builds the terminal structuredContent.
func buildProposeWithEditsTerminal(
	prompt contracts.ProposeWithEditsOutput, reply tasks.InputResponse,
) contracts.ProposeWithEditsOutput {
	out := contracts.ProposeWithEditsOutput{
		Kind:        prompt.Kind,
		Title:       prompt.Title,
		Description: prompt.Description,
		Fields:      prompt.Fields,
		Category:    prompt.Category,
		DecidedAt:   time.Now().UTC(),
	}
	if reply.Declined {
		out.State = "rejected"
		approved := false
		out.Approved = &approved
		out.Reason = "User declined to review the proposal."
		return out
	}
	parsed, err := decodeApprovalReply(reply.Data)
	if err != nil {
		out.State = "error"
		out.Message = "Could not decode the user's reply: " + err.Error()
		return out
	}
	out.Approved = &parsed.Approved
	out.Reason = parsed.Reason
	if parsed.Approved {
		out.State = "approved"
		// On approval, the edits map is the final values; default any
		// unedited field to its proposed value so a handler downstream
		// gets a complete map.
		out.Edits = finaliseEdits(prompt.Fields, parsed.Edits)
	} else {
		out.State = "rejected"
	}
	return out
}

func proposeWithEditsSummary(reply tasks.InputResponse) string {
	if reply.Declined {
		return "declined"
	}
	parsed, err := decodeApprovalReply(reply.Data)
	if err != nil {
		return "error decoding reply"
	}
	if parsed.Approved {
		return "approved with edits"
	}
	return "rejected"
}

// finaliseEdits merges the App-supplied edits over the proposed field
// values, ensuring every field key is present in the final map even
// when the user did not edit it (the proposed value is the default).
func finaliseEdits(fields []contracts.Field, edits map[string]any) map[string]any {
	final := make(map[string]any, len(fields))
	for _, f := range fields {
		final[f.Key] = f.Proposed
	}
	for k, v := range edits {
		final[k] = v
	}
	return final
}

// ---------------------------------------------------------------------------
// Shared decoding + marshalling helpers.
// ---------------------------------------------------------------------------

// decodeApprovalReply decodes the App's elicitation-response payload
// against the ApprovalReply contract. An empty payload is treated as
// an approve-with-no-reason (the App posting `{}` is the minimal
// happy path).
func decodeApprovalReply(raw []byte) (contracts.ApprovalReply, error) {
	if len(raw) == 0 {
		return contracts.ApprovalReply{Approved: true}, nil
	}
	var reply contracts.ApprovalReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return contracts.ApprovalReply{}, err
	}
	return reply, nil
}

// marshalCallToolResult builds the terminal CallToolResult JSON the
// task's tasks/result returns. It bridges the typed tool.Result shape
// (which is a runtime/tool concept) into the wire shape MCP clients
// expect (the SDK's CallToolResult: content[] + structuredContent +
// isError).
func marshalCallToolResult(structured any, text string) (json.RawMessage, error) {
	envelope := map[string]any{
		"content":           []map[string]any{{"type": "text", "text": text}},
		"structuredContent": structured,
		"isError":           false,
	}
	return json.Marshal(envelope)
}
