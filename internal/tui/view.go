package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Blathe/rezgen/internal/track"
)

var (
	accent   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A08CFF"})
	title    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A08CFF"})
	faint    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8A8A8A"})
	bold     = lipgloss.NewStyle().Bold(true)
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF7A70"})
	okStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#067647", Dark: "#5FD49A"})
	selected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
			Background(lipgloss.AdaptiveColor{Light: "#E6E0FF", Dark: "#3A2F70"})
	page = lipgloss.NewStyle().Padding(1, 2)
)

// statusStyle colors each status so the list scans at a glance.
func statusStyle(s track.Status) lipgloss.Style {
	c := map[track.Status]lipgloss.AdaptiveColor{
		track.Draft:        {Light: "#6B6B6B", Dark: "#9A9A9A"},
		track.Applied:      {Light: "#175CD3", Dark: "#7CB4FF"},
		track.Interviewing: {Light: "#B54708", Dark: "#FDB022"},
		track.Offer:        {Light: "#067647", Dark: "#5FD49A"},
		track.Rejected:     {Light: "#B42318", Dark: "#FF7A70"},
		track.Withdrawn:    {Light: "#6B6B6B", Dark: "#7A7A7A"},
	}[s]
	return lipgloss.NewStyle().Foreground(c)
}

func (m Model) View() string {
	var body, help string
	switch m.screen {
	case scrList:
		body, help = m.listView(), "↑/↓ move · enter details · s status · o open resume · n new application · r refresh · q quit"
	case scrDetail:
		body, help = m.detailView(), "s status · a add note · o resume · c cover letter · p posting · f folder · esc back"
	case scrStatus:
		body, help = m.statusView(), "↑/↓ or 1-6 choose · enter select · esc cancel"
	case scrNote:
		body, help = m.noteView(), "enter save · esc cancel"
	case scrPosting:
		body, help = m.postingView(), "enter start · esc cancel"
	case scrWorking:
		body, help = fmt.Sprintf("%s %s", m.spin.View(), m.working), "esc cancel"
	case scrQuestion:
		body, help = m.questionView(), "enter next (empty skips) · esc skip the rest"
	case scrLearn:
		body, help = m.learnView(), "y/enter save · n don't save"
	case scrDone:
		body, help = m.doneView(), "o open resume · c cover letter · f folder · enter back to list"
	}

	var b strings.Builder
	b.WriteString(title.Render("rezgen") + faint.Render("  resume and cover letter tailoring") + "\n\n")
	b.WriteString(body)
	b.WriteString("\n\n")
	if m.flash != "" {
		st := okStyle
		if m.flashErr {
			st = errStyle
		}
		b.WriteString(st.Render(m.flash) + "\n")
	}
	b.WriteString(faint.Render(help))
	return page.Render(b.String())
}

