// Package track manages application folders. Each application is a folder
// under the applications directory holding the saved posting, an
// application.json with its name and status, and, once generated, the
// tailored documents. The folders are the whole database: deleting one
// removes the application.
package track

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
)

// FileName is the status file inside each application folder.
const FileName = "application.json"

// Status is whether an application's documents exist yet.
type Status string

const (
	NotStarted Status = "not started"
	Generated  Status = "generated"
)

// Record is the contents of application.json.
type Record struct {
	Name      string `json:"name"`
	Company   string `json:"company,omitempty"`
	Role      string `json:"role,omitempty"`
	Source    string `json:"source,omitempty"` // posting URL or file, if not pasted
	Created   string `json:"created"`          // YYYY-MM-DD
	Status    Status `json:"status"`
	Generated string `json:"generated,omitempty"` // YYYY-MM-DD of the latest generation
}

const dateFormat = "2006-01-02"

// generatedFiles are the outputs of a generation, moved aside by Archive.
var generatedFiles = []string{
	"resume.pdf", "cover-letter.pdf", "resume.md", "cover-letter.md", "sources.md",
	"analysis.json", "answers.json", "draft.json", "draft-rejected.json",
}

// App is an application folder and its record.
type App struct {
	Dir string
	*Record
}

// Folder returns the folder's name.
func (a *App) Folder() string { return filepath.Base(a.Dir) }

// Has reports whether the application folder contains file.
func (a *App) Has(file string) bool {
	_, err := os.Stat(filepath.Join(a.Dir, file))
	return err == nil
}

// Create makes a new application folder under root for a posting and
// returns it, not started. The folder is named <date>-<name>.
func Create(root, name, posting, source string, on time.Time) (*App, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("give the application a name")
	}
	dir, err := newDir(root, on.Format(dateFormat)+"-"+orDefault(Slug(name), "application"))
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(posting) + "\n"
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		text = "Source: " + source + "\n\n" + text
	}
	if err := os.WriteFile(filepath.Join(dir, "posting.md"), []byte(text), 0o644); err != nil {
		return nil, err
	}
	r := &Record{Name: name, Source: source, Created: on.Format(dateFormat), Status: NotStarted}
	if err := Save(dir, r); err != nil {
		return nil, err
	}
	return &App{Dir: dir, Record: r}, nil
}

// Posting returns the saved posting text, without the Source line.
func (a *App) Posting() (string, error) {
	data, err := os.ReadFile(filepath.Join(a.Dir, "posting.md"))
	if err != nil {
		return "", err
	}
	text := string(data)
	if strings.HasPrefix(text, "Source: ") {
		if _, rest, ok := strings.Cut(text, "\n\n"); ok {
			text = rest
		}
	}
	return strings.TrimSpace(text), nil
}

// MarkGenerated records a generation's results.
func (a *App) MarkGenerated(company, role string, on time.Time) error {
	a.Status, a.Generated = Generated, on.Format(dateFormat)
	if company != "" {
		a.Company = company
	}
	if role != "" {
		a.Role = role
	}
	return Save(a.Dir, a.Record)
}

// Archive moves the current generated documents into the next free v<N>
// subfolder, so regenerating never loses a version. It returns the
// subfolder, or "" if there was nothing to archive.
func (a *App) Archive() (string, error) {
	var present []string
	for _, f := range generatedFiles {
		if a.Has(f) {
			present = append(present, f)
		}
	}
	if len(present) == 0 {
		return "", nil
	}
	var dest string
	for n := 1; ; n++ {
		dest = filepath.Join(a.Dir, fmt.Sprintf("v%d", n))
		if _, err := os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		return "", err
	}
	for _, f := range present {
		if err := os.Rename(filepath.Join(a.Dir, f), filepath.Join(dest, f)); err != nil {
			return dest, err
		}
	}
	return dest, nil
}

// Delete removes the application folder and everything in it.
func (a *App) Delete() error { return os.RemoveAll(a.Dir) }

// Save writes r to dir/application.json.
func Save(dir string, r *Record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), append(data, '\n'), 0o644)
}

// Load reads the application in dir. Its status always reflects the folder:
// generated if resume.md exists, otherwise not started. Folders made by
// older versions (other statuses, or no application.json) load the same
// way, with a name made from the company and role.
func Load(dir string) (*App, error) {
	var r Record
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := readAnalysis(dir, &r); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if r.Created == "" {
		r.Created = datePrefix.FindString(filepath.Base(dir))
	}
	if r.Name == "" {
		r.Name = joinNonEmpty(" - ", r.Company, r.Role)
		if r.Name == "" {
			r.Name = filepath.Base(dir)
		}
	}
	a := &App{Dir: dir, Record: &r}
	if a.Has("resume.md") {
		a.Status = Generated
		if a.Generated == "" {
			a.Generated = a.Created
		}
	} else {
		a.Status = NotStarted
	}
	return a, nil
}

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func readAnalysis(dir string, r *Record) error {
	data, err := os.ReadFile(filepath.Join(dir, "analysis.json"))
	if err != nil {
		return fmt.Errorf("%s is not an application folder", dir)
	}
	var a struct {
		Company string `json:"company"`
		Role    string `json:"role"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("%s: %w", filepath.Join(dir, "analysis.json"), err)
	}
	r.Company, r.Role = a.Company, a.Role
	return nil
}

// List returns every application under root, newest first. Entries that
// aren't application folders are skipped, and a missing root means none.
func List(root string) ([]*App, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var apps []*App
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if a, err := Load(filepath.Join(root, e.Name())); err == nil {
			apps = append(apps, a)
		}
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Created != apps[j].Created {
			return apps[i].Created > apps[j].Created
		}
		return apps[i].Folder() > apps[j].Folder()
	})
	return apps, nil
}

// Age formats a YYYY-MM-DD date with how long before now it was, e.g.
// "2026-10-01 (2 days)".
func Age(date string, now time.Time) string {
	t, err := time.Parse(dateFormat, date)
	if err != nil {
		return date
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch days := int(today.Sub(t).Hours() / 24); {
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

// NewDir creates a folder under root named name, adding -2, -3, ... if it
// already exists, and returns its path.
func NewDir(root, name string) (string, error) { return newDir(root, name) }

func newDir(root, name string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for i := 1; ; i++ {
		dir := filepath.Join(root, name)
		if i > 1 {
			dir = fmt.Sprintf("%s-%d", dir, i)
		}
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
