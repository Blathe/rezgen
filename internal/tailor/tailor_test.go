package tailor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blathe/rezgen/internal/llm/llmtest"
	"github.com/Blathe/rezgen/internal/profile"
)

func loadProfile(t *testing.T) *profile.Profile {
	t.Helper()
	p, err := profile.Load("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func goodDraft(t *testing.T) *Draft {
	t.Helper()
	var d Draft
	if err := json.Unmarshal(readFile(t, "testdata/draft.json"), &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestAnalyze(t *testing.T) {
	fake := &llmtest.Fake{Responses: [][]byte{readFile(t, "testdata/analysis.json")}}
	tl, err := New(fake, loadProfile(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := tl.Analyze(context.Background(), "We need an AI Solutions Engineer.")
	if err != nil {
		t.Fatal(err)
	}
	if a.Company != "Northwind Freight" || len(a.Matches) != 2 {
		t.Errorf("unexpected analysis: %+v", a)
	}
	if a.Questions[0].ID != "q-kubernetes" || a.Questions[1].ID != "q-kubernetes-2" {
		t.Errorf("question IDs not made unique: %q, %q", a.Questions[0].ID, a.Questions[1].ID)
	}
	req := fake.Requests[0]
	if !strings.Contains(req.Prompt, "We need an AI Solutions Engineer.") {
		t.Error("prompt is missing the posting")
	}
	if !strings.Contains(req.System, "fact-1: 8 years building internal tools") || !strings.Contains(req.System, `"acme-ticket-triage"`) {
		t.Error("system prompt is missing the summary facts or the profile")
	}
	if req.Schema["type"] != "object" {
		t.Error("request has no schema")
	}
}

func TestWriteAcceptsGoodDraft(t *testing.T) {
	fake := &llmtest.Fake{Responses: [][]byte{readFile(t, "testdata/analysis.json"), readFile(t, "testdata/draft.json")}}
	tl, _ := New(fake, loadProfile(t))
	a, err := tl.Analyze(context.Background(), "posting")
	if err != nil {
		t.Fatal(err)
	}
	answers := []Answer{{QuestionID: "q-kubernetes", Question: "Kubernetes?", Text: "No."}}
	d, err := tl.Write(context.Background(), "posting", a, answers)
	if err != nil {
		t.Fatal(err)
	}
	if d.Headline != "AI Solutions Engineer" {
		t.Errorf("headline %q", d.Headline)
	}
	if fake.Requests[0].System != fake.Requests[1].System {
		t.Error("system prompt differs between calls, so it can't be cached")
	}
	if !strings.Contains(fake.Requests[1].Prompt, "q-kubernetes\n  Q: Kubernetes?\n  A: No.") {
		t.Errorf("write prompt is missing the answers:\n%s", fake.Requests[1].Prompt)
	}
}

func TestWriteRetriesThenSucceeds(t *testing.T) {
	bad := goodDraft(t)
	bad.Experience[0].Bullets[0].Sources = []string{"made-up-id"}
	badJSON, _ := json.Marshal(bad)
	fake := &llmtest.Fake{Responses: [][]byte{badJSON, readFile(t, "testdata/draft.json")}}
	tl, _ := New(fake, loadProfile(t))
	if _, err := tl.Write(context.Background(), "posting", &Analysis{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.Requests) != 2 {
		t.Fatalf("want 2 calls, got %d", len(fake.Requests))
	}
	retry := fake.Requests[1].Prompt
	if !strings.Contains(retry, `"made-up-id"`) || !strings.Contains(retry, "<previous_draft>") {
		t.Errorf("retry prompt doesn't explain the problem:\n%s", retry)
	}
}

func TestWriteGivesUp(t *testing.T) {
	bad := goodDraft(t)
	bad.Summary.Sources = nil
	badJSON, _ := json.Marshal(bad)
	fake := &llmtest.Fake{Responses: [][]byte{badJSON, badJSON}}
	tl, _ := New(fake, loadProfile(t))
	_, err := tl.Write(context.Background(), "posting", &Analysis{}, nil)
	var de *DraftError
	if !errors.As(err, &de) {
		t.Fatalf("want DraftError, got %v", err)
	}
	if de.Draft == nil || len(de.Problems) != 1 || !strings.Contains(de.Problems[0], "summary cites no sources") {
		t.Errorf("unexpected problems: %v", de.Problems)
	}
}

func TestCheck(t *testing.T) {
	p := loadProfile(t)
	answers := []Answer{{QuestionID: "q-team-size", Text: "12 people"}}
	tests := []struct {
		name   string
		mutate func(d *Draft)
		want   string
	}{
		{"unknown source", func(d *Draft) { d.Summary.Sources = []string{"fact-9"} }, `cites "fact-9"`},
		{"cross-role citation", func(d *Draft) {
			d.Experience[1].Bullets[0].Sources = []string{"acme-ticket-triage"}
		}, `under role "brightpath" but cites "acme-ticket-triage"`},
		{"missing role", func(d *Draft) { d.Experience = d.Experience[:1] }, `role "brightpath" is missing`},
		{"unknown role", func(d *Draft) { d.Experience[0].ID = "globex" }, `"globex", which is not a role`},
		{"unknown project", func(d *Draft) { d.Projects[0].ID = "other" }, `"other", which is not a project`},
		{"invented skill", func(d *Draft) { d.Skills[0].Items = append(d.Skills[0].Items, "Kubernetes") }, `skill "Kubernetes"`},
		{"avoided word", func(d *Draft) { d.CoverLetter.Paragraphs[0].Text = "I am Passionate about this." }, `avoided word "passionate"`},
		{"empty cover letter", func(d *Draft) { d.CoverLetter.Paragraphs = nil }, "cover letter has no paragraphs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := goodDraft(t)
			tt.mutate(d)
			probs := newCatalog(p, answers).check(d)
			for _, pr := range probs {
				if strings.Contains(pr, tt.want) {
					return
				}
			}
			t.Errorf("no problem containing %q; got %v", tt.want, probs)
		})
	}

	t.Run("answers and skills are citable", func(t *testing.T) {
		d := goodDraft(t)
		d.Experience[0].Bullets[0].Sources = append(d.Experience[0].Bullets[0].Sources, "q-team-size")
		d.Skills[0].Items = []string{"claude api", "AWS Lambda"} // case-insensitive; Lambda comes from a highlight
		if probs := newCatalog(p, answers).check(d); len(probs) > 0 {
			t.Errorf("want no problems, got %v", probs)
		}
	})
}

func TestRender(t *testing.T) {
	p := loadProfile(t)
	d := goodDraft(t)
	resume := RenderResume(p, d)
	for _, want := range []string{
		"# Jordan Example\n\n**AI Solutions Engineer**\n\njordan@example.com | 555-0100 | Denver, CO | https://github.com/example | https://linkedin.com/in/example\n",
		"### Senior Software Engineer, Acme Logistics\n\nRemote | Mar 2022 - Present\n",
		"Jun 2018 - Feb 2022",
		"- **[rezgen](https://github.com/Blathe/rezgen)**: Go CLI",
		"- **AI:** Claude API, prompt engineering, tool use",
		"- B.S., Computer Science, Colorado State University (2016)",
		"- AWS Certified Developer - Associate, Amazon Web Services (May 2024)",
	} {
		if !strings.Contains(resume, want) {
			t.Errorf("resume missing %q:\n%s", want, resume)
		}
	}

	letter := RenderCoverLetter(p, d, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{"October 2, 2026", "Dear Northwind Freight hiring team,", "Sincerely,\n\nJordan Example\n"} {
		if !strings.Contains(letter, want) {
			t.Errorf("cover letter missing %q:\n%s", want, letter)
		}
	}

	if src := RenderSources(d); !strings.Contains(src, "| acme #1 | Built a Claude-based") {
		t.Errorf("sources table missing a row:\n%s", src)
	}
}

func TestSave(t *testing.T) {
	p := loadProfile(t)
	root := t.TempDir()
	on := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	app := Application{
		Posting:  "posting text",
		Analysis: &Analysis{Company: "Northwind Freight", Role: "AI Solutions Engineer (Remote)"},
		Draft:    goodDraft(t),
	}
	dir, err := Save(root, p, app, on)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "2026-10-02-northwind-freight-ai-solutions-engineer-remote"); dir != want {
		t.Errorf("dir %s, want %s", dir, want)
	}
	for _, f := range []string{"posting.md", "analysis.json", "draft.json", "resume.md", "cover-letter.md", "sources.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	dir2, err := Save(root, p, app, on)
	if err != nil {
		t.Fatal(err)
	}
	if dir2 != dir+"-2" {
		t.Errorf("second save went to %s, want %s-2", dir2, dir)
	}
}
