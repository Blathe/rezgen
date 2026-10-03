package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/store"
)

func (m Model) homeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cursor = clamp(m.cursor-1, len(m.apps))
	case "down", "j":
		m.cursor = clamp(m.cursor+1, len(m.apps))
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = clamp(len(m.apps)-1, len(m.apps))
	case "enter", "right", "l":
		if a := m.selected(); a != nil {
			return m.openApp(a)
		}
	case "n":
		return m.startNew()
	case "d", "delete":
		if a := m.selected(); a != nil {
			return m.askDelete(a)
		}
	case ",":
		return m.openSettings()
	case "r":
		return m, m.loadApps("")
	}
	return m, nil
}

func (m Model) selected() *store.Application {
	if m.cursor < 0 || m.cursor >= len(m.apps) {
		return nil
	}
	return m.apps[m.cursor]
}

func (m Model) homeView() (string, string) {
	help := "↑/↓ move · enter open · n new application · d delete · , settings · q quit"
	if len(m.apps) == 0 {
		return "No applications yet.\n\nPress " + bold.Render("n") + " to add one: paste a job posting's link or its text.", "n new application · , settings · q quit"
	}
	w := m.innerWidth()
	// Columns: marker, status, name, created. The name gets what's left.
	const statusW, createdW = 15, 19
	nameW := max(20, w-2-statusW-createdW-6)

	start, end := 0, len(m.apps)
	if rows := m.height - 16; m.height > 0 && rows > 3 && len(m.apps) > rows {
		start = max(0, min(m.cursor-rows/2, len(m.apps)-rows))
		end = start + rows
	}
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(lipgloss.NewStyle().Foreground(subtleColor)).
		Headers("", "STATUS", "NAME", "CREATED").
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(2)
			if col == 0 {
				s = lipgloss.NewStyle().Width(2)
			}
			if row == table.HeaderRow {
				return s.Foreground(mutedColor).Bold(true)
			}
			if start+row == m.cursor {
				return s.Background(selectBg).Foreground(textColor).Bold(true)
			}
			return s
		})
	for i := start; i < end; i++ {
		a := m.apps[i]
		created := store.Age(a.Created, m.cfg.Now())
		if i == m.cursor {
			// Plain text, so the row's highlight isn't interrupted by the
			// cells' own colors.
			icon := "○ "
			if a.Status() == store.Generated {
				icon = "● "
			}
			t.Row("▸", icon+string(a.Status()), clip(a.Name, nameW), created)
			continue
		}
		t.Row(" ", statusPill(a.Status()), clip(a.Name, nameW), faint.Render(created))
	}

	var b strings.Builder
	b.WriteString(t.Render() + "\n")
	if start > 0 || end < len(m.apps) {
		b.WriteString(faint.Render(fmt.Sprintf("  %d-%d of %d", start+1, end, len(m.apps))) + "\n")
	}
	generated := 0
	for _, a := range m.apps {
		if a.Status() == store.Generated {
			generated++
		}
	}
	b.WriteString("\n" + faint.Render(plural(len(m.apps), "application")+"   ") +
		pill(fmt.Sprintf("● %d generated", generated), okColor) + "   " +
		pill(fmt.Sprintf("○ %d not started", len(m.apps)-generated), mutedColor))
	return b.String(), help
}

func (m Model) askDelete(a *store.Application) (tea.Model, tea.Cmd) {
	prompt := fmt.Sprintf("Delete %s?\n\nThis removes the saved posting and the generated resume and cover letter.",
		bold.Render(a.Name))
	return m.ask(prompt, func(m Model) (tea.Model, tea.Cmd) {
		if err := m.store().Delete(a); err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		m.app = nil
		m.screen = scrHome
		m.setFlash("Deleted "+a.Name+".", false)
		return m, m.loadApps("")
	})
}

// New application: paste a link or text, then name it.

func (m Model) startNew() (tea.Model, tea.Cmd) {
	m.flash = ""
	m.newPost = nil
	m.screen = scrNew
	m.area.Reset()
	m.area.Placeholder = "Paste the job posting's link, or the whole job description"
	return m, m.area.Focus()
}

func (m Model) newKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.area.Blur()
		m.screen = scrHome
		return m, nil
	case "ctrl+s":
		return m.submitPosting()
	case "enter":
		// A single line that's a link or a path doesn't need ctrl+s.
		if v := strings.TrimSpace(m.area.Value()); v != "" && !strings.Contains(v, "\n") {
			src := strings.Trim(v, `"'`)
			if posting.IsURL(src) || fileExists(src) {
				return m.submitPosting()
			}
		}
	}
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(k)
	return m, cmd
}

type postingMsg struct {
	post *posting.Posting
	err  error
}

func (m Model) submitPosting() (tea.Model, tea.Cmd) {
	input := m.area.Value()
	if strings.TrimSpace(input) == "" {
		return m, nil
	}
	m.area.Blur()
	msg := "Reading the posting..."
	if posting.IsURL(strings.Trim(strings.TrimSpace(input), `"'`)) {
		msg = "Fetching the posting..."
	}
	ctx := m.startWork(msg)
	loader := m.cfg.Loader
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		p, err := loader.FromInput(ctx, input)
		return postingMsg{post: p, err: err}
	})
}

func (m Model) gotPosting(msg postingMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrWorking {
		return m, nil
	}
	m.stopWork()
	if msg.err != nil {
		m.setFlash(msg.err.Error(), true)
		m.screen = scrNew
		return m, m.area.Focus() // keep what they pasted
	}
	m.newPost = msg.post
	m.flash = ""
	m.screen = scrNewName
	m.textIn.Reset()
	m.textIn.EchoMode = textinput.EchoNormal
	m.textIn.Placeholder = "e.g. Acme - AI Solutions Engineer"
	m.textIn.SetValue(posting.SuggestName(msg.post))
	m.textIn.CursorEnd()
	return m, m.textIn.Focus()
}

func (m Model) newNameKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.textIn.Blur()
		m.screen = scrNew
		return m, m.area.Focus()
	case "enter":
		name := strings.TrimSpace(m.textIn.Value())
		if name == "" {
			return m, nil
		}
		m.textIn.Blur()
		p := m.newPost
		a, err := m.store().Create(name, p.Text, p.Source, m.cfg.Now())
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		m.newPost = nil
		m.screen = scrHome
		m.setFlash(fmt.Sprintf("Added %s. Open it to generate your resume and cover letter.", a.Name), false)
		return m, m.loadApps(a.ID)
	}
	return m.updateTextIn(k)
}

func (m Model) newView() string {
	return faint.Render("Paste the job posting's link, or copy the whole job description from the page and paste it here.") + "\n\n" +
		m.area.View()
}

func (m Model) newNameView() string {
	p := m.newPost
	var b strings.Builder
	b.WriteString(bold.Render("Name this application") + "\n\n")
	b.WriteString(m.textIn.View() + "\n\n")
	src := "pasted text"
	if p.Source != "" {
		src = p.Source
	}
	words := len(strings.Fields(p.Text))
	b.WriteString(faint.Render(fmt.Sprintf("Posting: %s (%d words)", src, words)) + "\n")
	preview := p.Text
	if lines := strings.SplitN(preview, "\n", 6); len(lines) > 5 {
		preview = strings.Join(lines[:5], "\n") + "\n..."
	}
	b.WriteString(faint.Render(clip(preview, 400)))
	return b.String()
}
