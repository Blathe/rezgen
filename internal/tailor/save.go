package tailor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/track"
)

// Results is what one generation produced. Draft is nil when the draft
// failed the source checks.
type Results struct {
	Analysis *Analysis
	Answers  []Answer
	Draft    *Draft
}

// WriteResults saves a generation into the application's folder: the
// analysis and answers, and, if there is a draft, resume.md,
// cover-letter.md, sources.md and draft.json. With a draft the application
// is marked generated, taking its company and role from the analysis.
func WriteResults(app *track.App, p *profile.Profile, r Results, on time.Time) error {
	files := map[string]string{}
	if r.Analysis != nil {
		files["analysis.json"] = toJSON(r.Analysis)
	}
	if len(r.Answers) > 0 {
		files["answers.json"] = toJSON(r.Answers)
	}
	if r.Draft != nil {
		files["draft.json"] = toJSON(r.Draft)
		files["resume.md"] = RenderResume(p, r.Draft)
		files["cover-letter.md"] = RenderCoverLetter(p, r.Draft, on)
		files["sources.md"] = RenderSources(r.Draft)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(app.Dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	if r.Draft == nil {
		return nil
	}
	var company, role string
	if r.Analysis != nil {
		company, role = r.Analysis.Company, r.Analysis.Role
	}
	return app.MarkGenerated(company, role, on)
}

// SaveRejected writes a draft that failed the source checks, with the
// problems found, to dir/draft-rejected.json for inspection.
func SaveRejected(dir string, de *DraftError) error {
	data := toJSON(struct {
		Problems []string `json:"problems"`
		Draft    *Draft   `json:"draft"`
	}{de.Problems, de.Draft})
	return os.WriteFile(filepath.Join(dir, "draft-rejected.json"), []byte(data), 0o644)
}

// Name suggests an application name from an analysis: "Company - Role".
func (a *Analysis) Name() string {
	switch {
	case a.Company != "" && a.Role != "":
		return a.Company + " - " + a.Role
	case a.Company != "":
		return a.Company
	case a.Role != "":
		return a.Role
	}
	return "Application"
}

func toJSON(v any) string {
	data, _ := json.MarshalIndent(v, "", "  ")
	return string(data) + "\n"
}
