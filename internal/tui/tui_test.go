package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/config"
	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/llm/llmtest"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/track"
)

var testNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

type harness struct {
	t        *testing.T
	m        Model
	fake     *llmtest.Fake
	opened   []string
	keyErr   error
	dataDir  string
	importDr string
}

// newHarness builds the app with fakes for the model, key check, clock and
// file opener. With setUp, a config and the example profile already exist.
func newHarness(t *testing.T, setUp bool) *harness {
	t.Helper()
	h := &harness{t: t, fake: &llmtest.Fake{}, dataDir: filepath.Join(t.TempDir(), "rezgen"), importDr: t.TempDir()}
	t.Setenv("REZGEN_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("ANTHROPIC_API_KEY", "")
	if setUp {
		if err := config.Save(&config.Config{Provider: config.Anthropic, APIKey: "sk-ant-api03-test-key-0000", Model: "claude-opus-5-5", DataDir: h.dataDir}); err != nil {
			t.Fatal(err)
		}
		copyExample(t, filepath.Join(h.dataDir, "profile.json"))
	}
	h.m = New(Config{
		ImportDir: h.importDr,
		NewClient: func(*config.Config) llm.Client { return h.fake },
		CheckKey:  func(context.Context, config.Provider, string) error { return h.keyErr },
		Now:       func() time.Time { return testNow },
		Open:      func(p string) error { h.opened = append(h.opened, p); return nil },
	})
	h.do(h.m.Init())
	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	return h
}

func copyExample(t *testing.T, to string) {
	t.Helper()
	data, err := os.ReadFile("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(to), 0o755)
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) respond(files ...string) {
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			h.t.Fatal(err)
		}
		h.fake.Responses = append(h.fake.Responses, data)
	}
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
// quickly (cursor blinks, spinner frames) are dropped and spinner ticks are
// ignored, so tests never wait on timers.
func (h *harness) do(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(300 * time.Millisecond):
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
func (h *harness) view() string      { return h.m.View() }

func (h *harness) wantScreen(s screen) {
	h.t.Helper()
	if h.m.screen != s {
		h.t.Fatalf("screen = %d, want %d\n%s", h.m.screen, s, h.view())
	}
}

func (h *harness) wantView(parts ...string) {
	h.t.Helper()
	v := h.view()
	for _, p := range parts {
		if !strings.Contains(v, p) {
			h.t.Errorf("view missing %q:\n%s", p, v)
		}
	}
}

// clearInput empties the single-line input.
func (h *harness) clearInput() { h.send(tea.KeyMsg{Type: tea.KeyCtrlU}) }

func TestSetupWithImport(t *testing.T) {
	// Files left by an older version in the starting folder.
	h0 := t.TempDir()
	os.WriteFile(filepath.Join(h0, ".env"), []byte("ANTHROPIC_API_KEY=sk-ant-from-env-file-1234\n"), 0o600)
	copyExample(t, filepath.Join(h0, "profile.json"))
	old, _ := track.Create(filepath.Join(h0, "applications"), "Northwind", "posting", "", testNow)

	h := &harness{t: t, fake: &llmtest.Fake{}, dataDir: filepath.Join(t.TempDir(), "rezgen")}
	t.Setenv("REZGEN_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("ANTHROPIC_API_KEY", "")
	h.m = New(Config{
		ImportDir: h0,
		NewClient: func(*config.Config) llm.Client { return h.fake },
		CheckKey:  func(context.Context, config.Provider, string) error { return nil },
		Now:       func() time.Time { return testNow },
	})
	h.do(h.m.Init())

	h.wantScreen(scrSetup)
	h.wantView("an API key in .env (sk-ant-...1234)", "1 application in")
	h.typeText("y")
	if h.m.textIn.Value() != "sk-ant-from-env-file-1234" {
		t.Errorf("key not prefilled: %q", h.m.textIn.Value())
	}
	h.key(tea.KeyEnter) // check key
	h.wantView("Which model", "claude-opus-5-5", "recommended")
	h.typeText("2")
	h.clearInput()
	h.typeText(h.dataDir)
	h.key(tea.KeyEnter)

	h.wantScreen(scrHome)
	h.wantView("Settings saved. Imported profile.json; imported 1 application.", "Northwind")
	c, err := config.Load()
	if err != nil || c.Model != "claude-sonnet-5-5" || c.APIKey != "sk-ant-from-env-file-1234" || c.DataDir != h.dataDir {
		t.Errorf("config: %+v, %v", c, err)
	}
	if !fileExists(filepath.Join(h.dataDir, "profile.json")) || !fileExists(filepath.Join(h0, "profile.json")) {
		t.Error("profile should be copied, not moved")
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "applications", old.Folder())); err != nil {
		t.Error("application not imported")
	}
}

const draftedProfile = `{
  "contact": {"name": "Jordan Example", "email": "jordan@example.com", "phone": "", "location": "", "links": []},
  "headline_variants": ["Engineer"], "summary_facts": ["Builds things"],
  "experience": [{"id": "acme", "company": "Acme", "title": "Engineer", "location": "", "start": "2022-03", "end": "", "context": "",
    "highlights": [{"id": "acme-x", "text": "Built a thing", "metrics": [], "skills": ["Go"], "tags": []}]}],
  "projects": [], "skills": [], "education": [], "certifications": [], "cover_letter_stories": [],
  "notes": ["Add metrics to your Acme work"]
}`

func TestSetupDraftsProfile(t *testing.T) {
	h := newHarness(t, false)
	h.wantScreen(scrSetup)
	h.wantView("First, your API key")

	h.typeText("not a key")
	h.key(tea.KeyEnter)
	h.wantView("doesn't look like an Anthropic")

	h.clearInput()
	h.typeText("sk-proj-openai")
	h.key(tea.KeyEnter)
	h.wantView("OpenAI support is coming")

	h.keyErr = errors.New("invalid x-api-key")
	h.clearInput()
	h.typeText("sk-ant-bad")
	h.key(tea.KeyEnter)
	h.wantScreen(scrSetup)
	h.wantView("That key didn't work: invalid x-api-key")

	h.keyErr = nil
	h.clearInput()
	h.typeText("sk-ant-good")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter) // first model
	h.clearInput()
	h.typeText(h.dataDir)
	h.key(tea.KeyEnter)

	h.wantView("Now your profile")
	h.typeText("1")
	h.typeText("Jordan Example\njordan@example.com\nEngineer at Acme since March 2022")
	h.fake.Responses = [][]byte{[]byte(draftedProfile)}
	h.send(tea.KeyMsg{Type: tea.KeyCtrlS})

	h.wantView("Here's your draft profile", "1 role, 1 highlight", "Engineer, Acme (2022-03 to present)", "Add metrics to your Acme work")
	if !strings.Contains(h.fake.Requests[0].Prompt, "Engineer at Acme since March 2022") {
		t.Error("pasted text not sent")
	}
	h.key(tea.KeyEnter)
	h.wantScreen(scrHome)
	if _, err := profile.Load(filepath.Join(h.dataDir, "profile.json")); err != nil {
		t.Errorf("profile not saved: %v", err)
	}
	h.wantView("No applications yet")
}

func TestAddGenerateRegenerateDelete(t *testing.T) {
	h := newHarness(t, true)
	h.wantScreen(scrHome)
	h.wantView("No applications yet")

	// Add an application by pasting the description.
	h.typeText("n")
	h.typeText("AI Solutions Engineer\nNorthwind Freight\n" + strings.Repeat("Build LLM automations for operations teams. ", 10))
	h.send(tea.KeyMsg{Type: tea.KeyCtrlS})
	h.wantScreen(scrNewName)
	if h.m.textIn.Value() != "AI Solutions Engineer" {
		t.Errorf("suggested name %q", h.m.textIn.Value())
	}
	h.clearInput()
	h.typeText("Northwind - AI Solutions Engineer")
	h.key(tea.KeyEnter)
	h.wantScreen(scrHome)
	h.wantView("not started", "Northwind - AI Solutions Engineer", "● 0 generated", "○ 1 not started")

	// Open it and generate.
	h.key(tea.KeyEnter)
	h.wantScreen(scrApp)
	h.wantView("Press g to generate")
	h.respond("../tailor/testdata/analysis.json", "../tailor/testdata/draft.json")
	h.typeText("g")
	h.wantScreen(scrQuestion)
	h.wantView("Question 1 of 2", "2 requirements matched")
	h.typeText("No, only Docker.")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter) // skip
	h.wantScreen(scrLearn)
	h.typeText("y")

	h.wantScreen(scrApp)
	h.wantView("Done! Your resume and cover letter are ready.", "resume.pdf", "cover-letter.pdf", "● generated")
	a := h.m.app
	for _, f := range []string{"resume.pdf", "cover-letter.pdf", "resume.md", "sources.md"} {
		if !a.Has(f) {
			t.Errorf("missing %s", f)
		}
	}
	if p, _ := profile.Load(filepath.Join(h.dataDir, "profile.json")); len(p.LearnedFacts) != 1 {
		t.Error("answer not saved to the profile")
	}
	if got, _ := track.Load(a.Dir); got.Company != "Northwind Freight" || got.Name != "Northwind - AI Solutions Engineer" {
		t.Errorf("record: %+v", got.Record)
	}

	h.typeText("o")
	if len(h.opened) != 1 || h.opened[0] != filepath.Join(a.Dir, "resume.pdf") {
		t.Errorf("opened %v", h.opened)
	}
	h.typeText("v")
	h.wantScreen(scrViewer)
	h.wantView("Sources", "acme #1")
	h.key(tea.KeyEsc)
	h.wantScreen(scrApp)

	// Regenerate: the first version moves to v1/.
	h.respond("../tailor/testdata/analysis.json", "../tailor/testdata/draft.json")
	h.typeText("g")
	h.wantScreen(scrConfirm)
	h.typeText("y")
	h.key(tea.KeyEsc) // skip questions
	h.wantScreen(scrApp)
	h.wantView("Done!", "Earlier   v1")
	if !a.Has(filepath.Join("v1", "resume.pdf")) || !a.Has("resume.pdf") {
		t.Error("regenerating should keep the old version in v1/")
	}

	h.key(tea.KeyEsc)
	h.wantScreen(scrHome)
	h.wantView("● 1 generated", "○ 0 not started")

	h.typeText("d")
	h.wantScreen(scrConfirm)
	h.typeText("y")
	h.wantScreen(scrHome)
	h.wantView("Deleted Northwind - AI Solutions Engineer.", "No applications yet")
	if _, err := os.Stat(a.Dir); err == nil {
		t.Error("folder not deleted")
	}
}

