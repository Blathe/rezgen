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

func mkApp(t *testing.T, root, name string, r *Record) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if r != nil {
		if err := Save(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]Status{"Applied": Applied, "interview": Interviewing, " reject ": Rejected, "offer": Offer} {
		if got, err := ParseStatus(in); err != nil || got != want {
			t.Errorf("ParseStatus(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseStatus("ghosted"); err == nil || !strings.Contains(err.Error(), "applied, interviewing") {
		t.Errorf("want helpful error, got %v", err)
	}
}

func TestSetAndRoundTrip(t *testing.T) {
	dir := mkApp(t, t.TempDir(), "2026-10-01-northwind-ai", nil)
	r := New("Northwind", "AI Engineer", "https://example.com/job", oct1)
	r.Set(Applied, " via referral ", oct3)
	if err := Save(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Applied || got.Updated() != "2026-10-03" || len(got.History) != 2 || got.History[1].Note != "via referral" {
		t.Errorf("unexpected record: %+v", got)
	}
}

func TestLoadInfersOldFolders(t *testing.T) {
	dir := mkApp(t, t.TempDir(), "2026-09-28-acme-engineer", nil)
	os.WriteFile(filepath.Join(dir, "analysis.json"), []byte(`{"company": "Acme", "role": "Engineer"}`), 0o644)
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Company != "Acme" || r.Status != Draft || r.Created != "2026-09-28" {
		t.Errorf("unexpected record: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
		t.Error("Load should not write application.json")
	}
}

func TestListAndFind(t *testing.T) {
	root := t.TempDir()
	mkApp(t, root, "2026-10-01-northwind-ai-engineer", New("Northwind", "AI Engineer", "", oct1))
	mkApp(t, root, "2026-10-03-acme-ai-engineer", New("Acme", "AI Engineer", "", oct3))
	mkApp(t, root, "2026-10-03-acme-solutions-engineer", New("Acme", "Solutions Engineer", "", oct3))
	mkApp(t, root, "not-an-app", nil)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)

	apps, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range apps {
		names = append(names, a.Name)
	}
	want := "2026-10-03-acme-solutions-engineer 2026-10-03-acme-ai-engineer 2026-10-01-northwind-ai-engineer"
	if strings.Join(names, " ") != want {
		t.Errorf("order: %v", names)
	}

	if a, err := Find(root, "NORTHWIND"); err != nil || a.Company != "Northwind" {
		t.Errorf("find by company: %v, %v", a, err)
	}
	if a, err := Find(root, "solutions"); err != nil || a.Role != "Solutions Engineer" {
		t.Errorf("find by role: %v, %v", a, err)
	}
	if _, err := Find(root, "acme"); err == nil || !strings.Contains(err.Error(), "matches 2 applications") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := Find(root, "globex"); err == nil {
		t.Error("want no match")
	}

	if apps, err := List(filepath.Join(root, "missing")); err != nil || len(apps) != 0 {
		t.Errorf("missing root: %v, %v", apps, err)
	}
}
