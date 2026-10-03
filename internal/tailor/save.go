package tailor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
)

// Application is everything produced for one posting.
type Application struct {
	Posting string
	// PostingSource is where the posting came from: a URL, file path or
	// "stdin". A URL is recorded at the top of posting.md.
	PostingSource string
	Analysis      *Analysis
	Answers       []Answer
	Draft         *Draft
}

// Save writes app into a new folder under root named
// <date>-<company>-<role> and returns the folder's path. It never overwrites
// an existing folder; a second run on the same day gets a numeric suffix.
func Save(root string, p *profile.Profile, app Application, on time.Time) (string, error) {
	dir, err := newDir(root, folderName(app.Analysis, on))
	if err != nil {
		return "", err
	}
	posting := app.Posting + "\n"
	if strings.HasPrefix(app.PostingSource, "http://") || strings.HasPrefix(app.PostingSource, "https://") {
		posting = "Source: " + app.PostingSource + "\n\n" + posting
	}
	files := map[string]string{"posting.md": posting}
	if app.Analysis != nil {
		files["analysis.json"] = toJSON(app.Analysis)
	}
	if len(app.Answers) > 0 {
		files["answers.json"] = toJSON(app.Answers)
	}
	if app.Draft != nil {
		files["draft.json"] = toJSON(app.Draft)
		files["resume.md"] = RenderResume(p, app.Draft)
		files["cover-letter.md"] = RenderCoverLetter(p, app.Draft, on)
		files["sources.md"] = RenderSources(app.Draft)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

func folderName(a *Analysis, on time.Time) string {
	parts := []string{on.Format("2006-01-02")}
	if a != nil {
		for _, s := range []string{slug(a.Company), slug(a.Role)} {
			if s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "-")
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug lowercases s and joins its words with hyphens, keeping at most 40
// characters.
func slug(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return s
}

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

func toJSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data) + "\n"
}
