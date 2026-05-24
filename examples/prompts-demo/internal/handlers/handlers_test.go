package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hurtener/dockyard/examples/prompts-demo/internal/contracts"
	"github.com/hurtener/dockyard/examples/prompts-demo/internal/handlers"
	"github.com/hurtener/dockyard/runtime/server"
)

// TestSummarizeText_RequiresText confirms the input validation.
func TestSummarizeText_RequiresText(t *testing.T) {
	t.Parallel()
	if _, err := handlers.SummarizeText(context.Background(), contracts.SummarizeTextInput{}); err == nil {
		t.Fatalf("expected an error for empty text")
	}
}

// TestSummarizeText_DefaultsAndClamp confirms MaxSentences defaults to
// 2 and is clamped to a sensible range.
func TestSummarizeText_DefaultsAndClamp(t *testing.T) {
	t.Parallel()
	in := contracts.SummarizeTextInput{Text: "One. Two. Three. Four. Five. Six. Seven."}
	got, err := handlers.SummarizeText(context.Background(), in)
	if err != nil {
		t.Fatalf("SummarizeText: %v", err)
	}
	if got.Structured.Sentences != 2 {
		t.Fatalf("default Sentences = %d, want 2", got.Structured.Sentences)
	}

	// Clamped at 5.
	in.MaxSentences = 99
	got, err = handlers.SummarizeText(context.Background(), in)
	if err != nil {
		t.Fatalf("SummarizeText: %v", err)
	}
	if got.Structured.Sentences > 5 {
		t.Fatalf("clamped Sentences = %d, want <= 5", got.Structured.Sentences)
	}
}

// TestSummarizeForReview_ShapesAudience confirms the prompt fills in
// the audience default and threads the passage into the user message.
func TestSummarizeForReview_ShapesAudience(t *testing.T) {
	t.Parallel()
	got, err := handlers.SummarizeForReview(context.Background(), server.PromptRequest{
		Arguments: map[string]string{"passage": "The quick brown fox."},
	})
	if err != nil {
		t.Fatalf("SummarizeForReview: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2", len(got.Messages))
	}
	if !strings.Contains(got.Messages[0].Text, "engineering peer") {
		t.Fatalf("expected default audience to be 'engineering peer', got %q", got.Messages[0].Text)
	}
	if !strings.Contains(got.Messages[1].Text, "quick brown fox") {
		t.Fatalf("passage missing from user message: %q", got.Messages[1].Text)
	}
}

// TestCodeReview_ShapesLanguageAndRubric confirms language + rubric
// defaults plus that the diff is fenced into a code block.
func TestCodeReview_ShapesLanguageAndRubric(t *testing.T) {
	t.Parallel()
	got, err := handlers.CodeReview(context.Background(), server.PromptRequest{
		Arguments: map[string]string{"diff": "func main() {}"},
	})
	if err != nil {
		t.Fatalf("CodeReview: %v", err)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("Messages = %d, want 4", len(got.Messages))
	}
	if !strings.Contains(got.Messages[2].Text, "```Go") {
		t.Fatalf("expected diff to be fenced with Go: %q", got.Messages[2].Text)
	}
}

// TestExplainError_NoErrorGraceful confirms a missing error argument
// renders a helpful guidance message rather than panicking.
func TestExplainError_NoErrorGraceful(t *testing.T) {
	t.Parallel()
	got, err := handlers.ExplainError(context.Background(), server.PromptRequest{})
	if err != nil {
		t.Fatalf("ExplainError: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(got.Messages))
	}
	if !strings.Contains(got.Messages[0].Text, "error") {
		t.Fatalf("graceful message missing 'error': %q", got.Messages[0].Text)
	}
}
