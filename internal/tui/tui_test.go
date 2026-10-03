package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/llm/llmtest"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/track"
)

var testNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

type harness struct {
	t      *testing.T
	m      Model
	opened []string
	fake   *llmtest.Fake
	root   string
	prof   string
}

func newHarness(t *testing.T, responses ...string) *harness {
	t.Helper()
	h := &harness{t: t, root: filepath.Join(t.TempDir(), "applications"), fake: &llmtest.Fake{}}
	for _, r := range responses {
		data, err := os.ReadFile(r)
		if err != nil {
			t.Fatal(err)
		}
		h.fake.Responses = append(h.fake.Responses, data)
	}
	data, err := os.ReadFile("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	h.prof = filepath.Join(t.TempDir(), "profile.json")
	os.WriteFile(h.prof, data, 0o644)

	h.m = New(Config{
		ProfilePath: h.prof,
		OutDir:      h.root,
		NewClient:   func(string, string) llm.Client { return h.fake },
		Now:         func() time.Time { return testNow },
		Open:        func(p string) error { h.opened = append(h.opened, p); return nil },
	})
	h.do(h.m.Init())
	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	return h
}

// send delivers msgs and runs the commands they produce until things settle.
func (h *harness) send(msgs ...tea.Msg) {
	for _, msg := range msgs {
		next, cmd := h.m.Update(msg)
		h.m = next.(Model)
		h.do(cmd)
	}
}

// do runs cmd and feeds its messages back in. Commands that don't return
// quickly (cursor blinks, spinner animation) are dropped, and spinner ticks
// are ignored, so tests never wait on timers.
func (h *harness) do(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return
	}
	switch msg := msg.(type) {
	case nil, spinner.TickMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			h.do(c)
		}
	default:
		h.send(msg)
	}
}

