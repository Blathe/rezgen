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

	roles := map[string]profile.Experience{}
	for _, e := range p.Experience {
		roles[e.ID] = e
	}
	if len(d.Experience) > 0 {
		b.WriteString("\n## Experience\n")
		for _, rd := range d.Experience {
			e, ok := roles[rd.ID]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "\n### %s, %s\n\n", e.Title, e.Company)
			when := monthRange(e.Start, e.End)
			if e.Location != "" {
				when = e.Location + " | " + when
			}
			fmt.Fprintf(&b, "%s\n\n", when)
			for _, bl := range rd.Bullets {
				fmt.Fprintf(&b, "- %s\n", bl.Text)
			}
		}
	}

	projects := map[string]profile.Project{}
	for _, pr := range p.Projects {
		projects[pr.ID] = pr
	}
	if len(d.Projects) > 0 {
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
			fmt.Fprintf(&b, "- %s: %s\n", name, pl.Text)
		}
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
	for _, s := range []string{c.Email, c.Phone, c.Location} {
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
