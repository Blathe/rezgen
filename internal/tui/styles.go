package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

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
		b.WriteString(bold.Render(clip(m.app.Name, m.innerWidth())) + "\n\n")
	}
	done := 0
	for _, s := range m.steps {
		if s.state == stepDone || s.state == stepSkipped {
			done++
		}
	}
	bar := progress.New(progress.WithScaledGradient("#7C5CFF", "#5FD49A"), progress.WithoutPercentage())
	bar.Width = min(50, m.innerWidth())
	b.WriteString(bar.ViewAs(float64(done)/float64(len(m.steps))) + faint.Render(fmt.Sprintf("  %d of %d", done, len(m.steps))) + "\n\n")
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
