package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// serve starts a fake Messages API that records the request and replies with
// body.
func serve(t *testing.T, body string) (*Anthropic, *map[string]any, *http.Header) {
	t.Helper()
	var (
		got    map[string]any
		header http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c := NewAnthropic("", "", option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	return c, &got, &header
}

const okResponse = `{
  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
  "content": [{"type": "text", "text": "{\"ok\":true}"}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 5}
}`

func TestAnthropicJSON(t *testing.T) {
	c, got, header := serve(t, okResponse)
	schema := map[string]any{"type": "object"}
	out, err := c.JSON(context.Background(), Request{System: "sys", Prompt: "hello", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("got %s", out)
	}

	req := *got
	if req["model"] != DefaultModel {
		t.Errorf("model %v", req["model"])
	}
	oc, _ := req["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "high" || format["type"] != "json_schema" {
		t.Errorf("output_config %v", oc)
	}
	sys, _ := req["system"].([]any)
	if len(sys) != 1 || !strings.Contains(toJSON(sys[0]), `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("system not cached: %v", sys)
	}
	if _, has := req["fallbacks"]; has || header.Get("Anthropic-Beta") != "" {
		t.Errorf("fallbacks should be off by default: %v, beta %q", req["fallbacks"], header.Get("Anthropic-Beta"))
	}

	// Opting in sends the default fallback form and its beta header.
	c, got, header = serve(t, okResponse)
	c.Fallbacks = true
	if _, err := c.JSON(context.Background(), Request{System: "sys", Prompt: "hello", Schema: schema}); err != nil {
		t.Fatal(err)
	}
	if (*got)["fallbacks"] != "default" || !strings.Contains(header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("fallbacks %v, beta %q", (*got)["fallbacks"], header.Get("Anthropic-Beta"))
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	tests := []struct{ stop, want string }{
		{`"refusal", "stop_details": {"type": "refusal", "category": "cyber"}`, "declined"},
		{`"max_tokens"`, "token limit"},
	}
	for _, tt := range tests {
		body := strings.Replace(okResponse, `"end_turn"`, tt.stop, 1)
		c, _, _ := serve(t, body)
		_, err := c.JSON(context.Background(), Request{Prompt: "x", Schema: map[string]any{"type": "object"}})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("stop %s: got %v, want error containing %q", tt.stop, err, tt.want)
		}
	}
}

func TestModelCapabilities(t *testing.T) {
	var bodies []map[string]any
	var betas []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &got)
		bodies = append(bodies, got)
		betas = append(betas, r.Header.Get("Anthropic-Beta"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, okResponse)
	}))
	defer srv.Close()
	req := Request{Prompt: "x", Schema: map[string]any{"type": "object"}}
	opts := []option.RequestOption{option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)}

	for _, tt := range []struct {
		model            string
		effort, fallback bool
	}{
		{"claude-haiku-4-5", false, false},
		{"claude-sonnet-4-5", false, false},
		{"claude-sonnet-4-6", true, false},
		{"claude-sonnet-5-5", true, true},
		{"claude-opus-5-5", true, true},
	} {
		// Fallbacks are off by default for every model.
		bodies, betas = nil, nil
		if _, err := NewAnthropic(tt.model, "high", opts...).JSON(context.Background(), req); err != nil {
			t.Fatalf("%s: %v", tt.model, err)
		}
		if _, has := bodies[0]["fallbacks"]; has || strings.Contains(betas[0], "server-side-fallback") {
			t.Errorf("%s: fallbacks sent by default", tt.model)
		}

		bodies, betas = nil, nil
		c := NewAnthropic(tt.model, "high", opts...)
		c.Fallbacks = true
		if _, err := c.JSON(context.Background(), req); err != nil {
			t.Fatalf("%s: %v", tt.model, err)
		}
		oc, _ := bodies[0]["output_config"].(map[string]any)
		_, hasEffort := oc["effort"]
		_, hasFallbacks := bodies[0]["fallbacks"]
		if hasEffort != tt.effort || hasFallbacks != tt.fallback || strings.Contains(betas[0], "server-side-fallback") != tt.fallback {
			t.Errorf("%s: effort=%v fallbacks=%v beta=%q", tt.model, hasEffort, hasFallbacks, betas[0])
		}
		if oc["format"] == nil {
			t.Errorf("%s: structured output missing", tt.model)
		}
	}
}

func TestRejectsFallbackAnswers(t *testing.T) {
	body := strings.Replace(okResponse, `"usage": {"input_tokens": 10, "output_tokens": 5}`,
		`"usage": {"input_tokens": 10, "output_tokens": 5, "iterations": [{"type": "message", "input_tokens": 10, "output_tokens": 0}, {"type": "fallback_message", "input_tokens": 10, "output_tokens": 5}]}`, 1)
	body = strings.Replace(body, `"model": "claude-opus-5-5"`, `"model": "claude-opus-4-8"`, 1)
	c, _, _ := serve(t, body)
	_, err := c.JSON(context.Background(), Request{Prompt: "x", Schema: map[string]any{"type": "object"}})
	if err == nil || !strings.Contains(err.Error(), "claude-opus-4-8 answered instead") {
		t.Errorf("want a fallback error, got %v", err)
	}
}

func TestRetriesWithoutEffort(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var got map[string]any
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &got)
		w.Header().Set("Content-Type", "application/json")
		if oc, _ := got["output_config"].(map[string]any); oc["effort"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"This model does not support the effort parameter."}}`)
			return
		}
		io.WriteString(w, okResponse)
	}))
	defer srv.Close()
	c := NewAnthropic("claude-opus-5-5", "high", option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	if _, err := c.JSON(context.Background(), Request{Prompt: "x", Schema: map[string]any{"type": "object"}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestCheckAnthropicKey(t *testing.T) {
	status := http.StatusOK
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			io.WriteString(w, `{"data": [], "has_more": false, "first_id": null, "last_id": null}`)
		} else {
			io.WriteString(w, `{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`)
		}
	}))
	defer srv.Close()

	if err := CheckAnthropicKey(context.Background(), "sk-ant-good", option.WithBaseURL(srv.URL)); err != nil || gotKey != "sk-ant-good" {
		t.Errorf("good key: %v (sent %q)", err, gotKey)
	}
	status = http.StatusUnauthorized
	if err := CheckAnthropicKey(context.Background(), "sk-ant-bad", option.WithBaseURL(srv.URL)); err == nil || !strings.Contains(err.Error(), "rejected that key") {
		t.Errorf("bad key: %v", err)
	}
}

func toJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
