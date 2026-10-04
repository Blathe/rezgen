package tailor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/Blathe/rezgen/internal/profile"
)

// catalog knows every ID a draft may cite, which role each highlight belongs
// to, and the text behind each ID.
type catalog struct {
	profile *profile.Profile
	// owner maps every citable ID to the experience id it belongs to, or ""
	// for IDs that aren't tied to one role (projects, stories, facts, answers).
	owner    map[string]string
	roles    map[string]bool
	projects map[string]bool
	skills   map[string]bool
	// text holds the lowercased words behind each citable ID, for checking
	// that numbers and names in the cover letter come from what it cites.
	text map[string]string
	// answerText holds the lowercased answers (learned and from this run),
	// which may name skills the profile's skill lists don't.
	answerText []string
	// everything is the whole profile and answers, lowercased: the fallback
	// for names a letter mentions without citing.
	everything string
}

func newCatalog(p *profile.Profile, answers []Answer) *catalog {
	c := &catalog{
		profile:  p,
		owner:    map[string]string{},
		roles:    map[string]bool{},
		projects: map[string]bool{},
		skills:   map[string]bool{},
		text:     map[string]string{},
	}
	addSkills := func(ss []string) {
		for _, s := range ss {
			c.skills[normSkill(s)] = true
		}
	}
	setText := func(id string, parts ...string) { c.text[id] = strings.ToLower(strings.Join(parts, " ")) }
	for _, e := range p.Experience {
		c.roles[e.ID] = true
		c.owner[e.ID] = e.ID
		setText(e.ID, e.Company, e.Title, e.Location, e.Context, e.Start, deref(e.End))
		for _, h := range e.Highlights {
			c.owner[h.ID] = e.ID
			addSkills(h.Skills)
			setText(h.ID, append([]string{h.Text, e.Company, e.Title}, append(h.Metrics, h.Skills...)...)...)
		}
	}
	for _, pr := range p.Projects {
		c.projects[pr.ID] = true
		c.owner[pr.ID] = ""
		addSkills(pr.Skills)
		setText(pr.ID, append([]string{pr.Name, pr.Text}, pr.Skills...)...)
	}
	for _, s := range p.CoverLetterStories {
		c.owner[s.ID] = ""
		setText(s.ID, s.Text)
	}
	for _, f := range p.LearnedFacts {
		c.owner[f.ID] = ""
		setText(f.ID, f.Question, f.Answer)
		c.answerText = append(c.answerText, strings.ToLower(f.Answer))
	}
	for i, f := range p.SummaryFacts {
		id := fmt.Sprintf("fact-%d", i+1)
		c.owner[id] = ""
		setText(id, f)
	}
	for _, a := range answers {
		c.owner[a.QuestionID] = ""
		setText(a.QuestionID, a.Question, a.Text)
		c.answerText = append(c.answerText, strings.ToLower(a.Text))
	}
	for _, group := range p.Skills {
		addSkills(group)
	}
	all, _ := json.Marshal(p)
	c.everything = strings.ToLower(string(all) + " " + strings.Join(c.answerText, " "))
	return c
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func normSkill(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// knowsSkill reports whether s is a profile skill or is named in one of the
// candidate's answers. Only answers count, not questions, so "No, I haven't
// used Kubernetes" still mentions it but "Have you used Kubernetes?" alone
// doesn't; the model is told not to list a skill an answer denies.
func (c *catalog) knowsSkill(s string) bool {
	n := normSkill(s)
	if c.skills[n] {
		return true
	}
	for _, t := range c.answerText {
		if strings.Contains(t, n) {
			return true
		}
	}
	return false
}

// findings collects what a check found. Problems are hard failures (a
// citation that doesn't exist, a skill the candidate doesn't have); warnings
// are about how it reads and are fixed on a retry if possible.
type findings struct {
	problems, warnings []string
}

func (f *findings) problem(format string, args ...any) {
	f.problems = append(f.problems, fmt.Sprintf(format, args...))
}

func (f *findings) warn(format string, args ...any) {
	f.warnings = append(f.warnings, fmt.Sprintf(format, args...))
}

// cite checks a line's citations. With required, a line with no sources is
// a problem.
func (c *catalog) cite(f *findings, where string, l Line, role string, required bool) {
	if strings.TrimSpace(l.Text) == "" {
		f.problem("%s is empty", where)
	}
	if required && len(l.Sources) == 0 {
		f.problem("%s cites no sources", where)
	}
	for _, id := range l.Sources {
		owner, ok := c.owner[id]
		switch {
		case !ok:
			f.problem("%s cites %q, which is not an ID in the profile or answers", where, id)
		case role != "" && owner != "" && owner != role:
			f.problem("%s is under role %q but cites %q from role %q", where, role, id, owner)
		}
	}
}

func (c *catalog) avoidWords(f *findings, texts []string) {
	for _, w := range c.profile.Preferences.AvoidWords {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
		for _, t := range texts {
			if re.MatchString(t) {
				f.problem("uses avoided word %q", w)
				break
			}
		}
	}
}

// checkResume returns every way the resume strays from the profile, plus
// lines that read badly.
func (c *catalog) checkResume(d *Draft) findings {
	var f findings
	c.cite(&f, "summary", d.Summary, "", true)
	if d.Summary.Text != "" {
		if strings.Contains(d.Summary.Text, ";") {
			f.warn("the summary joins ideas with a semicolon; use separate sentences")
		}
		if regexp.MustCompile(`(?i)^(spent|with|over|nearly|almost)\b.*\byears?\b`).MatchString(d.Summary.Text) {
			f.warn("the summary leads with tenure; lead with what the candidate does and the outcome")
		}
	}
	seenRoles := map[string]bool{}
	for i, r := range d.Experience {
		where := fmt.Sprintf("experience[%d]", i)
		if !c.roles[r.ID] {
			f.problem("%s has id %q, which is not a role in the profile", where, r.ID)
			continue
		}
		if seenRoles[r.ID] {
			f.problem("%s repeats role %q", where, r.ID)
		}
		seenRoles[r.ID] = true
		if len(r.Bullets) == 0 && !r.Brief {
			f.problem("role %q has no bullets", r.ID)
		}
		verbs := map[string]int{}
		for j, b := range r.Bullets {
			w := fmt.Sprintf("role %q bullet %d", r.ID, j+1)
			c.cite(&f, w, b, r.ID, true)
			if strings.Contains(b.Text, ";") {
				f.warn("%s joins two ideas with a semicolon; keep one idea per bullet", w)
			}
			if n := len(strings.Fields(b.Text)); n > 32 {
				f.warn("%s is %d words; keep bullets under about 25", w, n)
			}
			if v := firstWord(b.Text); v != "" {
				verbs[v]++
				if verbs[v] == 2 {
					f.warn("role %q starts more than one bullet with %q; vary the verbs", r.ID, v)
				}
			}
		}
	}
	for _, e := range c.profile.Experience {
		if !seenRoles[e.ID] {
			f.problem("role %q is missing; include every role", e.ID)
		}
	}
	for i, p := range d.Projects {
		if !c.projects[p.ID] {
			f.problem("projects[%d] has id %q, which is not a project in the profile", i, p.ID)
			continue
		}
		if name := c.projectName(p.ID); name != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.Text)), strings.ToLower(name)) {
			f.warn("project %q text repeats the project's name; it's shown automatically", p.ID)
		}
	}
	for _, g := range d.Skills {
		for _, s := range g.Items {
			if !c.knowsSkill(s) {
				f.problem("skill %q is not in the profile", s)
			}
		}
	}
	if len(d.Skills) > 5 {
		f.warn("there are %d skill groups; use 3 or 4", len(d.Skills))
	}
	c.avoidWords(&f, d.resumeTexts())
	return f
}

