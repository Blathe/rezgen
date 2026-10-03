package tui

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Blathe/rezgen/internal/track"
)

// TestPreview renders screens to an HTML file for eyeballing the styling.
// Run with REZGEN_PREVIEW=out.html go test ./internal/tui -run TestPreview
func TestPreview(t *testing.T) {
	out := os.Getenv("REZGEN_PREVIEW")
	if out == "" {
		t.Skip("set REZGEN_PREVIEW to write the preview")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	var shots []string
	snap := func(name string, h *harness) { shots = append(shots, name, h.view()) }

	// Setup.
	s := newHarness(t, false)
	snap("Setup: API key", s)
	s.typeText("sk-ant-good")
	s.key(tea.KeyEnter)
	snap("Setup: model", s)

	// Home with a few applications.
	h := newHarness(t, true)
	root := filepath.Join(h.dataDir, "applications")
	a1, _ := track.Create(root, "Northwind Freight - AI Solutions Engineer", "posting", "https://job-boards.greenhouse.io/northwind/jobs/123", testNow)
	track.Create(root, "Acme Corp - Automation Engineer", "posting", "", testNow.AddDate(0, 0, -2))
	track.Create(root, "Globex - Solutions Engineer (Remote, US)", "posting", "", testNow.AddDate(0, 0, -5))
	os.WriteFile(filepath.Join(a1.Dir, "resume.md"), []byte("x"), 0o644)
	h.typeText("r")
	h.m.setFlash("Added Acme Corp - Automation Engineer. Open it to generate your resume and cover letter.", false)
	snap("Home", h)

	// Generating.
	h.key(tea.KeyDown)
	h.key(tea.KeyEnter)
	snap("Application: not started", h)
	h.m.screen = scrWorking
	h.m.started = testNow.Add(-47e9)
	h.m.steps = []step{
		{label: "Analyze the posting against your profile", state: stepDone, note: "12 matched, 3 gaps"},
		{label: "Questions", state: stepDone, note: "2 questions answered, saved to your profile"},
		{label: "Write the resume and cover letter, and check every line against your profile", state: stepRunning},
		{label: "Build the PDFs"},
	}
	snap("Generating", h)

	// Done.
	h.respond("../tailor/testdata/analysis.json", "../tailor/testdata/draft.json")
	h.m.screen = scrApp
	h.m.steps = nil
	h.typeText("g")
	snap("Question", h)
	h.typeText("No, only Docker.")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.typeText("n")
	snap("Application: done", h)

	h.key(tea.KeyEsc)
	h.typeText(",")
	snap("Settings", h)
	h.key(tea.KeyEsc)
	h.typeText("n")
	snap("New application", h)

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>rezgen preview</title><style>
body{background:#14131a;color:#e8e6f0;font:14px/1.35 "Cascadia Mono",Consolas,monospace;margin:24px}
h2{font:600 15px system-ui;color:#a08cff;margin:28px 0 8px}
pre{background:#1b1a23;padding:8px 0;margin:0;border-radius:8px;width:max-content;min-width:120ch}
</style>`)
	for i := 0; i < len(shots); i += 2 {
		fmt.Fprintf(&b, "<h2>%s</h2><pre>%s</pre>\n", html.EscapeString(shots[i]), ansiToHTML(shots[i+1]))
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	oscRe = regexp.MustCompile("\x1b\\][^\x1b]*\x1b\\\\")
	sgrRe = regexp.MustCompile("\x1b\\[([0-9;]*)m")
)

// ansiToHTML converts the SGR color codes lipgloss emits into spans.
func ansiToHTML(s string) string {
	s = oscRe.ReplaceAllString(s, "")
	var b strings.Builder
	var fg, bg string
	var boldOn, faintOn, under bool
	open := false
	flush := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
		var css []string
		if fg != "" {
			css = append(css, "color:"+fg)
		}
		if bg != "" {
			css = append(css, "background:"+bg)
		}
		if boldOn {
			css = append(css, "font-weight:700")
		}
		if faintOn {
			css = append(css, "opacity:.6")
		}
		if under {
			css = append(css, "text-decoration:underline")
		}
		if len(css) > 0 {
			b.WriteString(`<span style="` + strings.Join(css, ";") + `">`)
			open = true
		}
	}
	last := 0
	for _, loc := range sgrRe.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:loc[0]]))
		last = loc[1]
		codes := strings.Split(s[loc[2]:loc[3]], ";")
		for i := 0; i < len(codes); i++ {
			switch c := codes[i]; c {
			case "", "0":
				fg, bg, boldOn, faintOn, under = "", "", false, false, false
			case "1":
				boldOn = true
			case "2":
				faintOn = true
			case "22":
				boldOn, faintOn = false, false
			case "4":
				under = true
			case "24":
				under = false
			case "39":
				fg = ""
			case "49":
				bg = ""
			case "38", "48":
				if i+4 < len(codes) && codes[i+1] == "2" {
					r, _ := strconv.Atoi(codes[i+2])
					g, _ := strconv.Atoi(codes[i+3])
					bl, _ := strconv.Atoi(codes[i+4])
					col := fmt.Sprintf("#%02x%02x%02x", r, g, bl)
					if c == "38" {
						fg = col
					} else {
						bg = col
					}
					i += 4
				}
			}
		}
		flush()
	}
	b.WriteString(html.EscapeString(s[last:]))
	if open {
		b.WriteString("</span>")
	}
	return b.String()
}
