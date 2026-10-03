package tailor

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Blathe/rezgen/internal/profile"
)

// catalog knows every ID a draft may cite and which role each highlight
// belongs to.
type catalog struct {
	profile *profile.Profile
	// owner maps every citable ID to the experience id it belongs to, or ""
	// for IDs that aren't tied to one role (projects, stories, facts, answers).
	owner    map[string]string
	roles    map[string]bool
	projects map[string]bool
	skills   map[string]bool
}

func newCatalog(p *profile.Profile, answers []Answer) *catalog {
	c := &catalog{
		profile:  p,
		owner:    map[string]string{},
		roles:    map[string]bool{},
		projects: map[string]bool{},
		skills:   map[string]bool{},
	}
	addSkills := func(ss []string) {
		for _, s := range ss {
			c.skills[normSkill(s)] = true
		}
	}
	for _, e := range p.Experience {
		c.roles[e.ID] = true
		c.owner[e.ID] = e.ID
		for _, h := range e.Highlights {
			c.owner[h.ID] = e.ID
			addSkills(h.Skills)
		}
	}
	for _, pr := range p.Projects {
		c.projects[pr.ID] = true
		c.owner[pr.ID] = ""
		addSkills(pr.Skills)
	}
	for _, s := range p.CoverLetterStories {
		c.owner[s.ID] = ""
	}
	for i := range p.SummaryFacts {
		c.owner[fmt.Sprintf("fact-%d", i+1)] = ""
	}
	for _, a := range answers {
		c.owner[a.QuestionID] = ""
	}
	for _, group := range p.Skills {
		addSkills(group)
	}
	return c
}

func normSkill(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// check returns every way d strays from the profile. An empty result means
// every line is cited, every citation exists, and nothing uses an avoided
// word.
func (c *catalog) check(d *Draft) []string {
	var probs []string
	add := func(format string, args ...any) { probs = append(probs, fmt.Sprintf(format, args...)) }

	cite := func(where string, l Line, role string) {
		if strings.TrimSpace(l.Text) == "" {
			add("%s is empty", where)
		}
		if len(l.Sources) == 0 {
			add("%s cites no sources", where)
		}
		for _, id := range l.Sources {
			owner, ok := c.owner[id]
			switch {
			case !ok:
				add("%s cites %q, which is not an ID in the profile or answers", where, id)
			case role != "" && owner != "" && owner != role:
				add("%s is under role %q but cites %q from role %q", where, role, id, owner)
			}
		}
	}

	cite("summary", d.Summary, "")
	seenRoles := map[string]bool{}
	for i, r := range d.Experience {
		where := fmt.Sprintf("experience[%d]", i)
		if !c.roles[r.ID] {
			add("%s has id %q, which is not a role in the profile", where, r.ID)
			continue
		}
		if seenRoles[r.ID] {
			add("%s repeats role %q", where, r.ID)
		}
		seenRoles[r.ID] = true
		if len(r.Bullets) == 0 {
			add("role %q has no bullets", r.ID)
		}
		for j, b := range r.Bullets {
			cite(fmt.Sprintf("role %q bullet %d", r.ID, j+1), b, r.ID)
		}
	}
	for _, e := range c.profile.Experience {
		if !seenRoles[e.ID] {
			add("role %q is missing; include every role", e.ID)
		}
	}
	for i, p := range d.Projects {
		if !c.projects[p.ID] {
			add("projects[%d] has id %q, which is not a project in the profile", i, p.ID)
		}
	}
	for _, g := range d.Skills {
		for _, s := range g.Items {
			if !c.skills[normSkill(s)] {
				add("skill %q is not in the profile", s)
			}
		}
	}
	for i, p := range d.CoverLetter.Paragraphs {
		cite(fmt.Sprintf("cover letter paragraph %d", i+1), p, "")
	}
	if len(d.CoverLetter.Paragraphs) == 0 {
		add("cover letter has no paragraphs")
	}

	for _, w := range c.profile.Preferences.AvoidWords {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
		for _, t := range d.texts() {
			if re.MatchString(t) {
				add("uses avoided word %q", w)
				break
			}
		}
	}
	return probs
}

// texts returns every piece of prose in the draft.
func (d *Draft) texts() []string {
	out := []string{d.Headline, d.Summary.Text, d.CoverLetter.Greeting, d.CoverLetter.Closing}
	for _, r := range d.Experience {
		for _, b := range r.Bullets {
			out = append(out, b.Text)
		}
	}
	for _, p := range d.Projects {
		out = append(out, p.Text)
	}
	for _, p := range d.CoverLetter.Paragraphs {
		out = append(out, p.Text)
	}
	return out
}