func (c *catalog) projectName(id string) string {
	for _, p := range c.profile.Projects {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

var (
	sentenceEnd = regexp.MustCompile(`[.!?]["')\]]?(\s+|$)`)
	numberRe    = regexp.MustCompile(`\d[\d,.]*%?`)
	wordRe      = regexp.MustCompile(`[\p{L}][\p{L}'’.-]*`)
	templated   = regexp.MustCompile(`(?i)^(i am writing|i'm writing|i am excited|i'm excited|i was excited|your (posting|job posting|listing|job description) (describes|says|asks)|you need someone|you are looking for|you're looking for)`)
)

// checkLetter returns problems with the cover letter's citations, plus the
// signs of a letter that reads like a list of facts. posting is used to
// accept numbers and names the letter takes from the posting itself.
func (c *catalog) checkLetter(cl *CoverLetter, posting string) findings {
	var f findings
	if len(cl.Paragraphs) == 0 {
		f.problem("cover letter has no paragraphs")
		return f
	}
	lowPosting := strings.ToLower(posting)
	words := 0
	for i, p := range cl.Paragraphs {
		where := fmt.Sprintf("cover letter paragraph %d", i+1)
		c.cite(&f, where, p, "", false)
		words += len(strings.Fields(p.Text))

		var cited strings.Builder
		distinct := map[string]bool{}
		for _, id := range p.Sources {
			cited.WriteString(c.text[id] + " ")
			distinct[id] = true
		}
		if len(distinct) >= 4 {
			f.warn("%s draws on %d separate sources; that reads as a list of facts. Tell one or two of them as a story instead", where, len(distinct))
		}
		support := cited.String() + " " + lowPosting
		for _, n := range numberRe.FindAllString(p.Text, -1) {
			n = strings.TrimRight(n, ".,")
			if !strings.Contains(support, strings.ToLower(n)) {
				f.warn("%s mentions %q, which isn't in what it cites or in the posting", where, n)
			}
		}
		for _, name := range properNouns(p.Text) {
			l := strings.ToLower(name)
			if !strings.Contains(support, l) && !strings.Contains(c.everything, l) {
				f.warn("%s mentions %q, which isn't in the profile, the answers or the posting", where, name)
			}
		}

		sentences := splitSentences(p.Text)
		run := 0
		for _, s := range sentences {
			if firstWord(s) == "i" {
				run++
				if run == 3 {
					f.warn("%s starts three sentences in a row with \"I\"; vary how sentences open", where)
				}
			} else {
				run = 0
			}
		}
		if i == 0 && len(sentences) > 0 && templated.MatchString(strings.TrimSpace(sentences[0])) {
			f.warn("the letter opens with a stock line (%q); open with something specific to this company", clip(sentences[0], 60))
		}
	}
	if words < 200 || words > 420 {
		f.warn("the letter is %d words; aim for 250 to 350", words)
	}
	c.avoidWords(&f, cl.texts())
	return f
}

// properNouns returns capitalized words that don't start a sentence, which
// are usually names of companies, products or places.
func properNouns(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range splitSentences(text) {
		ws := wordRe.FindAllString(s, -1)
		for i, w := range ws {
			w = strings.TrimRight(w, ".-’'")
			r := []rune(w)
			if i == 0 || len(r) < 2 || !unicode.IsUpper(r[0]) || w == "I" || strings.HasPrefix(w, "I'") || strings.HasPrefix(w, "I’") {
				continue
			}
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}

func splitSentences(text string) []string {
	var out []string
	for _, s := range sentenceEnd.Split(strings.TrimSpace(text), -1) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// firstWord returns the first word of s, lowercased, without punctuation.
func firstWord(s string) string {
	w := wordRe.FindString(s)
	w = strings.ToLower(strings.TrimRight(w, ".-’'"))
	if w == "i'm" || w == "i’m" || w == "i've" || w == "i’ve" || w == "i'd" || w == "i’d" {
		return "i"
	}
	return w
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

// resumeTexts returns every piece of prose on the resume.
func (d *Draft) resumeTexts() []string {
	out := []string{d.Headline, d.Summary.Text}
	for _, r := range d.Experience {
		for _, b := range r.Bullets {
			out = append(out, b.Text)
		}
	}
	for _, p := range d.Projects {
		out = append(out, p.Text)
	}
	return out
}

// texts returns every piece of prose in the cover letter.
func (cl *CoverLetter) texts() []string {
	out := []string{cl.Greeting, cl.Closing}
	for _, p := range cl.Paragraphs {
		out = append(out, p.Text)
	}
	return out
}