func TestAddFromLinkErrorsKeepInput(t *testing.T) {
	h := newHarness(t, true)
	h.typeText("n")
	h.typeText("C:\\no\\such\\posting.txt")
	h.send(tea.KeyMsg{Type: tea.KeyCtrlS})
	h.wantScreen(scrNew)
	h.wantView("isn't a link or a file")
	if h.m.area.Value() != "C:\\no\\such\\posting.txt" {
		t.Error("input should be kept after an error")
	}
	h.key(tea.KeyEsc)
	h.wantScreen(scrHome)
}

func TestGenerateFailureAndCancel(t *testing.T) {
	h := newHarness(t, true)
	a, _ := track.Create(filepath.Join(h.dataDir, "applications"), "Acme", "posting text", "", testNow)
	h.typeText("r")
	h.key(tea.KeyEnter)
	h.wantScreen(scrApp)

	// No scripted response, so the analysis call fails.
	h.typeText("g")
	h.wantScreen(scrApp)
	if !h.m.flashErr {
		t.Errorf("want an error:\n%s", h.view())
	}

	// Esc while working goes back to the application.
	h.m.screen = scrWorking
	h.m.gen.active = true
	h.key(tea.KeyEsc)
	h.wantScreen(scrApp)
	h.wantView("Cancelled.")
	h.send(analyzedMsg{}) // a late result is ignored
	h.wantScreen(scrApp)
	if got, _ := track.Load(a.Dir); got.Status != track.NotStarted {
		t.Error("status changed")
	}
}

