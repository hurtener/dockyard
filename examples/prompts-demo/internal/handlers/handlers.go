// Package handlers implements the prompts-demo example's one tool
// (summarize_text) and its three MCP Prompts. The prompts are the
// point of the example — they exercise the Phase 28 prompts API
// (runtime/server.AddPrompt; D-151).
package handlers

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/hurtener/dockyard/examples/prompts-demo/internal/contracts"
	"github.com/hurtener/dockyard/runtime/server"
	"github.com/hurtener/dockyard/runtime/tool"
)

// --- the one tool ----------------------------------------------------------

// SummarizeText is the summarize_text tool handler. Naive sentence
// chunker — keeps the first N sentences. A real summariser is out of
// scope for the demo; the point is showing the tool + prompts shape
// side-by-side.
func SummarizeText(_ context.Context, in contracts.SummarizeTextInput) (tool.Result[contracts.SummarizeTextOutput], error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return tool.Result[contracts.SummarizeTextOutput]{}, errors.New("summarize_text: text is required")
	}
	limit := in.MaxSentences
	if limit <= 0 {
		limit = 2
	}
	if limit > 5 {
		limit = 5
	}
	sentences := splitSentences(text)
	if len(sentences) > limit {
		sentences = sentences[:limit]
	}
	summary := strings.TrimSpace(strings.Join(sentences, " "))
	return tool.Result[contracts.SummarizeTextOutput]{
		Text: summary,
		Structured: contracts.SummarizeTextOutput{
			Summary:   summary,
			Sentences: len(sentences),
		},
	}, nil
}

var sentenceSplit = regexp.MustCompile(`([.!?])\s+`)

// splitSentences is a naive sentence chunker — splits on `.`, `!`,
// `?` followed by a space. It is good enough for the demo and
// deliberately tiny; a real summariser uses something better.
func splitSentences(text string) []string {
	if text == "" {
		return nil
	}
	withSep := sentenceSplit.ReplaceAllString(text, "$1\x00")
	parts := strings.Split(withSep, "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// --- the three prompts ----------------------------------------------------

// SummarizeForReview is the handler for the `summarize_for_review`
// prompt: it produces a two-message "system + user" template that
// seeds a careful, review-oriented summarisation chat.
func SummarizeForReview(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
	passage := req.Arguments["passage"]
	audience := req.Arguments["audience"]
	if audience == "" {
		audience = "an engineering peer"
	}
	return server.PromptResult{
		Description: "Summarise a passage carefully for an engineering peer review.",
		Messages: []server.PromptMessage{
			{
				Role: "system",
				Text: "You are a careful summariser. Render a two-sentence summary fit for " + audience + ", preserve every named entity, and call out one open question worth verifying.",
			},
			{
				Role: "user",
				Text: "Passage to summarise:\n\n" + passage,
			},
		},
	}, nil
}

// CodeReview is the handler for the `code_review` prompt: it produces
// a four-message "system + user(code) + user(rubric) + user(ask)"
// template that asks a model to review a code change against a
// rubric.
func CodeReview(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
	diff := req.Arguments["diff"]
	language := req.Arguments["language"]
	if language == "" {
		language = "Go"
	}
	rubric := req.Arguments["rubric"]
	if rubric == "" {
		rubric = "1) correctness; 2) test coverage; 3) any race risk; 4) one suggested change."
	}
	return server.PromptResult{
		Description: "Review a " + language + " diff against a rubric — Phase 28 prompt example.",
		Messages: []server.PromptMessage{
			{
				Role: "system",
				Text: "You are a careful " + language + " reviewer. Stay concrete and cite the file:line you are referring to.",
			},
			{
				Role: "user",
				Text: "Rubric:\n" + rubric,
			},
			{
				Role: "user",
				Text: "Diff:\n```" + language + "\n" + diff + "\n```",
			},
			{
				Role: "user",
				Text: "Write the review.",
			},
		},
	}, nil
}

// ExplainError is the handler for the `explain_error` prompt: a
// single-message template that asks a model to explain a Go (or
// other-language) error in plain language. Demonstrates the
// "no-message-arrays-needed" minimal prompt shape.
func ExplainError(_ context.Context, req server.PromptRequest) (server.PromptResult, error) {
	errMsg := req.Arguments["error"]
	language := req.Arguments["language"]
	if language == "" {
		language = "Go"
	}
	if errMsg == "" {
		return server.PromptResult{
			Description: "Explain a runtime error in plain language.",
			Messages: []server.PromptMessage{
				{
					Role: "user",
					Text: "No error supplied — call this prompt with an `error` argument.",
				},
			},
		}, nil
	}
	return server.PromptResult{
		Description: "Explain a " + language + " error in plain language.",
		Messages: []server.PromptMessage{
			{
				Role: "user",
				Text: "Explain this " + language + " error in two sentences, then suggest the most likely fix:\n\n" + errMsg,
			},
		},
	}, nil
}
