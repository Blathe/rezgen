package tailor

import (
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
)

// LearnAnswers saves answers to the learned_facts of the profile file at
// profilePath, noting the posting they came from, so later applications
// don't ask the same questions.
func LearnAnswers(profilePath string, a *Analysis, answers []Answer, on time.Time) error {
	var parts []string
	for _, s := range []string{a.Company, a.Role} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	facts := make([]profile.LearnedFact, len(answers))
	for i, an := range answers {
		facts[i] = profile.LearnedFact{
			ID:       an.QuestionID,
			Question: an.Question,
			Answer:   an.Text,
			Learned:  on.Format("2006-01-02"),
			Context:  strings.Join(parts, ", "),
		}
	}
	_, err := profile.AddLearnedFacts(profilePath, facts)
	return err
}
