package tailor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func goodLetter(t *testing.T) *CoverLetter {
	t.Helper()
	var cl CoverLetter
	if err := json.Unmarshal(readFile(t, "testdata/cover_letter.json"), &cl); err != nil {
		t.Fatal(err)
	}
	return &cl
}

const testPosting = "Northwind Freight is hiring its first AI Solutions Engineer for its operations, finance and support teams."

func TestWriteResumeThenLetter(t *testing.T) {
	fake := &llmtest.Fake{Responses: [][]byte{
		readFile(t, "testdata/analysis.json"), readFile(t, "testdata/draft.json"), readFile(t, "testdata/cover_letter.json"),
	}}
	tl, _ := New(fake, loadProfile(t))
	a, err := tl.Analyze(context.Background(), testPosting)
	if err != nil {
		t.Fatal(err)
	}
	answers := []Answer{{QuestionID: "q-kubernetes", Question: "Kubernetes?", Text: "No."}}
	d, err := tl.Write(context.Background(), testPosting, a, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Requests) != 3 {
		t.Fatalf("want analyze, resume and letter calls, got %d", len(fake.Requests))
	}
	if d.Headline != "AI Solutions Engineer" || !strings.HasPrefix(d.CoverLetter.Paragraphs[0].Text, "Northwind is hiring") || len(d.Warnings) != 0 {
		t.Errorf("unexpected draft: %q, %+v, warnings %v", d.Headline, d.CoverLetter, d.Warnings)
	}
	for i := 1; i < 3; i++ {
		if fake.Requests[i].System != fake.Requests[0].System {
			t.Error("system prompt differs between calls, so it can't be cached")
		}
		if !strings.Contains(fake.Requests[i].Prompt, "q-kubernetes\n  Q: Kubernetes?\n  A: No.") {
			t.Errorf("call %d is missing the answers", i)
		}
	}
	resume, letter := fake.Requests[1], fake.Requests[2]
	if _, ok := resume.Schema["properties"].(map[string]any)["cover_letter"]; ok {
		t.Error("the resume call should not ask for a cover letter")
	}
	if !strings.Contains(letter.Prompt, "<resume>") || !strings.Contains(letter.Prompt, "Built a Claude-based triage service") {
		t.Error("the letter call should see the finished resume")
	}
	if !strings.Contains(letter.Prompt, "BAD (a fact dump)") {
		t.Error("the letter prompt should show what to avoid")
	}
}

