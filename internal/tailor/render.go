package tailor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/profile"
)

// RenderResume returns the resume as Markdown. Contact details, titles,
// dates, education and certifications come from the profile; the draft
// supplies only the selected and reworded content.
func RenderResume(p *profile.Profile, d *Draft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Contact.Name)
	if d.Headline != "" {
		fmt.Fprintf(&b, "**%s**\n\n", d.Headline)
	}
	b.WriteString(contactLine(p.Contact) + "\n")

	if d.Summary.Text != "" {
		fmt.Fprintf(&b, "\n## Summary\n\n%s\n", d.Summary.Text)
	}

	// Projects go right after the summary unless the profile says
	// otherwise: for technical roles they're often the strongest evidence.
	projectsFirst := p.Preferences.ProjectsPosition != "after"
	if projectsFirst {
		renderProjects(&b, p, d)
	}

	roles := map[string]profile.Experience{}
	for _, e := range p.Experience {
		roles[e.ID] = e
	}
	var full, brief []profile.Experience
	drafts := map[string]RoleDraft{}
	for _, rd := range d.Experience {
		e, ok := roles[rd.ID]
		if !ok {
			continue
		}
		if rd.Brief {
			brief = append(brief, e)
		} else {
			full = append(full, e)
			drafts[e.ID] = rd
		}
	}
	if len(full) > 0 {
		b.WriteString("\n## Experience\n")
		for _, e := range full {
			fmt.Fprintf(&b, "\n### %s, %s\n\n", e.Title, e.Company)
			when := monthRange(e.Start, e.End)
			if loc := ShortLocation(e.Location); loc != "" {
				when = loc + " | " + when
			}
			fmt.Fprintf(&b, "%s\n\n", when)
			for _, bl := range drafts[e.ID].Bullets {
				fmt.Fprintf(&b, "- %s\n", bl.Text)
			}
		}
	}
	if len(brief) > 0 {
		b.WriteString("\n## Earlier experience\n\n")
		for _, e := range brief {
			fmt.Fprintf(&b, "- %s, %s (%s)\n", e.Title, e.Company, monthRange(e.Start, e.End))
		}
	}

	if !projectsFirst {
		renderProjects(&b, p, d)
	}

	if len(d.Skills) > 0 {
		b.WriteString("\n## Skills\n\n")
		for _, g := range d.Skills {
			if len(g.Items) == 0 {
				continue
			}
			fmt.Fprintf(&b, "- **%s:** %s\n", g.Group, strings.Join(g.Items, ", "))
		}
	}

	if len(p.Education) > 0 {
		b.WriteString("\n## Education\n\n")
		for _, ed := range p.Education {
			line := joinNonEmpty(", ", ed.Degree, ed.Field)
			line = joinNonEmpty(", ", line, ed.School)
			if ed.End != "" {
				line += " (" + formatDate(ed.End) + ")"
			}
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}

	if len(p.Certifications) > 0 {
		b.WriteString("\n## Certifications\n\n")
		for _, c := range p.Certifications {
			line := joinNonEmpty(", ", c.Name, c.Issuer)
			if c.Date != "" {
				line += " (" + formatDate(c.Date) + ")"
			}
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	return b.String()
}

func renderProjects(b *strings.Builder, p *profile.Profile, d *Draft) {
	if len(d.Projects) == 0 {
		return
	}
	projects := map[string]profile.Project{}
	for _, pr := range p.Projects {
		projects[pr.ID] = pr
	}
	b.WriteString("\n## Projects\n\n")
	for _, pl := range d.Projects {
		pr, ok := projects[pl.ID]
		if !ok {
			continue
		}
		name := "**" + pr.Name + "**"
		if pr.URL != "" {
			name = fmt.Sprintf("**[%s](%s)**", pr.Name, pr.URL)
		}
		fmt.Fprintf(b, "- %s: %s\n", name, stripName(pl.Text, pr.Name))
	}
}

// stripName removes a leading "Name:" or "Name -" from a project's text,
// since the name is already shown before it.
func stripName(text, name string) string {
	t := strings.TrimSpace(text)
	if name == "" || len(t) < len(name) || !strings.EqualFold(t[:len(name)], name) {
		return t
	}
	rest := strings.TrimLeft(t[len(name):], " :-–—,")
	if rest == "" || rest == t[len(name):] {
		return t // the name was just the first word of a sentence about it
	}
	return strings.ToUpper(rest[:1]) + rest[1:]
}

var usStates = map[string]string{
	"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR", "california": "CA", "colorado": "CO",
	"connecticut": "CT", "delaware": "DE", "florida": "FL", "georgia": "GA", "hawaii": "HI", "idaho": "ID",
	"illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS", "kentucky": "KY", "louisiana": "LA",
	"maine": "ME", "maryland": "MD", "massachusetts": "MA", "michigan": "MI", "minnesota": "MN",
	"mississippi": "MS", "missouri": "MO", "montana": "MT", "nebraska": "NE", "nevada": "NV",
	"new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY", "north carolina": "NC",
	"north dakota": "ND", "ohio": "OH", "oklahoma": "OK", "oregon": "OR", "pennsylvania": "PA",
	"rhode island": "RI", "south carolina": "SC", "south dakota": "SD", "tennessee": "TN", "texas": "TX",
	"utah": "UT", "vermont": "VT", "virginia": "VA", "washington": "WA", "west virginia": "WV",
	"wisconsin": "WI", "wyoming": "WY", "district of columbia": "DC",
}

// ShortLocation normalizes US locations to "City, ST": "Post Falls, Idaho,
// United States" becomes "Post Falls, ID". Anything else is left as is.
func ShortLocation(loc string) string {
	parts := strings.Split(loc, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if n := len(parts); n > 1 {
		switch strings.ToLower(parts[n-1]) {
		case "united states", "united states of america", "usa", "us", "u.s.", "u.s.a.":
			parts = parts[:n-1]
		}
	}
	if n := len(parts); n > 1 {
		if abbr, ok := usStates[strings.ToLower(parts[n-1])]; ok {
			parts[n-1] = abbr
		}
	}
	return strings.Join(parts, ", ")
}

// RenderCoverLetter returns the cover letter as Markdown, dated on.
func RenderCoverLetter(p *profile.Profile, d *Draft, on time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", p.Contact.Name, contactLine(p.Contact))
	fmt.Fprintf(&b, "%s\n\n", on.Format("January 2, 2006"))
	if d.CoverLetter.Greeting != "" {
		fmt.Fprintf(&b, "%s\n\n", d.CoverLetter.Greeting)
	}
	for _, para := range d.CoverLetter.Paragraphs {
		fmt.Fprintf(&b, "%s\n\n", para.Text)
	}
	closing := d.CoverLetter.Closing
	if closing == "" {
		closing = "Sincerely,"
	}
	fmt.Fprintf(&b, "%s\n\n%s\n", closing, p.Contact.Name)
	return b.String()
}

// RenderSources returns a Markdown table mapping each drafted line to the
// profile entries it cites, for checking the draft by hand.
func RenderSources(d *Draft) string {
	var b strings.Builder
	b.WriteString("# Sources\n\nEach generated line and the profile entries or answers it was drawn from.\n\n| Where | Text | Sources |\n| --- | --- | --- |\n")
	row := func(where string, l Line) {
		text := strings.ReplaceAll(l.Text, "|", `\|`)
		fmt.Fprintf(&b, "| %s | %s | %s |\n", where, text, strings.Join(l.Sources, ", "))
	}
	row("summary", d.Summary)
	for _, r := range d.Experience {
		for i, bl := range r.Bullets {
			row(fmt.Sprintf("%s #%d", r.ID, i+1), bl)
		}
	}
	for i, p := range d.CoverLetter.Paragraphs {
		row(fmt.Sprintf("cover letter #%d", i+1), p)
	}
	return b.String()
}

func contactLine(c profile.Contact) string {
	parts := []string{}
	for _, s := range []string{c.Email, c.Phone, ShortLocation(c.Location)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	keys := make([]string, 0, len(c.Links))
	for k := range c.Links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, c.Links[k])
	}
	return strings.Join(parts, " | ")
}

func monthRange(start string, end *string) string {
	e := "Present"
	if end != nil {
		e = formatDate(*end)
	}
	return formatDate(start) + " - " + e
}

// formatDate turns "2022-03" into "Mar 2022" and leaves "2016" as is.
func formatDate(s string) string {
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.Format("Jan 2006")
	}
	return s
}

func joinNonEmpty(sep string, parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
