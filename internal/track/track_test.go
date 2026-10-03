package track

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	oct1 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	oct3 = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
)

func TestCreateAndLoad(t *testing.T) {
	root := filepath.Join(t.TempDir(), "applications")
	a, err := Create(root, "Northwind - AI Engineer", "We are hiring.", "https://example.com/job", oct1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Folder() != "2026-10-01-northwind-ai-engineer" || a.Status != NotStarted {
		t.Errorf("created %s, %s", a.Folder(), a.Status)
	}
	text, err := a.Posting()
	if err != nil || text != "We are hiring." {
		t.Errorf("posting %q, %v", text, err)
	}
	raw, _ := os.ReadFile(filepath.Join(a.Dir, "posting.md"))
	if !strings.HasPrefix(string(raw), "Source: https://example.com/job\n\n") {
		t.Errorf("posting.md doesn't record the URL:\n%s", raw)
	}

	// A second application with the same name gets its own folder.
	b, err := Create(root, "Northwind - AI Engineer", "x", "", oct1)
	if err != nil || b.Folder() != "2026-10-01-northwind-ai-engineer-2" {
		t.Errorf("duplicate: %v, %v", b, err)
	}
	if _, err := Create(root, "  ", "x", "", oct1); err == nil {
		t.Error("empty name should fail")
	}
}

func TestGenerateArchiveDelete(t *testing.T) {
	root := t.TempDir()
	a, _ := Create(root, "Acme", "posting", "", oct1)
	os.WriteFile(filepath.Join(a.Dir, "resume.md"), []byte("# v1"), 0o644)
	os.WriteFile(filepath.Join(a.Dir, "resume.pdf"), []byte("%PDF"), 0o644)
	if err := a.MarkGenerated("Acme Corp", "Engineer", oct3); err != nil {
		t.Fatal(err)
	}
	got, err := Load(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Generated || got.Generated != "2026-10-03" || got.Role != "Engineer" || got.Name != "Acme" {
		t.Errorf("after generate: %+v", got.Record)
	}

	dest, err := a.Archive()
	if err != nil || filepath.Base(dest) != "v1" {
		t.Fatalf("archive: %s, %v", dest, err)
	}
	if a.Has("resume.md") || !a.Has(filepath.Join("v1", "resume.md")) || !a.Has("posting.md") {
		t.Error("archive should move documents and keep the posting")
	}
	if got, _ := Load(a.Dir); got.Status != NotStarted {
		t.Errorf("status after archive: %s", got.Status)
	}
	os.WriteFile(filepath.Join(a.Dir, "resume.md"), []byte("# v2"), 0o644)
	if dest, _ := a.Archive(); filepath.Base(dest) != "v2" {
		t.Errorf("second archive went to %s", dest)
	}
	if dest, _ := a.Archive(); dest != "" {
		t.Errorf("nothing to archive, got %s", dest)
	}

	if err := a.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Dir); err == nil {
		t.Error("folder still exists")
	}
}

func TestLoadOlderFolders(t *testing.T) {
	root := t.TempDir()
	// Made before application.json existed.
	old := filepath.Join(root, "2026-09-28-acme-engineer")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "analysis.json"), []byte(`{"company": "Acme", "role": "Engineer"}`), 0o644)
	os.WriteFile(filepath.Join(old, "resume.md"), []byte("x"), 0o644)
	// Made by the version that tracked applied/interviewing.
	mid := filepath.Join(root, "2026-10-02-northwind")
	os.MkdirAll(mid, 0o755)
	os.WriteFile(filepath.Join(mid, FileName), []byte(`{"company": "Northwind", "role": "AI Engineer", "created": "2026-10-02", "status": "applied", "history": []}`), 0o644)

	a, err := Load(old)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Acme - Engineer" || a.Status != Generated || a.Created != "2026-09-28" {
		t.Errorf("old folder: %+v", a.Record)
	}
	b, err := Load(mid)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "Northwind - AI Engineer" || b.Status != NotStarted {
		t.Errorf("mid folder: %+v", b.Record)
	}

	os.MkdirAll(filepath.Join(root, "not-an-app"), 0o755)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	apps, err := List(root)
	if err != nil || len(apps) != 2 || apps[0].Name != "Northwind - AI Engineer" {
		t.Errorf("list: %v, %v", apps, err)
	}
	if apps, err := List(filepath.Join(root, "missing")); err != nil || len(apps) != 0 {
		t.Errorf("missing root: %v, %v", apps, err)
	}
}

func TestAgeAndSlug(t *testing.T) {
	if got := Age("2026-10-01", oct3.Add(15*time.Hour)); got != "2026-10-01 (2 days)" {
		t.Errorf("Age = %q", got)
	}
	if got := Slug("Northwind Freight: AI Solutions Engineer (Remote)"); got != "northwind-freight-ai-solutions-engineer-remote" {
		t.Errorf("Slug = %q", got)
	}
}