func (h *harness) typeText(s string) { h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
func (h *harness) key(k tea.KeyType) { h.send(tea.KeyMsg{Type: k}) }
func (h *harness) press(s string)    { h.typeText(s) }

func (h *harness) view() string { return h.m.View() }

func (h *harness) addApp(name, company, role string, created time.Time) string {
	dir := filepath.Join(h.root, name)
	os.MkdirAll(dir, 0o755)
	if err := track.Save(dir, track.New(company, role, "", created)); err != nil {
		h.t.Fatal(err)
	}
	return dir
}

func TestListAndStatusChange(t *testing.T) {
	h := newHarness(t)
	if !strings.Contains(h.view(), "No applications") {
		t.Fatalf("empty list view:\n%s", h.view())
	}
	dir := h.addApp("2026-10-01-northwind-ai", "Northwind Freight", "AI Solutions Engineer", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	h.addApp("2026-10-02-acme-automation", "Acme", "Automation Engineer", time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	h.press("r")

	v := h.view()
	if !strings.Contains(v, "Northwind Freight") || !strings.Contains(v, "2 applications: 2 draft") {
		t.Fatalf("list view:\n%s", v)
	}

	// Newest first, so Northwind is second.
	h.key(tea.KeyDown)
	h.press("s")
	if h.m.screen != scrStatus || !strings.Contains(h.view(), "New status for Northwind Freight") {
		t.Fatalf("status picker:\n%s", h.view())
	}
	h.press("2") // applied
	h.typeText("via referral")
	h.key(tea.KeyEnter)

	r, err := track.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != track.Applied || r.History[1].Note != "via referral" || r.History[1].Date != "2026-10-03" {
		t.Errorf("record not updated: %+v", r)
	}
	if h.m.screen != scrList || !strings.Contains(h.view(), "Northwind Freight: draft -> applied") {
		t.Errorf("after save:\n%s", h.view())
	}
	if !strings.Contains(h.view(), "1 draft") || !strings.Contains(h.view(), "1 applied") {
		t.Errorf("counts not refreshed:\n%s", h.view())
	}
}

func TestDetailNoteAndOpen(t *testing.T) {
	h := newHarness(t)
	dir := h.addApp("2026-10-01-northwind-ai", "Northwind Freight", "AI Solutions Engineer", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	os.WriteFile(filepath.Join(dir, "resume.pdf"), []byte("%PDF-"), 0o644)
	h.press("r")

	h.key(tea.KeyEnter)
	if h.m.screen != scrDetail || !strings.Contains(h.view(), "Files: resume.pdf") {
		t.Fatalf("detail view:\n%s", h.view())
	}
	h.press("a")
	h.typeText("recruiter emailed")
	h.key(tea.KeyEnter)
	if h.m.screen != scrDetail || !strings.Contains(h.view(), "recruiter emailed") {
		t.Errorf("note not shown in detail:\n%s", h.view())
	}
	r, _ := track.Load(dir)
	if r.Status != track.Draft || len(r.History) != 2 {
		t.Errorf("note should keep the status: %+v", r)
	}

	h.press("o")
	if len(h.opened) != 1 || h.opened[0] != filepath.Join(dir, "resume.pdf") {
		t.Errorf("opened %v", h.opened)
	}
	h.key(tea.KeyEsc)
	if h.m.screen != scrList {
		t.Error("esc should return to the list")
	}
}

func TestNewApplicationFlow(t *testing.T) {
	h := newHarness(t, "../tailor/testdata/analysis.json", "../tailor/testdata/draft.json")
	posting := filepath.Join(t.TempDir(), "posting.txt")
	os.WriteFile(posting, []byte("Northwind Freight is hiring an AI Solutions Engineer."), 0o644)

	h.press("n")
	if h.m.screen != scrPosting {
		t.Fatalf("posting screen:\n%s", h.view())
	}
	h.typeText(`"` + posting + `"`) // quotes from a pasted Windows path are stripped
	h.key(tea.KeyEnter)

	if h.m.screen != scrQuestion || !strings.Contains(h.view(), "Question 1 of 2") || !strings.Contains(h.view(), "2 requirements matched") {
		t.Fatalf("question screen:\n%s", h.view())
	}
	h.typeText("No, only Docker.")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter) // skip the second question

	if h.m.screen != scrLearn || !strings.Contains(h.view(), "Save 1 answer") {
		t.Fatalf("learn screen:\n%s", h.view())
	}
	h.press("y")

	if h.m.screen != scrDone {
		t.Fatalf("want done screen, got %d:\n%s", h.m.screen, h.view())
	}
	dir := filepath.Join(h.root, "2026-10-03-northwind-freight-ai-solutions-engineer")
	for _, f := range []string{"resume.pdf", "cover-letter.pdf", "resume.md", "application.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if !strings.Contains(h.view(), "Saved to") || !strings.Contains(h.view(), "Saved 1 answer to your profile") {
		t.Errorf("done view:\n%s", h.view())
	}
	p, err := profile.Load(h.prof)
	if err != nil || len(p.LearnedFacts) != 1 || p.LearnedFacts[0].Answer != "No, only Docker." {
		t.Errorf("answer not learned: %v %+v", err, p)
	}
	if !strings.Contains(h.fake.Requests[1].Prompt, "No, only Docker.") {
		t.Error("answer not sent to the write call")
	}

	h.press("o")
	if len(h.opened) != 1 || h.opened[0] != filepath.Join(dir, "resume.pdf") {
		t.Errorf("opened %v", h.opened)
	}
	h.key(tea.KeyEnter)
	if h.m.screen != scrList || h.m.selected() == nil || h.m.selected().Dir != dir {
		t.Errorf("new application should be selected on the list:\n%s", h.view())
	}
}

func TestNewApplicationErrors(t *testing.T) {
	h := newHarness(t)
	h.press("n")
	h.typeText(filepath.Join(t.TempDir(), "missing.txt"))
	h.key(tea.KeyEnter)
	if h.m.screen != scrPosting || !h.m.flashErr {
		t.Errorf("a bad path should return to the posting screen with an error:\n%s", h.view())
	}
	h.key(tea.KeyEsc)
	if h.m.screen != scrList {
		t.Error("esc should cancel back to the list")
	}

	os.WriteFile(h.prof, []byte("{}"), 0o644)
	h.press("n")
	if h.m.screen != scrList || !h.m.flashErr || !strings.Contains(h.view(), "profile has") {
		t.Errorf("an invalid profile should be reported:\n%s", h.view())
	}
}

func TestCancelWhileWorking(t *testing.T) {
	h := newHarness(t)
	h.press("n")
	h.m.screen = scrWorking
	h.m.working = "Analyzing..."
	h.key(tea.KeyEsc)
	if h.m.screen != scrList || !strings.Contains(h.view(), "Cancelled.") {
		t.Errorf("after esc:\n%s", h.view())
	}
	// A late result from the cancelled work is ignored.
	h.send(analyzedMsg{})
	if h.m.screen != scrList {
		t.Error("late result changed the screen")
	}
}
