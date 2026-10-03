package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
)

var now = time.Date(2026, 10, 3, 14, 25, 30, 0, time.UTC)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	data := t.TempDir()
	return New(filepath.Join(data, "applications")), data
}

func loadProfile(t *testing.T) *profile.Profile {
	t.Helper()
	p, err := profile.Load("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func loadDraft(t *testing.T) *tailor.Draft {
	t.Helper()
	data, err := os.ReadFile("../tailor/testdata/draft.json")
	if err != nil {
		t.Fatal(err)
	}
	var d tailor.Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestCreateListLoad(t *testing.T) {
	s, data := newStore(t)
	a, err := s.Create("Acme - Automation Engineer", "  We are hiring.  ", "https://example.com/job", now)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "2026-10-03-142530-acme-automation-engineer" || a.Status() != NotStarted || a.Posting != "We are hiring." {
		t.Errorf("created %+v", a)
	}
	if _, err := os.Stat(filepath.Join(data, ".state", a.ID+".json")); err != nil {
		t.Errorf("state file not in .state: %v", err)
	}
	if _, err := os.Stat(s.DocsDir(a)); err == nil {
		t.Error("the documents folder should not exist before export")
	}

	// Same name in the same second gets a suffix; a later one sorts first.
	b, _ := s.Create("Acme - Automation Engineer", "x", "", now)
	c, _ := s.Create("Globex", "x", "", now.Add(time.Hour))
	if b.ID != a.ID+"-2" {
		t.Errorf("duplicate ID %q", b.ID)
	}
	apps, err := s.List()
	if err != nil || len(apps) != 3 || apps[0].ID != c.ID {
		t.Fatalf("list: %v %v", apps, err)
	}
	got, err := s.Load(a.ID)
	if err != nil || got.Source != "https://example.com/job" || !got.Created.Equal(now) {
		t.Errorf("load: %+v %v", got, err)
	}

	if _, err := s.Create(" ", "x", "", now); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := s.Create("x", " ", "", now); err == nil {
		t.Error("empty posting should fail")
	}
}

func TestMarkdownSurvivesJSON(t *testing.T) {
	s, _ := newStore(t)
	a, _ := s.Create("Acme", "posting", "", now)
	md := "# Jordan \"JD\" Example\n\n**AI Engineer** | café\n\n- Cut costs 40% \\ with `tools` 🚀\n- Tabs\tand <angle> & ampersands\n"
	a.ResumeEdit = md
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load(a.ID)
	if got.ResumeEdit != md {
		t.Errorf("round trip changed the Markdown:\n%q\n%q", got.ResumeEdit, md)
	}
}

func TestGenerateExportDelete(t *testing.T) {
	s, _ := newStore(t)
	p := loadProfile(t)
	a, _ := s.Create("Northwind", "posting", "", now)

	if _, err := s.Export(a, p); err == nil {
		t.Error("export before generating should fail")
	}

	// A rejected draft records why and stays not started.
	rej := &tailor.DraftError{Problems: []string{"cites fact-99"}}
	if err := s.SetResults(a, Results{Analysis: &tailor.Analysis{Company: "Northwind Freight", Role: "AI Engineer"}, Rejected: rej}, now); err != nil {
		t.Fatal(err)
	}
	if a.Status() != NotStarted || a.Rejected == nil || a.Company != "Northwind Freight" {
		t.Errorf("after rejection: %+v", a)
	}

	a.ResumeEdit = "stale edit"
	if err := s.SetResults(a, Results{Draft: loadDraft(t)}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if a.Status() != Generated || a.Rejected != nil || a.ResumeEdit != "" || a.Generated == nil {
		t.Errorf("after generation: %+v", a)
	}
	if !strings.Contains(a.ResumeMarkdown(p), "# Jordan Example") || !strings.Contains(a.CoverLetterMarkdown(p), "October 3, 2026") {
		t.Error("markdown not rendered from the draft")
	}

	pages, err := s.Export(a, p)
	if err != nil || pages != 1 || !s.HasPDFs(a) {
		t.Fatalf("export: %d pages, %v", pages, err)
	}
	entries, _ := os.ReadDir(s.DocsDir(a))
	if len(entries) != 2 {
		t.Errorf("documents folder should hold only the two PDFs, has %d entries", len(entries))
	}

	// A hand edit replaces the rendered resume.
	a.ResumeEdit = "# Edited\n"
	if a.ResumeMarkdown(p) != "# Edited\n" {
		t.Error("edit not used")
	}

	if err := s.Delete(a); err != nil {
		t.Fatal(err)
	}
	if apps, _ := s.List(); len(apps) != 0 {
		t.Error("state not deleted")
	}
	if _, err := os.Stat(s.DocsDir(a)); err == nil {
		t.Error("documents not deleted")
	}
}

func TestListSkipsBadFiles(t *testing.T) {
	s, data := newStore(t)
	s.Create("Good", "posting", "", now)
	os.WriteFile(filepath.Join(data, ".state", "broken.json"), []byte("{not json"), 0o644)
	os.WriteFile(filepath.Join(data, ".state", "newer.json"), []byte(`{"version": 99, "id": "newer"}`), 0o644)
	apps, err := s.List()
	if len(apps) != 1 || err == nil || !strings.Contains(err.Error(), "2 application(s)") || !strings.Contains(err.Error(), "newer rezgen") {
		t.Errorf("list: %v, %v", apps, err)
	}
}

func TestImportLegacy(t *testing.T) {
	s, data := newStore(t)
	p := loadProfile(t)
	apps := filepath.Join(data, "applications")

	// A folder from the version with application.json and the full set of files.
	old := filepath.Join(apps, "2026-10-02-northwind-freight-ai-solutions-engineer")
	os.MkdirAll(filepath.Join(old, "v1"), 0o755)
	write := func(name, content string) { os.WriteFile(filepath.Join(old, name), []byte(content), 0o644) }
	write("application.json", `{"name": "Northwind - AI", "company": "Northwind Freight", "created": "2026-10-02", "status": "applied", "generated": "2026-10-02"}`)
	write("posting.md", "Source: https://example.com/northwind\n\nWe are hiring an AI engineer.\n")
	write("analysis.json", `{"company": "Northwind Freight", "role": "AI Solutions Engineer"}`)
	draft, _ := os.ReadFile("../tailor/testdata/draft.json")
	write("draft.json", string(draft))
	write("resume.md", "# Jordan Example\n\n- Hand-edited bullet\n")
	write("cover-letter.md", "# Jordan Example\n\nDear team,\n")
	write("sources.md", "x")
	os.WriteFile(filepath.Join(old, "v1", "resume.md"), []byte("old"), 0o644)

	// A folder that's already imported, and one that isn't an application.
	done, _ := s.Create("Already", "posting", "", now)
	os.MkdirAll(filepath.Join(apps, done.ID), 0o755)
	os.MkdirAll(filepath.Join(apps, "random-folder"), 0o755)

	dirs, err := s.FindLegacy()
	if err != nil || len(dirs) != 1 || dirs[0] != old {
		t.Fatalf("FindLegacy: %v %v", dirs, err)
	}

	a, err := s.Import(old, p, true)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != filepath.Base(old) || a.Name != "Northwind - AI" || a.Role != "AI Solutions Engineer" ||
		a.Source != "https://example.com/northwind" || a.Posting != "We are hiring an AI engineer." {
		t.Errorf("imported %+v", a)
	}
	if a.Status() != Generated || a.Draft == nil || !strings.Contains(a.ResumeMarkdown(p), "Hand-edited bullet") {
		t.Errorf("documents not preserved: status %s", a.Status())
	}
	if a.Created.Format("2006-01-02") != "2026-10-02" || a.Generated == nil {
		t.Errorf("dates: %v %v", a.Created, a.Generated)
	}

	entries, _ := os.ReadDir(old)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "cover-letter.pdf,resume.pdf" {
		t.Errorf("after cleanup the folder has %v", names)
	}
	if dirs, _ := s.FindLegacy(); len(dirs) != 0 {
		t.Errorf("still found as legacy: %v", dirs)
	}
}

func TestAge(t *testing.T) {
	if got := Age(now.AddDate(0, 0, -2), now); got != "2026-10-01 (2 days)" {
		t.Errorf("Age = %q", got)
	}
}
