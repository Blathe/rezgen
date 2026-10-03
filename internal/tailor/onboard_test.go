package tailor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Blathe/rezgen/internal/llm/llmtest"
	"github.com/Blathe/rezgen/internal/profile"
)

const draftOK = `{
  "contact": {"name": "Jordan Example", "email": "jordan@example.com", "phone": "", "location": "Denver, CO",
              "links": [{"label": "GitHub", "url": "https://github.com/example"}]},
  "headline_variants": ["AI Solutions Engineer"],
  "summary_facts": ["8 years building internal tools", " "],
  "experience": [
    {"id": "acme", "company": "Acme", "title": "Engineer", "location": "", "start": "2022-03", "end": "", "context": "",
     "highlights": [{"id": "acme-triage", "text": "Built a ticket triage service", "metrics": [], "skills": ["Python"], "tags": ["llm"]}]},
    {"id": "globex", "company": "Globex", "title": "Developer", "location": "Remote", "start": "2018-01", "end": "2022-02", "context": "",
     "highlights": [{"id": "globex-api", "text": "Maintained REST APIs", "metrics": [], "skills": [], "tags": []}]}
  ],
  "projects": [],
  "skills": [{"group": "Languages", "items": ["Python", "Go"]}, {"group": "empty", "items": []}],
  "education": [{"school": "State University", "degree": "B.S.", "field": "", "end": "2016"}],
  "certifications": [],
  "cover_letter_stories": [],
  "notes": ["Add a metric for the triage service"]
}`

func TestDraftProfile(t *testing.T) {
	fake := &llmtest.Fake{Responses: [][]byte{[]byte(draftOK)}}
	p, notes, err := DraftProfile(context.Background(), fake, "Jordan Example, jordan@example.com ...")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Experience[0].Current() || p.Experience[1].Current() {
		t.Error("current role not converted")
	}
	if p.Contact.Links["github"] != "https://github.com/example" || len(p.Skills["languages"]) != 2 || p.Skills["empty"] != nil {
		t.Errorf("links/skills: %+v %+v", p.Contact.Links, p.Skills)
	}
	if len(p.SummaryFacts) != 1 || p.Preferences.MaxPages != 1 {
		t.Errorf("facts/preferences: %+v", p)
	}
	if len(notes) != 1 {
		t.Errorf("notes: %v", notes)
	}
	if !strings.Contains(fake.Requests[0].Prompt, "jordan@example.com") {
		t.Error("text not sent")
	}

	path := filepath.Join(t.TempDir(), "data", "profile.json")
	if err := profile.Save(path, p); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Load(path); err != nil {
		t.Errorf("saved profile doesn't load: %v", err)
	}
	if err := profile.Save(path, p); err == nil {
		t.Error("Save should not overwrite an existing profile")
	}
}

func TestDraftProfileRetriesInvalid(t *testing.T) {
	bad := strings.Replace(draftOK, `"start": "2022-03"`, `"start": "March 2022"`, 1)
	fake := &llmtest.Fake{Responses: [][]byte{[]byte(bad), []byte(draftOK)}}
	if _, _, err := DraftProfile(context.Background(), fake, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.Requests[1].Prompt, "/experience/0/start") {
		t.Errorf("retry prompt doesn't name the problem:\n%s", fake.Requests[1].Prompt)
	}
}

func TestDraftProfileNoEmail(t *testing.T) {
	noEmail := strings.Replace(draftOK, `"jordan@example.com"`, `""`, 1)
	fake := &llmtest.Fake{Responses: [][]byte{[]byte(noEmail)}}
	if _, _, err := DraftProfile(context.Background(), fake, "text"); err == nil || !strings.Contains(err.Error(), "email") {
		t.Errorf("want email error, got %v", err)
	}
	if len(fake.Requests) != 1 {
		t.Error("should not retry a missing email")
	}
}