func TestWriteRetriesThenSucceeds(t *testing.T) {
	bad := goodDraft(t)
	bad.Experience[0].Bullets[0].Sources = []string{"made-up-id"}
	badJSON, _ := json.Marshal(bad)
	fake := &llmtest.Fake{Responses: [][]byte{badJSON, readFile(t, "testdata/draft.json"), readFile(t, "testdata/cover_letter.json")}}
	tl, _ := New(fake, loadProfile(t))
	if _, err := tl.Write(context.Background(), testPosting, &Analysis{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.Requests) != 3 {
		t.Fatalf("want 3 calls, got %d", len(fake.Requests))
	}
	retry := fake.Requests[1].Prompt
	if !strings.Contains(retry, `"made-up-id"`) || !strings.Contains(retry, "<previous_attempt>") || !strings.Contains(retry, "must all be fixed") {
		t.Errorf("retry prompt doesn't explain the problem:\n%s", retry)
	}
}

func TestWriteGivesUp(t *testing.T) {
	bad := goodDraft(t)
	bad.Summary.Sources = nil
	badJSON, _ := json.Marshal(bad)
	fake := &llmtest.Fake{Responses: [][]byte{badJSON, badJSON}}
	tl, _ := New(fake, loadProfile(t))
	_, err := tl.Write(context.Background(), testPosting, &Analysis{}, nil)
	var de *DraftError
	if !errors.As(err, &de) {
		t.Fatalf("want DraftError, got %v", err)
	}
	if de.Draft == nil || len(de.Problems) != 1 || !strings.Contains(de.Problems[0], "summary cites no sources") {
		t.Errorf("unexpected problems: %v", de.Problems)
	}
}

func TestLetterStyleWarningsRetryThenStick(t *testing.T) {
	dump := goodLetter(t)
	dump.Paragraphs[1] = Line{
		Text:    "I built a triage service. I cut first-response time 40%. I automated invoice entry. I saved 30 hours a week.",
		Sources: []string{"acme-ticket-triage", "acme-invoice-extract", "brightpath-scheduling", "fact-1"},
	}
	dumpJSON, _ := json.Marshal(dump)
	fake := &llmtest.Fake{Responses: [][]byte{readFile(t, "testdata/draft.json"), dumpJSON, dumpJSON}}
	tl, _ := New(fake, loadProfile(t))
	d, err := tl.Write(context.Background(), testPosting, &Analysis{}, nil)
	if err != nil {
		t.Fatalf("style warnings shouldn't fail the draft: %v", err)
	}
	if len(fake.Requests) != 3 || !strings.Contains(fake.Requests[2].Prompt, "These parts read badly") {
		t.Fatalf("the letter should be retried once with the warnings")
	}
	joined := strings.Join(d.Warnings, "\n")
	if !strings.Contains(joined, "4 separate sources") || !strings.Contains(joined, `three sentences in a row with "I"`) {
		t.Errorf("warnings not kept: %v", d.Warnings)
	}
}

func TestCheckResume(t *testing.T) {
	p := loadProfile(t)
	answers := []Answer{{QuestionID: "q-team-size", Text: "12 people"}}
	problems := []struct {
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
		{"avoided word", func(d *Draft) { d.Summary.Text = "A passionate engineer." }, `avoided word "passionate"`},
		{"no bullets", func(d *Draft) { d.Experience[1].Bullets = nil }, `role "brightpath" has no bullets`},
	}
	for _, tt := range problems {
		t.Run(tt.name, func(t *testing.T) {
			d := goodDraft(t)
			tt.mutate(d)
			f := newCatalog(p, answers).checkResume(d)
			for _, pr := range f.problems {
				if strings.Contains(pr, tt.want) {
					return
				}
			}
			t.Errorf("no problem containing %q; got %v", tt.want, f.problems)
		})
	}

	warnings := []struct {
		name   string
		mutate func(d *Draft)
		want   string
	}{
		{"semicolon bullet", func(d *Draft) { d.Experience[0].Bullets[0].Text = "Built a triage service; cut response time" }, "semicolon"},
		{"tenure summary", func(d *Draft) { d.Summary.Text = "Spent nearly 8 years building internal tools." }, "leads with tenure"},
		{"repeated verb", func(d *Draft) { d.Experience[0].Bullets[1].Text = "Built an invoice extraction workflow." }, `more than one bullet with "built"`},
		{"project name", func(d *Draft) { d.Projects[0].Text = "rezgen: a Go CLI." }, "repeats the project's name"},
	}
	for _, tt := range warnings {
		t.Run(tt.name, func(t *testing.T) {
			d := goodDraft(t)
			tt.mutate(d)
			f := newCatalog(p, answers).checkResume(d)
			if len(f.problems) > 0 {
				t.Errorf("style issues shouldn't be problems: %v", f.problems)
			}
			for _, w := range f.warnings {
				if strings.Contains(w, tt.want) {
					return
				}
			}
			t.Errorf("no warning containing %q; got %v", tt.want, f.warnings)
		})
	}

	t.Run("brief roles need no bullets", func(t *testing.T) {
		d := goodDraft(t)
		d.Experience[1] = RoleDraft{ID: "brightpath", Brief: true}
		if f := newCatalog(p, answers).checkResume(d); len(f.problems)+len(f.warnings) > 0 {
			t.Errorf("want no findings, got %+v", f)
		}
	})

	t.Run("learned facts are citable and can supply skills", func(t *testing.T) {
		lp := *p
		lp.LearnedFacts = []profile.LearnedFact{{ID: "q-docker", Question: "Containers?", Answer: "Yes, I run everything in Docker."}}
		d := goodDraft(t)
		d.Experience[0].Bullets[0].Sources = append(d.Experience[0].Bullets[0].Sources, "q-docker")
		d.Skills[0].Items = append(d.Skills[0].Items, "Docker")
		if f := newCatalog(&lp, nil).checkResume(d); len(f.problems) > 0 {
			t.Errorf("want no problems, got %v", f.problems)
		}
		// A skill only named in a question, not an answer, is still rejected.
		d.Skills[0].Items = append(d.Skills[0].Items, "Containers")
		if f := newCatalog(&lp, nil).checkResume(d); len(f.problems) != 1 {
			t.Errorf("want 1 problem, got %v", f.problems)
		}
	})

	t.Run("answers and skills are citable", func(t *testing.T) {
		d := goodDraft(t)
		d.Experience[0].Bullets[0].Sources = append(d.Experience[0].Bullets[0].Sources, "q-team-size")
		d.Skills[0].Items = []string{"claude api", "AWS Lambda"} // case-insensitive; Lambda comes from a highlight
		if f := newCatalog(p, answers).checkResume(d); len(f.problems) > 0 {
			t.Errorf("want no problems, got %v", f.problems)
		}
	})
}

func TestCheckLetter(t *testing.T) {
	p := loadProfile(t)
	if f := newCatalog(p, nil).checkLetter(goodLetter(t), testPosting); len(f.problems)+len(f.warnings) > 0 {
		t.Fatalf("the good letter should pass cleanly, got %+v", f)
	}

	tests := []struct {
		name    string
		mutate  func(cl *CoverLetter)
		problem bool
		want    string
	}{
		{"unknown source", func(cl *CoverLetter) { cl.Paragraphs[1].Sources = []string{"made-up"} }, true, `cites "made-up"`},
		{"avoided word", func(cl *CoverLetter) { cl.Paragraphs[3].Text = "I'm passionate about this." }, true, `avoided word "passionate"`},
		{"no paragraphs", func(cl *CoverLetter) { cl.Paragraphs = nil }, true, "no paragraphs"},
		{"invented number", func(cl *CoverLetter) {
			cl.Paragraphs[1].Text = strings.Replace(cl.Paragraphs[1].Text, "1,200", "5,000", 1)
		}, false, `mentions "5,000"`},
		{"invented name", func(cl *CoverLetter) {
			cl.Paragraphs[2].Text = strings.Replace(cl.Paragraphs[2].Text, "document extraction", "Salesforce extraction", 1)
		}, false, `mentions "Salesforce"`},
		{"uncited number", func(cl *CoverLetter) { cl.Paragraphs[3].Text = "I cut costs 40% somewhere else." }, false, `mentions "40%"`},
		{"stock opening", func(cl *CoverLetter) {
			cl.Paragraphs[0].Text = "I am writing to apply for the AI Solutions Engineer role. " + cl.Paragraphs[0].Text
		}, false, "opens with a stock line"},
		{"restated posting", func(cl *CoverLetter) {
			cl.Paragraphs[0].Text = "Your posting describes the work I want to do. " + cl.Paragraphs[0].Text
		}, false, "opens with a stock line"},
		{"too short", func(cl *CoverLetter) { cl.Paragraphs = cl.Paragraphs[:2] }, false, "aim for 250 to 350"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := goodLetter(t)
			tt.mutate(cl)
			f := newCatalog(p, nil).checkLetter(cl, testPosting)
			got := f.warnings
			if tt.problem {
				got = f.problems
			}
			for _, s := range got {
				if strings.Contains(s, tt.want) {
					return
				}
			}
			t.Errorf("no finding containing %q; got %+v", tt.want, f)
		})
	}
}

