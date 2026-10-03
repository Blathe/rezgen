package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Blathe/rezgen/internal/track"
)

var (
	accentColor = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A08CFF"}
	accent      = lipgloss.NewStyle().Foreground(accentColor)
	title       = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	faint       = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8A8A8A"})
	bold        = lipgloss.NewStyle().Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF7A70"})
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#067647", Dark: "#5FD49A"})
	selected    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
			Background(lipgloss.AdaptiveColor{Light: "#E6E0FF", Dark: "#3A2F70"})
	panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Padding(0, 1)
	page  = lipgloss.NewStyle().Padding(1, 2)
)

func statusStyle(s track.Status) lipgloss.Style {
	if s == track.Generated {
		return okStyle
	}
	return faint
}

// link renders text as a terminal hyperlink to url (OSC 8). Terminals that
// don't support hyperlinks just show the text.
func link(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func (m Model) View() string {
	var body, help string
	switch m.screen {
	case scrSetup:
		body, help = m.setupView()
	case scrHome:
		body, help = m.homeView()
	case scrNew:
		body, help = m.newView(), "ctrl+s continue (enter works for a single link or path) · esc cancel"
	case scrNewName:
		body, help = m.newNameView(), "enter add application · esc cancel"
	case scrApp:
		body, help = m.appView()
	case scrConfirm:
		body, help = m.confirm.prompt, "y yes · n no"
	case scrViewer:
		body = bold.Render(m.confirm.viewTitle) + "\n\n" + m.viewer.View()
		help = "↑/↓ pgup/pgdn scroll · esc back"
	case scrWorking:
		body, help = m.workingView(), "esc cancel"
	case scrQuestion:
		body, help = m.questionView(), "enter next (empty skips) · esc skip the rest"
	case scrLearn:
		body, help = m.learnView(), "y/enter save · n don't save"
	case scrSettings:
		body, help = m.settingsView()
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

// picker is a vertical list with a cursor.
type picker struct {
	items  []string
	notes  []string
	cursor int
}

func newPicker(items, notes []string, cursor int) picker {
	return picker{items: items, notes: notes, cursor: clamp(cursor, len(items))}
}

// update moves the cursor, and reports whether the user chose an item.
func (p *picker) update(k tea.KeyMsg) (chosen bool) {
	switch k.String() {
	case "up", "k":
		p.cursor = clamp(p.cursor-1, len(p.items))
	case "down", "j":
		p.cursor = clamp(p.cursor+1, len(p.items))
	case "enter":
		return true
	default:
		if s := k.String(); len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.items) {
			p.cursor = int(s[0] - '1')
			return true
		}
	}
	return false
}

func (p picker) view() string {
	var b strings.Builder
	for i, it := range p.items {
		line := fmt.Sprintf("%d  %s", i+1, it)
		note := ""
		if i < len(p.notes) && p.notes[i] != "" {
			note = "  " + faint.Render(p.notes[i])
		}
		if i == p.cursor {
			b.WriteString(accent.Render("▸ ") + selected.Render(line) + note + "\n")
		} else {
			b.WriteString("  " + line + note + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// step is one line of a progress checklist.
type step struct {
	label string
	state stepState
	note  string
}

type stepState int

const (
	stepPending stepState = iota
	stepRunning
	stepDone
	stepSkipped
)

func (m Model) workingView() string {
	if len(m.steps) == 0 {
		return m.spin.View() + " " + m.working
	}
	var b strings.Builder
	if m.app != nil {
		b.WriteString(bold.Render(m.app.Name) + "\n\n")
	}
	elapsed := m.cfg.Now().Sub(m.started).Round(1e9)
	for _, s := range m.steps {
		var mark string
		switch s.state {
		case stepDone:
			mark = okStyle.Render("✓")
		case stepRunning:
			mark = m.spin.View()
		case stepSkipped:
			mark = faint.Render("–")
		default:
			mark = faint.Render("○")
		}
		line := mark + " " + s.label
		if s.state == stepRunning {
			line += faint.Render(fmt.Sprintf("  %d:%02d", int(elapsed.Minutes()), int(elapsed.Seconds())%60))
		}
		if s.note != "" {
			line += faint.Render("  " + s.note)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// confirmState is a pending yes/no question.
type confirmState struct {
	prompt    string
	onYes     func(Model) (tea.Model, tea.Cmd)
	back      screen
	viewTitle string // title for the document viewer
}

func (m Model) ask(prompt string, onYes func(Model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	m.confirm = confirmState{prompt: prompt, onYes: onYes, back: m.screen}
	m.screen = scrConfirm
	return m, nil
}

func (m Model) confirmKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(k.String()) {
	case "y":
		m.screen = m.confirm.back
		return m.confirm.onYes(m)
	case "n", "esc":
		m.screen = m.confirm.back
	}
	return m, nil
}

// showDoc opens text in the scrolling viewer.
func (m Model) showDoc(titleText, text string) (tea.Model, tea.Cmd) {
	m.confirm.viewTitle = titleText
	m.confirm.back = m.screen
	m.viewer.SetContent(text)
	m.viewer.GotoTop()
	m.screen = scrViewer
	return m, nil
}

func (m Model) viewerKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.screen = m.confirm.back
		return m, nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(k)
	return m, cmd
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-3])) + "..."
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