func TestSettingsChangeModel(t *testing.T) {
	h := newHarness(t, true)
	h.typeText(",")
	h.wantScreen(scrSettings)
	h.wantView("claude-opus-5-5", "sk-ant-...0000")
	h.typeText("m")
	h.typeText("3")
	h.wantView("Model set to claude-haiku-4-5.")
	if c, _ := config.Load(); c.Model != "claude-haiku-4-5" {
		t.Errorf("model not saved: %+v", c)
	}
	h.key(tea.KeyEsc)
	h.wantScreen(scrHome)
}

func TestEditRebuildsPDF(t *testing.T) {
	h := newHarness(t, true)
	a, _ := track.Create(filepath.Join(h.dataDir, "applications"), "Acme", "posting", "", testNow)
	md := filepath.Join(a.Dir, "resume.md")
	os.WriteFile(md, []byte("# Jordan Example\n\n- Edited bullet\n"), 0o644)
	h.typeText("r")
	h.key(tea.KeyEnter)
	h.send(editedMsg{path: md})
	h.wantView("Rebuilt resume.pdf (1 page).")
	if !a.Has("resume.pdf") {
		t.Error("PDF not rebuilt")
	}
}

func TestMissingProfileRunsProfileSetup(t *testing.T) {
	h := newHarness(t, true)
	os.Remove(filepath.Join(h.dataDir, "profile.json"))
	h2 := New(h.m.cfg)
	if h2.screen != scrSetup || h2.setup.step != stepProfile {
		t.Errorf("want profile setup, got screen %d step %d", h2.screen, h2.setup.step)
	}
}
