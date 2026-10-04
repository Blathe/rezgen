package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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
	// Fallbacks turns on server-side refusal fallbacks. It's off by default:
	// a fallback answer comes from a different model, and the API keeps
	// routing similar requests (every call shares rezgen's system prompt) to
	// that model for about an hour, which produced empty drafts.
	Fallbacks bool
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

// effortModels accept the effort setting: every Claude 5 model, Opus 4.5 and
// later, and Sonnet 4.6. Others (Haiku 4.5, older Sonnets) reject it with a
// 400, so it's left out for them.
var effortModels = []string{
	"claude-fable-", "claude-mythos-", "claude-opus-5", "claude-sonnet-5",
	"claude-opus-4-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-sonnet-4-6",
}

// fallbackModels support server-side refusal fallbacks in their "default"
// form, which picks a fallback model by refusal category.
var fallbackModels = []string{"claude-fable-5-1", "claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5"}

func hasPrefix(model string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// JSON sends req with structured output enabled, so the response text is
// JSON that matches req.Schema. On models that support it, it also sets the
// effort level. A response from a model other than the one requested (a
// server-side fallback) is returned as an error rather than used.
func (a *Anthropic) JSON(ctx context.Context, req Request) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16000
	}
	params := anthropic.BetaMessageNewParams{
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
			Format: anthropic.BetaJSONOutputFormatParam{Schema: req.Schema},
		},
	}
	if hasPrefix(a.model, effortModels) {
		params.OutputConfig.Effort = a.effort
		if os.Getenv("REZGEN_DEBUG_DIR") != "" {
			// Ask for a readable summary of the model's reasoning, so the
			// debug log shows why it answered as it did.
			params.Thinking = anthropic.BetaThinkingConfigParamUnion{
				OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{Display: anthropic.BetaThinkingConfigAdaptiveDisplaySummarized},
			}
		}
	}
	if a.Fallbacks && hasPrefix(a.model, fallbackModels) {
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	msg, err := a.client.Beta.Messages.New(ctx, params)
	// effortModels can be wrong about a model; if one rejects effort, try
	// once more without it rather than failing the run.
	if err != nil && params.OutputConfig.Effort != "" && strings.Contains(err.Error(), "does not support the effort parameter") {
		params.OutputConfig.Effort = ""
		msg, err = a.client.Beta.Messages.New(ctx, params)
	}
	if err != nil {
		return nil, err
	}
	debugLog(req, msg)
	if served := fallbackModel(msg); served != "" {
		return nil, fmt.Errorf("%s declined this request and %s answered instead; rezgen doesn't use fallback answers. Try again, or pick another model in settings", a.model, served)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		cat := string(msg.StopDetails.Category)
		if cat == "" {
			cat = "no reason given"
		}
		return nil, fmt.Errorf("%s declined this request (%s). This is usually a false positive; try again, or pick another model in settings", a.model, cat)
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

// fallbackModel returns the model that answered if a server-side fallback
// served the response, or "".
func fallbackModel(msg *anthropic.BetaMessage) string {
	for _, it := range msg.Usage.Iterations {
		if it.Type == "fallback_message" {
			return string(msg.Model)
		}
	}
	return ""
}