func TestShortLocation(t *testing.T) {
	for in, want := range map[string]string{
		"Post Falls, Idaho, United States": "Post Falls, ID",
		"Coeur d'Alene, ID":                "Coeur d'Alene, ID",
		"Sacramento, California, USA":      "Sacramento, CA",
		"Remote":                           "Remote",
		"London, United Kingdom":           "London, United Kingdom",
	} {
		if got := ShortLocation(in); got != want {
			t.Errorf("ShortLocation(%q) = %q, want %q", in, got, want)
		}
	}
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

func TestAnalysisName(t *testing.T) {
	for _, tt := range []struct{ company, role, want string }{
		{"Northwind Freight", "AI Solutions Engineer", "Northwind Freight - AI Solutions Engineer"},
		{"Northwind Freight", "", "Northwind Freight"},
		{"", "AI Solutions Engineer", "AI Solutions Engineer"},
		{"", "", "Application"},
	} {
		a := Analysis{Company: tt.company, Role: tt.role}
		if got := a.Name(); got != tt.want {
			t.Errorf("Name() = %q, want %q", got, tt.want)
		}
	}
}

func TestRenderLayout(t *testing.T) {
	p := loadProfile(t)
	d := goodDraft(t)
	d.Experience[1] = RoleDraft{ID: "brightpath", Brief: true}
	d.Projects[0].Text = "rezgen: Go CLI that tailors resumes."
	resume := RenderResume(p, d)
	proj, exp, earlier := strings.Index(resume, "## Projects"), strings.Index(resume, "## Experience"), strings.Index(resume, "## Earlier experience")
	if proj < 0 || exp < 0 || earlier < 0 || !(proj < exp && exp < earlier) {
		t.Errorf("want Projects, then Experience, then Earlier experience:\n%s", resume)
	}
	if !strings.Contains(resume, "- Software Developer, BrightPath Health (Jun 2018 - Feb 2022)\n") {
		t.Errorf("brief role not on one line:\n%s", resume)
	}
	if !strings.Contains(resume, "**: Go CLI that tailors resumes.") {
		t.Errorf("project name not stripped:\n%s", resume)
	}

	p.Preferences.ProjectsPosition = "after"
	resume = RenderResume(p, d)
	if strings.Index(resume, "## Projects") < strings.Index(resume, "## Experience") {
		t.Error("projects_position after not honored")
	}
}
