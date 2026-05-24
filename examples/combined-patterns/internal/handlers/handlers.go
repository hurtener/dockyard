// Package handlers implements the combined-patterns example's two tool
// handlers. The point of this example is showing that the analytics-
// widget half and the approval-flow half compose on one App — the
// rollout_health widget surfaces an insight, then propose_rollout_action
// proposes a follow-up the user approves.
//
// Replace the body of Snapshot.For and the action-store side of
// ApprovalProposer with calls into your real telemetry + your real
// flag-management API; the typed contracts are the integration surface.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hurtener/dockyard/examples/combined-patterns/internal/contracts"
	"github.com/hurtener/dockyard/runtime/tasks"
	"github.com/hurtener/dockyard/runtime/tool"
)

// --- the rollout_health (analytics-widget) handler --------------------------

// Snapshot is a synthetic rollout health source. A real deployment would
// swap this for a telemetry client; the typed contract is unchanged.
type Snapshot struct{}

// NewSnapshot returns a Snapshot whose For() returns synthetic but
// realistic data — different flags produce different health profiles so
// the demo shows the OK / warn / critical tones without configuration.
func NewSnapshot() *Snapshot { return &Snapshot{} }

// Handler is the rollout_health tool handler. It produces a metric card
// the App's analytics-widget renderer consumes.
func (s *Snapshot) Handler(_ context.Context, in contracts.RolloutHealthInput) (tool.Result[contracts.RolloutHealthOutput], error) {
	if strings.TrimSpace(in.Flag) == "" {
		return tool.Result[contracts.RolloutHealthOutput]{
			Text: "rollout_health: no flag — nothing to inspect.",
			Structured: contracts.RolloutHealthOutput{
				Kind:    "metric_card",
				State:   "empty",
				Message: "Provide a feature-flag key to inspect.",
				Theme:   resolveTheme(in.Theme),
			},
		}, nil
	}
	window := in.WindowMinutes
	if window <= 0 {
		window = 60
	}
	value, tone, trend, suggestion := s.For(in.Flag, window)
	out := contracts.RolloutHealthOutput{
		Kind:            "metric_card",
		Flag:            in.Flag,
		Label:           in.Flag + " rollout health",
		Value:           value,
		Unit:            "%",
		Tone:            tone,
		Trend:           trend,
		SuggestedAction: suggestion,
		Theme:           resolveTheme(in.Theme),
		State:           "ready",
	}
	text := fmt.Sprintf("rollout_health: %s — %.2f%% error rate (%s)", in.Flag, value, tone)
	return tool.Result[contracts.RolloutHealthOutput]{Text: text, Structured: out}, nil
}

// For computes the synthetic health snapshot for a flag and look-back
// window. The deterministic seed (the flag name's rune sum) means a
// given flag has a stable health profile across calls — predictable
// demos, deterministic tests.
func (s *Snapshot) For(flag string, windowMinutes int) (value float64, tone contracts.HealthTone, trend []float64, suggestion string) {
	seed := 0
	for _, r := range flag {
		seed += int(r)
	}
	// Three deterministic health profiles, picked by the seed's residue.
	switch seed % 3 {
	case 0:
		value = 0.4 + (float64(seed%5) * 0.05)
		tone = contracts.HealthOK
		suggestion = "advance"
	case 1:
		value = 2.5 + (float64(seed%5) * 0.2)
		tone = contracts.HealthWarn
		suggestion = "pause"
	default:
		value = 7.0 + (float64(seed%5) * 0.5)
		tone = contracts.HealthCritical
		suggestion = "rollback"
	}
	trend = make([]float64, windowMinutes)
	for i := range trend {
		// A gentle sinusoid around the value so the sparkline is
		// recognisable but realistic. Amplitude scales with severity.
		amp := math.Max(0.1, value*0.15)
		trend[i] = value + amp*math.Sin(float64(i+seed)*0.4)
		if trend[i] < 0 {
			trend[i] = 0
		}
	}
	return value, tone, trend, suggestion
}

func resolveTheme(t string) string {
	switch t {
	case "light", "dark":
		return t
	default:
		return "auto"
	}
}

// --- the propose_rollout_action (approval-flow) handler ---------------------

// ApprovalProposer is the propose_rollout_action handler bound to the
// process-wide Tasks engine. It mirrors the approval-flows template's
// CreateRequestApproval pattern: the synchronous side builds the prompt
// + creates the task; the background goroutine pauses at RequireInput
// until the user replies through the bridge.
//
// Swap the body of `runApproval` for the real action-store integration
// (a flag-management API call, an audit-log entry) when wiring this to
// a production system.
type ApprovalProposer struct{ Engine *tasks.Engine }

// NewApprovalProposer constructs a proposer over engine. A nil engine is
// allowed; the handler degrades to a synchronous "this host did not
// negotiate Tasks" response (RFC §7.5, capability degradation).
func NewApprovalProposer(engine *tasks.Engine) *ApprovalProposer {
	return &ApprovalProposer{Engine: engine}
}

