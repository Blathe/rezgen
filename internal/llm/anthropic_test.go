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
	if req["fallbacks"] != "default" {
		t.Errorf("fallbacks %v", req["fallbacks"])
	}
	if !strings.Contains(header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("beta header %q", header.Get("Anthropic-Beta"))
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

func toJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
