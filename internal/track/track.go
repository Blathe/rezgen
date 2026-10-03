// Package track records where each application stands. Every application
// folder holds an application.json with its current status and the history
// of changes, so the folders themselves are the tracker: no database, and
// deleting a folder removes the application.
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

// Status is where an application stands.
type Status string

const (
	Draft        Status = "draft"
	Applied      Status = "applied"
	Interviewing Status = "interviewing"
	Offer        Status = "offer"
	Rejected     Status = "rejected"
	Withdrawn    Status = "withdrawn"
)

// Statuses lists every status in pipeline order.
var Statuses = []Status{Draft, Applied, Interviewing, Offer, Rejected, Withdrawn}

var aliases = map[string]Status{
	"apply": Applied, "interview": Interviewing, "interviews": Interviewing,
	"reject": Rejected, "withdraw": Withdrawn, "offered": Offer,
}

// ParseStatus accepts a status name or a common variant ("interview",
// "reject"), case-insensitively.
func ParseStatus(s string) (Status, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, st := range Statuses {
		if s == string(st) {
			return st, nil
		}
	}
	if st, ok := aliases[s]; ok {
		return st, nil
	}
	names := make([]string, len(Statuses))
	for i, st := range Statuses {
		names[i] = string(st)
	}
	return "", fmt.Errorf("unknown status %q (use one of: %s)", s, strings.Join(names, ", "))
}

// Event is one status change.
type Event struct {
	Status Status `json:"status"`
	Date   string `json:"date"` // YYYY-MM-DD
	Note   string `json:"note,omitempty"`
}

// Record is the contents of application.json.
type Record struct {
	Company string  `json:"company"`
	Role    string  `json:"role"`
	Source  string  `json:"source,omitempty"` // posting URL or file
	Created string  `json:"created"`          // YYYY-MM-DD
	Status  Status  `json:"status"`
	History []Event `json:"history"`
}

// New returns a draft record created on.
func New(company, role, source string, on time.Time) *Record {
	d := on.Format(dateFormat)
	return &Record{
		Company: company, Role: role, Source: source, Created: d,
		Status: Draft, History: []Event{{Status: Draft, Date: d}},
	}
}

const dateFormat = "2006-01-02"

// Set moves the record to status on the given date. Setting the current
// status again just adds a note to the history.
func (r *Record) Set(status Status, note string, on time.Time) {
	r.Status = status
	r.History = append(r.History, Event{Status: status, Date: on.Format(dateFormat), Note: strings.TrimSpace(note)})
}

// Updated returns the date of the last change.
func (r *Record) Updated() string {
	if n := len(r.History); n > 0 {
		return r.History[n-1].Date
	}
	return r.Created
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

// Save writes r to dir/application.json.
func Save(dir string, r *Record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), append(data, '\n'), 0o644)
}

// Load reads dir/application.json. Folders made before tracking existed have
// no such file; for those it builds a draft record from analysis.json and
// the date in the folder name, without writing anything.
func Load(dir string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err == nil {
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
		}
		return &r, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return infer(dir)
}

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func infer(dir string) (*Record, error) {
	var a struct {
		Company string `json:"company"`
		Role    string `json:"role"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "analysis.json"))
	if err != nil {
		return nil, fmt.Errorf("%s is not an application folder (no %s or analysis.json)", dir, FileName)
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, "analysis.json"), err)
	}
	created := time.Now()
	if d := datePrefix.FindString(filepath.Base(dir)); d != "" {
		if t, err := time.Parse(dateFormat, d); err == nil {
			created = t
		}
	}
	return New(a.Company, a.Role, "", created), nil
}

// App is an application folder and its record.
type App struct {
	Name string // folder name, which doubles as the application's ID
	Dir  string
	*Record
}

// List returns every application under root, newest first. Entries that
// aren't application folders are skipped. A missing root means no
// applications yet.
func List(root string) ([]App, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		r, err := Load(dir)
		if err != nil {
			continue
		}
		apps = append(apps, App{Name: e.Name(), Dir: dir, Record: r})
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Created != apps[j].Created {
			return apps[i].Created > apps[j].Created
		}
		return apps[i].Name > apps[j].Name
	})
	return apps, nil
}

// Find returns the application whose folder name is query, or failing that
// the only one whose folder name, company or role contains query
// (case-insensitive). "northwind" is enough when it's unambiguous.
func Find(root, query string) (*App, error) {
	apps, err := List(root)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var matches []App
	for _, a := range apps {
		if a.Name == query {
			return &a, nil
		}
		if strings.Contains(strings.ToLower(a.Name+" "+a.Company+" "+a.Role), q) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no application in %s matches %q", root, query)
	case 1:
		return &matches[0], nil
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = "  " + m.Name
	}
	return nil, fmt.Errorf("%q matches %d applications; be more specific:\n%s", query, len(matches), strings.Join(names, "\n"))
}
