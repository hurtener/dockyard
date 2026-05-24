// Package contracts holds the tool contract for the prompts-demo
// example's one tool (Phase 28). Prompts themselves carry no Go
// contract — they are registered via runtime/server.AddPrompt with
// string arguments (D-152).
package contracts

// SummarizeTextInput is the input contract for summarize_text.
type SummarizeTextInput struct {
	// Text is the passage to summarise. Required.
	Text string `json:"text"`
	// MaxSentences is the upper bound on the summary length, in
	// sentences. Defaults to 2. Clamped to [1..5].
	MaxSentences int `json:"max_sentences,omitempty"`
}

// SummarizeTextOutput is the output contract for summarize_text.
type SummarizeTextOutput struct {
	// Summary is the rendered summary.
	Summary string `json:"summary"`
	// Sentences is the number of sentences the summary contains.
	Sentences int `json:"sentences"`
}
