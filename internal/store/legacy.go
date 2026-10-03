package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
)

// Older versions kept everything inside each application folder. These are
// the files they wrote.
var legacyFiles = []string{
	"application.json", "posting.md", "analysis.json", "answers.json", "draft.json",
	"draft-rejected.json", "resume.md", "cover-letter.md", "sources.md",
}

// FindLegacy returns application folders made by older versions that
// haven't been imported yet.
func (s *Store) FindLegacy() ([]string, error) {
	entries, err := os.ReadDir(s.docs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || isFile(s.statePath(e.Name())) {
			continue
		}
		dir := filepath.Join(s.docs, e.Name())
		for _, f := range legacyFiles {
			if isFile(filepath.Join(dir, f)) {
				dirs = append(dirs, dir)
				break
			}
		}
	}
	return dirs, nil
}

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// Import reads an older application folder into the store. Its resume and
// cover letter Markdown are kept as hand edits, so the documents stay
// exactly as they were; PDFs are built if missing (when p is given). With
// cleanup, every file except resume.pdf and cover-letter.pdf is then
// removed from the folder, including old v1/, v2/ versions.
func (s *Store) Import(dir string, p *profile.Profile, cleanup bool) (*Application, error) {
	id := filepath.Base(dir)
	a := &Application{Version: Version, ID: id}

	var rec struct {
		Name, Company, Role, Source, Created, Generated string
	}
	readJSON(filepath.Join(dir, "application.json"), &rec)
	a.Name, a.Company, a.Role, a.Source = rec.Name, rec.Company, rec.Role, rec.Source

	var analysis tailor.Analysis
	if readJSON(filepath.Join(dir, "analysis.json"), &analysis) {
		a.Analysis = &analysis
		if a.Company == "" {
			a.Company = analysis.Company
		}
		if a.Role == "" {
			a.Role = analysis.Role
		}
	}
	readJSON(filepath.Join(dir, "answers.json"), &a.Answers)
	var draft tailor.Draft
	if readJSON(filepath.Join(dir, "draft.json"), &draft) {
		a.Draft = &draft
	}

	if data, err := os.ReadFile(filepath.Join(dir, "posting.md")); err == nil {
		text := string(data)
		if src, ok := strings.CutPrefix(text, "Source: "); ok {
			line, rest, _ := strings.Cut(src, "\n")
			if a.Source == "" {
				a.Source = strings.TrimSpace(line)
			}
			text = rest
		}
		a.Posting = strings.TrimSpace(text)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "resume.md")); err == nil {
		a.ResumeEdit = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cover-letter.md")); err == nil {
		a.CoverLetterEdit = string(b)
	}

	a.Created = parseDate(rec.Created, datePrefix.FindString(id), modTime(dir))
	if a.Status() == Generated {
		t := parseDate(rec.Generated, "", modTime(filepath.Join(dir, "resume.md")))
		if t.Before(a.Created) {
			t = a.Created
		}
		a.Generated = &t
	}
	if a.Name == "" {
		a.Name = strings.Trim(a.Company+" - "+a.Role, " -")
	}
	if a.Name == "" {
		a.Name = id
	}

	if err := s.Save(a); err != nil {
		return nil, err
	}
	if p != nil && a.Status() == Generated && !s.HasPDFs(a) {
		if _, err := s.Export(a, p); err != nil {
			return a, err
		}
	}
	if cleanup {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return a, err
		}
		for _, e := range entries {
			if n := e.Name(); n != "resume.pdf" && n != "cover-letter.pdf" {
				if err := os.RemoveAll(filepath.Join(dir, n)); err != nil {
					return a, err
				}
			}
		}
	}
	return a, nil
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, v) == nil
}

// parseDate returns the first of the YYYY-MM-DD dates that parses, or
// fallback.
func parseDate(a, b string, fallback time.Time) time.Time {
	for _, s := range []string{a, b} {
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			return t
		}
	}
	return fallback.Truncate(time.Second)
}

func modTime(p string) time.Time {
	if st, err := os.Stat(p); err == nil {
		return st.ModTime()
	}
	return time.Now()
}
