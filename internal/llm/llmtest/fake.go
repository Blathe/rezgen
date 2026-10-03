// Package llmtest provides a scripted llm.Client for tests.
package llmtest

import (
	"context"
	"fmt"

	"github.com/Blathe/rezgen/internal/llm"
)

// Fake returns its Responses in order and records every request.
type Fake struct {
	Responses [][]byte
	Requests  []llm.Request
}

func (f *Fake) JSON(_ context.Context, req llm.Request) ([]byte, error) {
	f.Requests = append(f.Requests, req)
	if len(f.Requests) > len(f.Responses) {
		return nil, fmt.Errorf("llmtest: unexpected call %d (only %d responses scripted)", len(f.Requests), len(f.Responses))
	}
	return f.Responses[len(f.Requests)-1], nil
}