func (m Model) listView() string {
	if len(m.apps) == 0 {
		return fmt.Sprintf("No applications in %s yet.\n\nPress %s to tailor your first one.", m.cfg.OutDir, bold.Render("n"))
	}
	compW, roleW := 22, 34
	if m.width > 0 {
		// Give extra room to the role column on wide terminals.
		if extra := m.width - 4 - (14 + 22 + compW + roleW + 6); extra > 0 {
			roleW += min(extra, 30)
		}
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-13s %-20s %-*s %-*s", "STATUS", "UPDATED", compW, "COMPANY", roleW, "ROLE")
	b.WriteString(faint.Render(header) + "\n")

	// Show a window of rows around the cursor on short terminals.
	start, end := 0, len(m.apps)
	if rows := m.height - 10; m.height > 0 && rows > 3 && len(m.apps) > rows {
		start = max(0, min(m.cursor-rows/2, len(m.apps)-rows))
		end = start + rows
	}
	for i := start; i < end; i++ {
		a := m.apps[i]
		row := fmt.Sprintf("%-13s %-20s %-*s %-*s",
			a.Status, track.Age(a.Updated(), m.cfg.Now()),
			compW, clip(dash(a.Company), compW), roleW, clip(dash(a.Role), roleW))
		if i == m.cursor {
			b.WriteString(accent.Render("▸ ") + selected.Render(row) + "\n")
		} else {
			// Color just the status word.
			s := string(a.Status)
			b.WriteString("  " + statusStyle(a.Status).Render(s) + row[len(s):] + "\n")
		}
	}
	if start > 0 || end < len(m.apps) {
		b.WriteString(faint.Render(fmt.Sprintf("  %d-%d of %d", start+1, end, len(m.apps))) + "\n")
	}

	counts := map[track.Status]int{}
	for _, a := range m.apps {
		counts[a.Status]++
	}
	var parts []string
	for _, st := range track.Statuses {
		if n := counts[st]; n > 0 {
			parts = append(parts, statusStyle(st).Render(fmt.Sprintf("%d %s", n, st)))
		}
	}
	b.WriteString("\n" + plural(len(m.apps), "application") + ": " + strings.Join(parts, faint.Render(", ")))
	return b.String()
}

func (m Model) detailView() string {
	a := m.selected()
	if a == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(bold.Render(dash(a.Role)) + faint.Render(" at ") + bold.Render(dash(a.Company)) + "\n")
	b.WriteString(faint.Render(a.Dir) + "\n")
	if a.Source != "" {
		b.WriteString(faint.Render("Posting: ") + a.Source + "\n")
	}
	b.WriteString("\nStatus: " + statusStyle(a.Status).Bold(true).Render(string(a.Status)) + "\n\n")
	b.WriteString(faint.Render("History") + "\n")
	for i := len(a.History) - 1; i >= 0; i-- {
		e := a.History[i]
		line := fmt.Sprintf("  %s  %s", e.Date, statusStyle(e.Status).Render(fmt.Sprintf("%-12s", e.Status)))
		if e.Note != "" {
			line += " " + e.Note
		}
		b.WriteString(line + "\n")
	}
	var files []string
	for _, f := range []string{"resume.pdf", "cover-letter.pdf", "resume.md", "cover-letter.md", "sources.md", "draft-rejected.json"} {
		if _, err := os.Stat(filepath.Join(a.Dir, f)); err == nil {
			files = append(files, f)
		}
	}
	if len(files) > 0 {
		b.WriteString("\n" + faint.Render("Files: ") + strings.Join(files, ", "))
	}
	return b.String()
}

func (m Model) statusView() string {
	a := m.selected()
	if a == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("New status for %s (now %s):\n\n", bold.Render(label(*a)), statusStyle(a.Status).Render(string(a.Status))))
	for i, st := range track.Statuses {
		line := fmt.Sprintf("%d  %s", i+1, st)
		if i == m.statusCursor {
			b.WriteString(accent.Render("▸ ") + selected.Render(line) + "\n")
		} else {
			b.WriteString("  " + fmt.Sprintf("%d  ", i+1) + statusStyle(st).Render(string(st)) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) noteView() string {
	a := m.selected()
	if a == nil {
		return ""
	}
	head := fmt.Sprintf("%s -> %s", label(*a), statusStyle(m.newStatus).Render(string(m.newStatus)))
	if a.Status == m.newStatus {
		head = fmt.Sprintf("Add a note to %s (%s)", label(*a), statusStyle(m.newStatus).Render(string(m.newStatus)))
	}
	return head + "\n\nNote: " + m.note.View()
}

func (m Model) postingView() string {
	return bold.Render("New application") + "\n\n" +
		"Paste the job posting's link, or the path to a text file with the posting in it.\n\n" +
		m.input.View()
}

func (m Model) questionView() string {
	a := m.analysis
	qs := a.Questions
	q := qs[m.question]
	var b strings.Builder
	b.WriteString(bold.Render(dash(a.Role)) + faint.Render(" at ") + bold.Render(dash(a.Company)) + "\n")
	b.WriteString(okStyle.Render(fmt.Sprintf("%d requirements matched", len(a.Matches))))
	if len(a.Gaps) > 0 {
		b.WriteString(faint.Render(" · ") + errStyle.Render(fmt.Sprintf("%d gaps: %s", len(a.Gaps), clip(strings.Join(a.Gaps, ", "), 80))))
	}
	b.WriteString("\n\n")
	b.WriteString(faint.Render(fmt.Sprintf("Question %d of %d", m.question+1, len(qs))) + "\n")
	b.WriteString(bold.Render(q.Text) + "\n")
	if q.Why != "" {
		b.WriteString(faint.Render(q.Why) + "\n")
	}
	b.WriteString("\n" + m.input.View())
	return b.String()
}

func (m Model) learnView() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Save %s to your profile so rezgen won't ask again?\n\n", plural(len(m.answers), "answer")))
	for _, an := range m.answers {
		b.WriteString(faint.Render("  "+an.Question) + "\n  " + an.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) doneView() string {
	r := m.result
	var b strings.Builder
	if r.rejected != nil {
		b.WriteString(errStyle.Render("The draft failed the source checks twice, so it wasn't turned into a resume.") + "\n\n")
		for _, p := range r.rejected.Problems {
			b.WriteString("  - " + p + "\n")
		}
		b.WriteString("\nThe analysis and the rejected draft are in " + r.dir)
		return b.String()
	}
	b.WriteString(okStyle.Render("Done.") + " Saved to " + bold.Render(r.dir) + "\n")
	if r.err != nil {
		b.WriteString(errStyle.Render("PDF export failed: "+r.err.Error()) + "\n")
	}
	if p := m.profile; p != nil && r.pages > 0 && p.Preferences.MaxPages > 0 && r.pages > p.Preferences.MaxPages {
		b.WriteString(errStyle.Render(fmt.Sprintf("The resume is %d pages; your profile asks for %d. Trim resume.md, then run: rezgen pdf %s",
			r.pages, p.Preferences.MaxPages, filepath.Join(r.dir, "resume.md"))) + "\n")
	}
	b.WriteString("\nIt's on your list as a draft. Once you've sent it, select it and press " + bold.Render("s") + " to mark it applied.")
	return b.String()
}

// clip shortens s to n characters.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-3])) + "..."
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
