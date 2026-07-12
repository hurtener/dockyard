// Package handlers implements the two approval-flows tool handlers.
//
// request_approval demonstrates modern task mid-flight input through
// TaskHandle.RequestInput and tasks/update. propose_with_edits demonstrates
// core MRTR before creating durable task work.
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
// the `awaiting` state. The completed task carries the user's decision.
type CreateRequestApproval struct{ Engine *tasks.Engine }

const approvalInputKey = "approval-decision"

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

	// Create durable work before requesting modern task input.
	// `runPrompt` is the snapshot the background goroutine sees; we copy
	// it to avoid a data race with the post-Create mutation that stamps
	// the task id into the synchronous return value.
	runPrompt := prompt
	created, err := c.Engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
		ToolName:    "request_approval",
		AuthContext: tasks.RequestAuthContext(ctx),
		Handle: func(rc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			return runRequestApproval(rc, h, runPrompt)
		},
	}, true)
	if err != nil {
		return tool.Result[contracts.RequestApprovalOutput]{}, fmt.Errorf(
			"create approval task: %w", err)
	}
	// Return the prompt verbatim. The runtime substitutes a
	// CreateTaskResult on the wire for a Tasks-capable host; the App
	// receives the prompt as the first `tool-result` push and renders
	// the `awaiting` state.
	return tool.Result[contracts.RequestApprovalOutput]{
		Text:        fmt.Sprintf("request_approval: %s — awaiting decision", in.Title),
		Structured:  prompt,
		CreatedTask: &created,
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
	prompt contracts.RequestApprovalOutput,
) (json.RawMessage, error) {
	// Status message — surfaces in the inspector's Tasks panel and any
	// obs/v1 consumer.
	if err := h.Status(ctx, "awaiting approval: "+prompt.Title); err != nil {
		// A status write that fails is not fatal — the task continues.
		_ = err
	}
	requestPayload, err := json.Marshal(map[string]any{
		"method": "elicitation/create",
		"params": map[string]any{
			"message": "approval-flows.request_approval: " + prompt.Title,
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approved": map[string]any{"type": "boolean"},
					"reason":   map[string]any{"type": "string"},
				},
				"required": []string{"approved"},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal approval input request: %w", err)
	}
	err = h.RequestInput(ctx, tasks.InputRequest{
		Key: approvalInputKey, Method: tasks.InputMethodElicitation, Payload: requestPayload,
	})
	if err != nil {
		return nil, err
	}
	response, ok, err := h.ModernInputResponse(ctx, approvalInputKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("approval input response was not persisted")
	}
	if h.Cancelled() {
		return nil, errors.New("approval cancelled before a decision was reached")
	}
	var elicitation struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.Payload, &elicitation); err != nil {
		return nil, fmt.Errorf("decode elicitation response: %w", err)
	}
	reply := tasks.InputResponse{Data: elicitation.Content, Declined: elicitation.Action != "accept"}
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

// CreateProposeWithEdits performs core MRTR before creating durable work.
type CreateProposeWithEdits struct{ Engine *tasks.Engine }

// Handler is the contract-first tool handler for create_propose_with_edits.
func (c CreateProposeWithEdits) Handler(ctx context.Context, call tool.Call[contracts.ProposeWithEditsInput]) (tool.Result[contracts.ProposeWithEditsOutput], error) {
	in := call.Input
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
	response, ok := call.InputResponses[approvalInputKey]
	if !ok {
		schema, err := proposalReplySchema(in.Fields)
		if err != nil {
			return tool.Result[contracts.ProposeWithEditsOutput]{}, err
		}
		return tool.Result[contracts.ProposeWithEditsOutput]{
			Text:       fmt.Sprintf("propose_with_edits: %s — review required", in.Title),
			Structured: prompt,
			InputRequests: map[string]tool.InputRequest{approvalInputKey: tool.ElicitationRequest{
				Mode: "form", Message: "Review and edit: " + in.Title, RequestedSchema: schema,
			}},
		}, nil
	}
	elicited, ok := response.(tool.ElicitationResponse)
	if !ok {
		return tool.Result[contracts.ProposeWithEditsOutput]{}, fmt.Errorf("propose_with_edits: response %q is not elicitation data", approvalInputKey)
	}
	reply := tasks.InputResponse{Declined: elicited.Action != "accept"}
	if !reply.Declined {
		reply.Data, _ = json.Marshal(proposalReplyContent(elicited.Content, in.Fields))
	}
	terminal := buildProposeWithEditsTerminal(prompt, reply)
	if c.Engine == nil {
		return tool.Result[contracts.ProposeWithEditsOutput]{
			Text: fmt.Sprintf("propose_with_edits: %s — %s", in.Title, proposeWithEditsSummary(reply)), Structured: terminal,
		}, nil
	}
	created, err := c.Engine.CreateToolTask(ctx, tasks.CreateToolCallParams{
		ToolName: "propose_with_edits", AuthContext: tasks.RequestAuthContext(ctx),
		Run: func(context.Context) (json.RawMessage, error) {
			return marshalCallToolResult(terminal, fmt.Sprintf("propose_with_edits: %s — %s", in.Title, proposeWithEditsSummary(reply)))
		},
	}, true)
	if err != nil {
		return tool.Result[contracts.ProposeWithEditsOutput]{}, fmt.Errorf("create proposal task: %w", err)
	}
	return tool.Result[contracts.ProposeWithEditsOutput]{CreatedTask: &created}, nil
}

func proposalReplySchema(fields []contracts.Field) (json.RawMessage, error) {
	properties := map[string]any{
		"approved": map[string]any{"type": "boolean"},
		"reason":   map[string]any{"type": "string"},
	}
	for _, field := range fields {
		property := map[string]any{"type": "string"}
		switch field.Type {
		case contracts.FieldTypeNumber:
			property["type"] = "number"
		case contracts.FieldTypeBoolean:
			property["type"] = "boolean"
		case contracts.FieldTypeEnum:
			values := make([]any, len(field.Options))
			for i, option := range field.Options {
				values[i] = option.Value
			}
			property["enum"] = values
		}
		properties[field.Key] = property
	}
	return json.Marshal(map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   []string{"approved"},
	})
}

func proposalReplyContent(content map[string]any, fields []contracts.Field) map[string]any {
	reply := map[string]any{"approved": content["approved"], "reason": content["reason"]}
	edits := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, ok := content[field.Key]; ok {
			edits[field.Key] = value
		}
	}
	reply["edits"] = edits
	return reply
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

// marshalCallToolResult builds the terminal CallToolResult stored on the task.
// It bridges the typed tool.Result shape
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
