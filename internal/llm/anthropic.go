package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// DefaultModel is the model rezgen uses unless told otherwise.
const DefaultModel = "claude-opus-5-5"

// Anthropic calls the Claude API. Credentials come from the environment
// (ANTHROPIC_API_KEY, or a profile from `ant auth login`).
type Anthropic struct {
	client anthropic.Client
	model  string
	effort anthropic.BetaOutputConfigEffort
}

// NewAnthropic returns a Client for model at the given effort level
// ("low" through "max"; empty means "high"). Without an option.WithAPIKey,
// the key comes from the environment.
func NewAnthropic(model, effort string, opts ...option.RequestOption) *Anthropic {
	if model == "" {
		model = DefaultModel
	}
	if effort == "" {
		effort = "high"
	}
	return &Anthropic{
		client: anthropic.NewClient(opts...),
		model:  model,
		effort: anthropic.BetaOutputConfigEffort(effort),
	}
}

// CheckAnthropicKey makes the cheapest authenticated call (listing one
// model) to confirm key works, so setup can catch a typo immediately.
func CheckAnthropicKey(ctx context.Context, key string, opts ...option.RequestOption) error {
	c := anthropic.NewClient(append([]option.RequestOption{option.WithAPIKey(key), option.WithMaxRetries(1)}, opts...)...)
	_, err := c.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1)})
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
		return errors.New("the API rejected that key; check it at console.anthropic.com")
	}
	return err
}

// JSON sends req with structured output enabled, so the response text is
// JSON that matches req.Schema. Server-side fallbacks are on: if a safety
// classifier declines the request, the API re-serves it on a fallback model
// within the same call instead of failing.
func (a *Anthropic) JSON(ctx context.Context, req Request) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16000
	}
	msg, err := a.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: maxTokens,
		System: []anthropic.BetaTextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.Prompt)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: a.effort,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: req.Schema},
		},
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return nil, err
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return nil, fmt.Errorf("model declined the request (%s)", msg.StopDetails.Category)
	case anthropic.BetaStopReasonMaxTokens:
		return nil, fmt.Errorf("response hit the %d-token limit before finishing", maxTokens)
	}
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			return []byte(t.Text), nil
		}
	}
	return nil, fmt.Errorf("response had no text (stop reason %q)", msg.StopReason)
}
