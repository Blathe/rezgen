package tailor

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/profile"
)

//go:embed schemas/profile_draft.json
var profileDraftSchemaJSON []byte

var profileDraftSchema = mustSchema(profileDraftSchemaJSON)

const profileDraftSystem = `You turn a candidate's resume, LinkedIn profile or notes into a structured profile that a resume-tailoring tool will draw every future application from.

Record only facts that are in the text. Never invent an employer, title, date, skill, metric or credential. Keep numbers exactly as written. Where something is missing or unclear, leave it empty and say so in notes.

- experience: every role, most recent first. Dates as YYYY-MM; if only a year is given, use YYYY-01 and add a note asking for the month. A current role has an empty end.
- highlights: one factual accomplishment or responsibility each, written as a plain sentence starting with a verb. Split bullets that contain two separate accomplishments. Put numbers in metrics as well as the text, list the tools and skills used, and add a few lowercase tags (e.g. automation, leadership, llm).
- ids: lowercase-kebab-case, unique across the whole profile, e.g. "acme" for a role and "acme-ticket-triage" for one of its highlights.
- headline_variants: 2 or 3 job titles the candidate could credibly put at the top of a resume, based on the text.
- summary_facts: 3 to 6 short factual statements a summary could be built from.
- skills: group the skills the text mentions under short headings.
- cover_letter_stories: only if the text contains a story about motivation or a notable win; otherwise empty.
- notes: the most useful things the candidate could add, such as metrics for strong highlights or missing dates and links.`

// DraftProfile builds a profile from pasted resume or LinkedIn text, and
// returns it with suggestions for what to add. The profile passes
// validation; if the first attempt doesn't, the model is asked once more
// with the problems listed.
func DraftProfile(ctx context.Context, c llm.Client, text string) (*profile.Profile, []string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil, errors.New("paste your resume or LinkedIn profile text first")
	}
	prompt := "Build a profile from this text.\n\n<text>\n" + text + "\n</text>"
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		p := prompt
		if lastErr != nil {
			p += "\n\nYour previous attempt failed validation:\n" + lastErr.Error() + "\nFix these problems."
		}
		out, err := c.JSON(ctx, llm.Request{System: profileDraftSystem, Prompt: p, Schema: profileDraftSchema, MaxTokens: 16000})
		if err != nil {
			return nil, nil, fmt.Errorf("draft profile: %w", err)
		}
		prof, notes, err := convertDraft(out)
		if err == nil {
			return prof, notes, nil
		}
		if errors.Is(err, errNoEmail) {
			return nil, nil, err // retrying won't find it
		}
		lastErr = err
	}
	return nil, nil, fmt.Errorf("draft profile: %w", lastErr)
}

// profileDraft mirrors schemas/profile_draft.json, which avoids the maps and
// nullable fields that structured output schemas can't express.
type profileDraft struct {
	Contact struct {
		Name, Email, Phone, Location string
		Links                        []struct{ Label, URL string }
	}
	HeadlineVariants []string `json:"headline_variants"`
	SummaryFacts     []string `json:"summary_facts"`
	Experience       []struct {
		ID, Company, Title, Location, Start, End, Context string
		Highlights                                        []profile.Highlight
	}
	Projects []profile.Project
	Skills   []struct {
		Group string
		Items []string
	}
	Education          []profile.Education
	Certifications     []profile.Certification
	CoverLetterStories []profile.Story `json:"cover_letter_stories"`
	Notes              []string
}

var errNoEmail = errors.New("couldn't find an email address in that text; add a line with your email and try again")

// convertDraft turns the model's output into a profile and validates it.
func convertDraft(out []byte) (*profile.Profile, []string, error) {
	var d profileDraft
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if strings.TrimSpace(d.Contact.Email) == "" {
		return nil, nil, errNoEmail
	}
	p := profile.Profile{
		SchemaVersion: profile.SchemaVersion,
		Contact: profile.Contact{
			Name: d.Contact.Name, Email: d.Contact.Email, Phone: d.Contact.Phone, Location: d.Contact.Location,
		},
		HeadlineVariants:   nonEmpty(d.HeadlineVariants),
		SummaryFacts:       nonEmpty(d.SummaryFacts),
		Projects:           d.Projects,
		Education:          d.Education,
		Certifications:     d.Certifications,
		CoverLetterStories: d.CoverLetterStories,
		Preferences: profile.Preferences{
			Tone: "direct, concrete", MaxPages: 1,
			AvoidWords: []string{"passionate", "synergy", "rockstar", "ninja", "guru"},
		},
	}
	if len(d.Contact.Links) > 0 {
		p.Contact.Links = map[string]string{}
		for _, l := range d.Contact.Links {
			if l.URL != "" {
				p.Contact.Links[strings.ToLower(strings.TrimSpace(l.Label))] = l.URL
			}
		}
	}
	for _, e := range d.Experience {
		x := profile.Experience{
			ID: e.ID, Company: e.Company, Title: e.Title, Location: e.Location,
			Start: e.Start, Context: e.Context, Highlights: e.Highlights,
		}
		if e.End != "" {
			end := e.End
			x.End = &end
		}
		p.Experience = append(p.Experience, x)
	}
	if len(d.Skills) > 0 {
		p.Skills = map[string][]string{}
		for _, g := range d.Skills {
			if items := nonEmpty(g.Items); len(items) > 0 {
				p.Skills[strings.ToLower(strings.TrimSpace(g.Group))] = items
			}
		}
	}

	// Round-trip through JSON so the profile gets the same validation as a
	// hand-written file.
	data, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	valid, err := profile.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	return valid, nonEmpty(d.Notes), nil
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
