package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

var debugSeq atomic.Int64

// debugLog writes the request and the raw response to a file in
// $REZGEN_DEBUG_DIR, if set, for diagnosing what the model returned. The
// files include the profile and posting, so the folder should stay local.
func debugLog(req Request, msg *anthropic.BetaMessage) {
	dir := os.Getenv("REZGEN_DEBUG_DIR")
	if dir == "" {
		return
	}
	type block struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	var blocks []block
	for _, b := range msg.Content {
		bl := block{Type: b.Type}
		if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
			bl.Text = t.Text
		}
		blocks = append(blocks, bl)
	}
	data, _ := json.MarshalIndent(map[string]any{
		"model":       msg.Model,
		"stop_reason": msg.StopReason,
		"usage":       msg.Usage,
		"content":     blocks,
		"prompt":      req.Prompt,
	}, "", "  ")
	os.MkdirAll(dir, 0o700)
	name := fmt.Sprintf("%s-%02d.json", time.Now().Format("150405"), debugSeq.Add(1))
	os.WriteFile(filepath.Join(dir, name), data, 0o600)
}