// Handler is the propose_rollout_action tool handler.
func (a *ApprovalProposer) Handler(ctx context.Context, in contracts.ProposeRolloutActionInput) (tool.Result[contracts.ProposeRolloutActionOutput], error) {
	prompt := buildPrompt(in)
	if prompt.State == "empty" {
		return tool.Result[contracts.ProposeRolloutActionOutput]{
			Text:       "propose_rollout_action: empty input — nothing to propose.",
			Structured: prompt,
		}, nil
	}
	if a == nil || a.Engine == nil {
		// Capability-degraded path: render the prompt synchronously so a
		// host without Tasks still sees the proposal verbatim.
		prompt.Message = "This host did not negotiate the MCP Tasks extension — interactive approval is unavailable."
		return tool.Result[contracts.ProposeRolloutActionOutput]{
			Text:       "propose_rollout_action: " + prompt.Title,
			Structured: prompt,
		}, nil
	}

	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return tool.Result[contracts.ProposeRolloutActionOutput]{}, fmt.Errorf("marshal proposal prompt: %w", err)
	}
	runPrompt := prompt
	raw, err := a.Engine.CreateForToolCall(ctx, tasks.CreateToolCallParams{
		ToolName: "propose_rollout_action",
		Handle: func(rc context.Context, h tasks.TaskHandle) (json.RawMessage, error) {
			return runApproval(rc, h, runPrompt, promptJSON)
		},
	})
	if err != nil {
		return tool.Result[contracts.ProposeRolloutActionOutput]{}, fmt.Errorf("create proposal task: %w", err)
	}
	prompt.TaskID = decodeCreatedTaskID(raw)
	return tool.Result[contracts.ProposeRolloutActionOutput]{
		Text:       "propose_rollout_action: " + prompt.Title + " — awaiting decision",
		Structured: prompt,
	}, nil
}

// buildPrompt assembles the awaiting-state structured payload the App's
// approval renderer reads. Empty input drives the empty state.
func buildPrompt(in contracts.ProposeRolloutActionInput) contracts.ProposeRolloutActionOutput {
	if strings.TrimSpace(in.Flag) == "" || in.Action == "" {
		return contracts.ProposeRolloutActionOutput{
			Kind:    "approval",
			State:   "empty",
			Message: "The proposal carries no flag or action — nothing to approve.",
		}
	}
	title := actionTitle(in.Action, in.Flag, in.TargetRamp)
	return contracts.ProposeRolloutActionOutput{
		Kind:        "approval",
		Flag:        in.Flag,
		Action:      in.Action,
		Rationale:   in.Rationale,
		CurrentRamp: in.CurrentRamp,
		TargetRamp:  in.TargetRamp,
		Title:       title,
		Description: in.Rationale,
		Category:    "rollout",
		State:       "awaiting",
	}
}

// actionTitle composes the headline the approval card renders.
func actionTitle(action contracts.RolloutAction, flag string, target int) string {
	switch action {
	case contracts.ActionAdvance:
		if target > 0 {
			return fmt.Sprintf("Advance %s to %d%%", flag, target)
		}
		return fmt.Sprintf("Advance %s", flag)
	case contracts.ActionPause:
		return fmt.Sprintf("Pause %s", flag)
	case contracts.ActionRollback:
		return fmt.Sprintf("Rollback %s", flag)
	default:
		return fmt.Sprintf("%s %s", action, flag)
	}
}

// runApproval is the task body for propose_rollout_action. It pauses at
// RequireInput until the bridge delivers the user's reply via
// tasks/result, then returns the terminal CallToolResult JSON.
func runApproval(ctx context.Context, h tasks.TaskHandle, prompt contracts.ProposeRolloutActionOutput, promptJSON []byte) (json.RawMessage, error) {
	if err := h.Status(ctx, "awaiting approval: "+prompt.Title); err != nil {
		_ = err
	}
	reply, err := h.RequireInput(ctx, tasks.InputPrompt{
		Message: "combined-patterns.propose_rollout_action: " + prompt.Title,
		Schema:  promptJSON,
	})
	if err != nil {
		return nil, err
	}
	if h.Cancelled() {
		return nil, errors.New("approval cancelled before a decision was reached")
	}
	terminal := buildTerminal(prompt, reply)
	return marshalCallToolResult(terminal, summarise(prompt.Title, reply))
}

// buildTerminal builds the terminal structured payload from the user's
// elicitation reply.
func buildTerminal(prompt contracts.ProposeRolloutActionOutput, reply tasks.InputResponse) contracts.ProposeRolloutActionOutput {
	out := prompt
	out.DecidedAt = time.Now().UTC()
	if reply.Declined {
		out.State = "rejected"
		approved := false
		out.Approved = &approved
		out.Reason = "User declined to decide."
		return out
	}
	parsed, err := decodeReply(reply.Data)
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

// reply is the App-supplied JSON the bridge posts back via
// sendElicitationResponse. It matches the approval-flows template's
// ApprovalReply shape (Phase 28 example deliberately mirrors the
// template's contract so the same App component can render both).
type reply struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

func decodeReply(raw []byte) (reply, error) {
	if len(raw) == 0 {
		return reply{Approved: true}, nil
	}
	var r reply
	if err := json.Unmarshal(raw, &r); err != nil {
		return reply{}, err
	}
	return r, nil
}

func summarise(title string, r tasks.InputResponse) string {
	if r.Declined {
		return "propose_rollout_action: " + title + " — declined"
	}
	parsed, err := decodeReply(r.Data)
	if err != nil {
		return "propose_rollout_action: " + title + " — error decoding reply"
	}
	if parsed.Approved {
		return "propose_rollout_action: " + title + " — approved"
	}
	return "propose_rollout_action: " + title + " — rejected"
}

func marshalCallToolResult(structured any, text string) (json.RawMessage, error) {
	envelope := map[string]any{
		"content":           []map[string]any{{"type": "text", "text": text}},
		"structuredContent": structured,
		"isError":           false,
	}
	return json.Marshal(envelope)
}

// decodeCreatedTaskID extracts the task id from a CreateTaskResult JSON
// envelope. Returns "" on any decoding failure (the App still renders;
// the elicitation-response just lacks a useful task id).
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
