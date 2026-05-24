// Package contracts holds the tool contracts for the combined-patterns
// example (Phase 28).
//
// Two contract pairs: RolloutHealth* (the analytics-widget half;
// synchronous; renders a metric card) and ProposeRolloutAction* (the
// approval-flow half; task-augmented; renders an approval card). Both
// share the same App — the agent surfaces the metric, then proposes an
// action on it, in the same chat surface.
package contracts

import "time"

// --- rollout_health (analytics-widget half) ---------------------------------

// RolloutHealthInput is the input contract for rollout_health.
type RolloutHealthInput struct {
	// Flag is the feature-flag key whose rollout is being inspected. An
	// empty Flag drives the App's empty state.
	Flag string `json:"flag"`
	// Window is the look-back window in minutes for the error-rate
	// trend. Defaults to 60.
	WindowMinutes int `json:"window_minutes,omitempty"`
	// Theme is an optional per-call theme override.
	Theme string `json:"theme,omitempty"`
}

// HealthTone classifies a rollout's overall health.
type HealthTone string

const (
	// HealthOK — the rollout is healthy; no action needed.
	HealthOK HealthTone = "ok"
	// HealthWarn — degraded; consider pausing the next ramp.
	HealthWarn HealthTone = "warn"
	// HealthCritical — actively failing; consider a rollback.
	HealthCritical HealthTone = "critical"
)

// RolloutHealthOutput is the structured payload the App's metric-card
// renderer consumes — the same shape as the analytics-widgets template's
// metric card, with a couple of rollout-specific fields.
type RolloutHealthOutput struct {
	// Kind is always "metric_card" so the App's dispatcher routes the
	// payload to its existing metric-card renderer. Compositional
	// purpose: the App's dispatch table is the seam shared between the
	// widget half and the approval half.
	Kind string `json:"kind"`
	// Flag is the inspected flag key, passed through.
	Flag string `json:"flag"`
	// Label is the card title (e.g. "checkout-v2 rollout health").
	Label string `json:"label"`
	// Value is the headline number (an error rate as a percentage).
	Value float64 `json:"value"`
	// Unit is the headline unit ("%" for the demo).
	Unit string `json:"unit"`
	// Tone is the health classification — drives the card's tone chip.
	Tone HealthTone `json:"tone"`
	// Trend is the time-series sparkline (per-minute error rate over the
	// look-back window). Empty when no data is available.
	Trend []float64 `json:"trend,omitempty"`
	// SuggestedAction is the next-step hint the model (or a follow-up
	// propose_rollout_action call) might act on. Empty when no action
	// is suggested.
	SuggestedAction string `json:"suggested_action,omitempty"`
	// Theme is the resolved theme (never "auto" — the handler resolves
	// "auto" to the host default).
	Theme string `json:"theme"`
	// State is the UI state ("ready" | "empty" | "error" | "permission"
	// | "loading"). Mirrors the four-state PageState (CLAUDE.md §20).
	State string `json:"state"`
	// Message is the human-readable line shown in non-ready states.
	Message string `json:"message,omitempty"`
}

// --- propose_rollout_action (approval-flow half) ----------------------------

// RolloutAction is one of the V1 rollout actions the approval prompt
// asks the user to confirm. The agent picks the action based on the
// rollout's health; the user has the final say.
type RolloutAction string

const (
	// ActionAdvance — advance the rollout to the next ramp step.
	ActionAdvance RolloutAction = "advance"
	// ActionPause — pause the rollout at the current ramp.
	ActionPause RolloutAction = "pause"
	// ActionRollback — roll the flag back to the previous ramp.
	ActionRollback RolloutAction = "rollback"
)

// ProposeRolloutActionInput is the input contract for
// propose_rollout_action.
type ProposeRolloutActionInput struct {
	// Flag is the feature-flag key the action applies to.
	Flag string `json:"flag"`
	// Action is the proposed action (advance / pause / rollback).
	Action RolloutAction `json:"action"`
	// Rationale is the model's short justification, rendered in the
	// approval card so the user sees the reasoning.
	Rationale string `json:"rationale"`
	// CurrentRamp is the current rollout percentage (0..100). Optional
	// context for the approval card.
	CurrentRamp int `json:"current_ramp,omitempty"`
	// TargetRamp is the proposed next rollout percentage (0..100).
	// Optional; only meaningful for an "advance" action.
	TargetRamp int `json:"target_ramp,omitempty"`
}

// ProposeRolloutActionOutput is the approval-card structured payload.
// It mirrors the approval-flows template's RequestApprovalOutput shape
// (Kind="approval", State="awaiting" | "approved" | "rejected" | …)
// so the App's existing approval renderer can render it verbatim —
// that compositional re-use is the whole point of this example.
type ProposeRolloutActionOutput struct {
	// Kind is always "approval" so the App's dispatcher routes the
	// payload to the approval renderer.
	Kind string `json:"kind"`
	// TaskID is the id of the underlying task, when one was created.
	// Empty in the capability-degraded path.
	TaskID string `json:"task_id,omitempty"`
	// Flag is the action's flag key, passed through.
	Flag string `json:"flag"`
	// Action is the proposed action, passed through.
	Action RolloutAction `json:"action"`
	// Rationale is the model's justification, passed through.
	Rationale string `json:"rationale"`
	// CurrentRamp / TargetRamp are passed through.
	CurrentRamp int `json:"current_ramp,omitempty"`
	TargetRamp  int `json:"target_ramp,omitempty"`
	// Title is the headline the App's approval renderer reads
	// (e.g. "Advance checkout-v2 to 50%").
	Title string `json:"title"`
	// Description is the long-form prompt body.
	Description string `json:"description"`
	// Category is the StatusChip tag ("rollout").
	Category string `json:"category,omitempty"`
	// State is the UI lifecycle state ("awaiting" | "approved" |
	// "rejected" | "empty" | "error" | "permission"). The lifecycle
	// matches the approval-flows template's RequestApprovalOutput.
	State string `json:"state"`
	// Approved is the user's verdict when terminal.
	Approved *bool `json:"approved,omitempty"`
	// Reason is the user's optional free-text reason.
	Reason string `json:"reason,omitempty"`
	// DecidedAt is the timestamp of the decision.
	DecidedAt time.Time `json:"decided_at,omitempty"`
	// Message is the human-readable line for non-ready states.
	Message string `json:"message,omitempty"`
}
