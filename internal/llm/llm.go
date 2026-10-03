// Package llm is the narrow seam between rezgen and a language model: one
// call that takes a prompt and a JSON Schema and returns JSON matching it.
// The pipeline depends on the Client interface so tests can run without the
// network.
package llm

import "context"

// Request is one structured-output call.
type Request struct {
	// System is stable context (instructions, the profile). It is sent first
	// and marked for prompt caching so repeated calls reuse it.
	System string
	// Prompt is the per-call input.
	Prompt string
	// Schema is the JSON Schema the response must match.
	Schema map[string]any
	// MaxTokens caps the response length.
	MaxTokens int64
}

// Client returns the model's JSON response to req.
type Client interface {
	JSON(ctx context.Context, req Request) ([]byte, error)
}
