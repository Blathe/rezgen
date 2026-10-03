// Package store keeps rezgen's applications. Each application's state (its
// name, the posting, the analysis and the generated draft) lives in one JSON
// file in a hidden .state folder, and its exported documents (resume.pdf and
// cover-letter.pdf, nothing else) in a folder of the same name:
//
//	<data>/applications/2026-10-03-142530-acme-engineer/resume.pdf
//	<data>/.state/2026-10-03-142530-acme-engineer.json
//
// Keeping state out of the document folders means those folders hold only
// what gets sent to employers, and one file per application means a bad
// write or a hand-editing mistake can only affect one application.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/pdf"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
)

// Version is the state file format this build writes.
const Version = 1

// Status is whether an application's documents have been generated.
type Status string

const (
	NotStarted Status = "not started"
	Generated  Status = "generated"
)

// Application is one job application's state.
type Application struct {
	Version   int        `json:"version"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Company   string     `json:"company,omitempty"`
	Role      string     `json:"role,omitempty"`
	Source    string     `json:"source,omitempty"` // posting URL or file, empty if pasted
	Created   time.Time  `json:"created"`
	Generated *time.Time `json:"generated,omitempty"`

	Posting  string           `json:"posting"`
	Analysis *tailor.Analysis `json:"analysis,omitempty"`
	Answers  []tailor.Answer  `json:"answers,omitempty"`
	Draft    *tailor.Draft    `json:"draft,omitempty"`
	// Rejected holds the last draft that failed the source checks, so the
	// app can show why.
	Rejected *Rejected `json:"rejected,omitempty"`

	// Hand edits made in the app. When set, they replace the documents
	// rendered from Draft until the next generation.
	ResumeEdit      string `json:"resume_edit,omitempty"`
	CoverLetterEdit string `json:"cover_letter_edit,omitempty"`
}

// Rejected is a draft that failed the source checks and why.
type Rejected struct {
	Problems []string      `json:"problems"`
	Draft    *tailor.Draft `json:"draft,omitempty"`
}

// Status reports whether documents have been generated.
func (a *Application) Status() Status {
	if a.Draft != nil || a.ResumeEdit != "" {
		return Generated
	}
	return NotStarted
}

// Store reads and writes applications under a data folder.
type Store struct {
	docs  string // one folder of PDFs per application
	state string // one JSON file per application
}

// New returns a store whose document folders live in appsDir and whose
// state lives in a .state folder beside it.
func New(appsDir string) *Store {
	appsDir = filepath.Clean(appsDir)
	return &Store{docs: appsDir, state: filepath.Join(filepath.Dir(appsDir), ".state")}
}

// DocsDir is the folder holding a's PDFs.
func (s *Store) DocsDir(a *Application) string { return filepath.Join(s.docs, a.ID) }

// ResumePDF and CoverLetterPDF are a's exported documents.
func (s *Store) ResumePDF(a *Application) string { return filepath.Join(s.DocsDir(a), "resume.pdf") }
func (s *Store) CoverLetterPDF(a *Application) string {
	return filepath.Join(s.DocsDir(a), "cover-letter.pdf")
}

func (s *Store) statePath(id string) string { return filepath.Join(s.state, id+".json") }

// Create saves a new application for a posting and returns it. Its ID is
// the creation time to the second plus the name, e.g.
// 2026-10-03-142530-acme-engineer, with -2, -3, ... if that's taken.
func (s *Store) Create(name, posting, source string, now time.Time) (*Application, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("give the application a name")
	}
	if strings.TrimSpace(posting) == "" {
		return nil, errors.New("the posting is empty")
	}
	if err := os.MkdirAll(s.state, 0o755); err != nil {
		return nil, err
	}
	base := now.Format("2006-01-02-150405") + "-" + orDefault(Slug(name), "application")
	a := &Application{
		Version: Version, Name: name, Source: source,
		Created: now.Truncate(time.Second), Posting: strings.TrimSpace(posting),
	}
	for n := 1; ; n++ {
		a.ID = base
		if n > 1 {
			a.ID = fmt.Sprintf("%s-%d", base, n)
		}
		// Claim the ID atomically so two saves can't pick the same one.
		f, err := os.OpenFile(s.statePath(a.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		f.Close()
		return a, s.Save(a)
	}
}

// Save writes a's state. The file is written to a temporary name and then
// renamed, so a crash can't leave it half-written.
func (s *Store) Save(a *Application) error {
	a.Version = Version
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.state, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.state, "."+a.ID+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.statePath(a.ID))
}

// Load reads one application.
func (s *Store) Load(id string) (*Application, error) {
	data, err := os.ReadFile(s.statePath(id))
	if err != nil {
		return nil, err
	}
	var a Application
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("%s: %w", s.statePath(id), err)
	}
	if a.Version > Version {
		return nil, fmt.Errorf("%s was written by a newer rezgen; update rezgen to open it", s.statePath(id))
	}
	if a.ID == "" {
		a.ID = id
	}
	return &a, nil
}

// List returns every application, newest first. A state file that can't be
// read is reported in the error but doesn't hide the others.
func (s *Store) List() ([]*Application, error) {
	entries, err := os.ReadDir(s.state)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var apps []*Application
	var bad []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		a, err := s.Load(strings.TrimSuffix(name, ".json"))
		if err != nil {
			bad = append(bad, err.Error())
			continue
		}
		apps = append(apps, a)
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if !apps[i].Created.Equal(apps[j].Created) {
			return apps[i].Created.After(apps[j].Created)
		}
		return apps[i].ID > apps[j].ID
	})
	if len(bad) > 0 {
		return apps, fmt.Errorf("couldn't read %d application(s): %s", len(bad), strings.Join(bad, "; "))
	}
	return apps, nil
}

// Delete removes a's state and its documents.
func (s *Store) Delete(a *Application) error {
	if err := os.RemoveAll(s.DocsDir(a)); err != nil {
		return err
	}
	if err := os.Remove(s.statePath(a.ID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Results is what one generation produced. Draft is nil when the draft
// failed the source checks; Rejected then says why.
type Results struct {
	Analysis *tailor.Analysis
	Answers  []tailor.Answer
	Draft    *tailor.Draft
	Rejected *tailor.DraftError
}

// SetResults records a generation. A successful one replaces the previous
// draft and clears any hand edits; a rejected one keeps the previous
// documents and records why the new draft failed.
func (s *Store) SetResults(a *Application, r Results, now time.Time) error {
	a.Analysis, a.Answers = r.Analysis, r.Answers
	if r.Analysis != nil {
		if r.Analysis.Company != "" {
			a.Company = r.Analysis.Company
		}
		if r.Analysis.Role != "" {
			a.Role = r.Analysis.Role
		}
	}
	if r.Draft == nil {
		if r.Rejected != nil {
			a.Rejected = &Rejected{Problems: r.Rejected.Problems, Draft: r.Rejected.Draft}
		}
		return s.Save(a)
	}
	t := now.Truncate(time.Second)
	a.Draft, a.Generated, a.Rejected = r.Draft, &t, nil
	a.ResumeEdit, a.CoverLetterEdit = "", ""
	return s.Save(a)
}

// ResumeMarkdown is the resume as Markdown: the hand edit if there is one,
// otherwise rendered from the draft.
func (a *Application) ResumeMarkdown(p *profile.Profile) string {
	if a.ResumeEdit != "" {
		return a.ResumeEdit
	}
	if a.Draft == nil {
		return ""
	}
	return tailor.RenderResume(p, a.Draft)
}

// CoverLetterMarkdown is the cover letter as Markdown, dated the day it
// was generated.
func (a *Application) CoverLetterMarkdown(p *profile.Profile) string {
	if a.CoverLetterEdit != "" {
		return a.CoverLetterEdit
	}
	if a.Draft == nil {
		return ""
	}
	on := a.Created
	if a.Generated != nil {
		on = *a.Generated
	}
	return tailor.RenderCoverLetter(p, a.Draft, on)
}

// Export writes resume.pdf and cover-letter.pdf for a and returns the
// resume's page count. They are the only files in the folder.
func (s *Store) Export(a *Application, p *profile.Profile) (int, error) {
	resume := a.ResumeMarkdown(p)
	if resume == "" {
		return 0, errors.New("nothing to export yet; generate the documents first")
	}
	if err := os.MkdirAll(s.DocsDir(a), 0o755); err != nil {
		return 0, err
	}
	pages, err := writePDF(s.ResumePDF(a), resume)
	if err != nil {
		return 0, err
	}
	if _, err := writePDF(s.CoverLetterPDF(a), a.CoverLetterMarkdown(p)); err != nil {
		return 0, err
	}
	return pages, nil
}

func writePDF(path, md string) (int, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".*.pdf.tmp")
	if err != nil {
		return 0, err
	}
	defer os.Remove(f.Name())
	pages, err := pdf.Render([]byte(md), f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	return pages, os.Rename(f.Name(), path)
}

// HasPDFs reports whether both documents have been exported.
func (s *Store) HasPDFs(a *Application) bool {
	return isFile(s.ResumePDF(a)) && isFile(s.CoverLetterPDF(a))
}

// Age formats a time's date with how long before now it was, e.g.
// "2026-10-01 (2 days)".
func Age(t, now time.Time) string {
	date := t.Format("2006-01-02")
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC) }
	switch days := int(day(now).Sub(day(t)).Hours() / 24); {
	case days <= 0:
		return date + " (today)"
	case days == 1:
		return date + " (1 day)"
	default:
		return fmt.Sprintf("%s (%d days)", date, days)
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug lowercases s and joins its words with hyphens, keeping at most 50
// characters.
func Slug(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
