package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Blathe/rezgen/internal/track"
)

// Colors adapt to light and dark terminal backgrounds.
var (
	accentColor  = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A08CFF"}
	accentStrong = lipgloss.AdaptiveColor{Light: "#4A2FC0", Dark: "#7C5CFF"}
	textColor    = lipgloss.AdaptiveColor{Light: "#1F1F24", Dark: "#E8E6F0"}
	mutedColor   = lipgloss.AdaptiveColor{Light: "#6B6B76", Dark: "#8C8A99"}
	subtleColor  = lipgloss.AdaptiveColor{Light: "#D9D6E3", Dark: "#3A3848"}
	okColor      = lipgloss.AdaptiveColor{Light: "#067647", Dark: "#5FD49A"}
	errColor     = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF7A70"}
	warnColor    = lipgloss.AdaptiveColor{Light: "#B54708", Dark: "#FDB022"}
	selectBg     = lipgloss.AdaptiveColor{Light: "#ECE8FF", Dark: "#2E2650"}
)

var (
	accent   = lipgloss.NewStyle().Foreground(accentColor)
	faint    = lipgloss.NewStyle().Foreground(mutedColor)
	bold     = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	okStyle  = lipgloss.NewStyle().Foreground(okColor)
	errStyle = lipgloss.NewStyle().Foreground(errColor)
	selected = lipgloss.NewStyle().Bold(true).Foreground(textColor).Background(selectBg)

	page = lipgloss.NewStyle().Padding(1, 2)

	// badge is the app name in the header.
	badge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accentStrong).Padding(0, 1)

	// card holds each screen's content.
	card = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtleColor).Padding(1, 2)

	// panel highlights a result inside a card; see okPanel and errPanel.
	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Padding(0, 1)
	okPanel  = panel.BorderForeground(okColor)
	errPanel = panel.BorderForeground(errColor)

	heading = lipgloss.NewStyle().Bold(true).Foreground(textColor).MarginBottom(1)
	label   = lipgloss.NewStyle().Foreground(mutedColor).Width(10)
	keyCap  = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
)

// pill renders a small rounded-looking tag in color c.
func pill(text string, c lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(text)
}

// statusPill shows an application's status with an icon.
func statusPill(s track.Status) string {
	if s == track.Generated {
		return pill("● generated", okColor)
	}
	return pill("○ not started", mutedColor)
}

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

// field renders a label and value on one line, labels aligned.
func field(name, value string) string {
	return label.Render(name) + value
}

// contentWidth is the card's outer width: the terminal width less the page
// padding, capped so lines stay readable on wide screens.
func (m Model) contentWidth() int {
	if m.width <= 0 {
		return 96
	}
	return max(50, min(m.width-4, 110))
}

// innerWidth is the space inside the card's border and padding.
func (m Model) innerWidth() int { return m.contentWidth() - 6 }

func (m Model) View() string {
	var body, help string
	switch m.screen {
	case scrSetup:
		body, help = m.setupView()
	case scrHome:
		body, help = m.homeView()
	case scrNew:
		body, help = m.newView(), "ctrl+s continue · enter continue (single link or path) · esc cancel"
	case scrNewName:
		body, help = m.newNameView(), "enter add application · esc cancel"
	case scrApp:
		body, help = m.appView()
	case scrConfirm:
		body, help = m.confirm.prompt, "y yes · n no"
	case scrViewer:
		body = heading.Render(m.confirm.viewTitle) + "\n" + m.viewer.View()
		help = "↑/↓ scroll · pgup/pgdn page · esc back"
	case scrWorking:
		body, help = m.workingView(), "esc cancel"
	case scrQuestion:
		body, help = m.questionView(), "enter next (empty skips) · esc skip the rest"
	case scrLearn:
		body, help = m.learnView(), "y/enter save · n don't save"
	case scrSettings:
		body, help = m.settingsView()
	}

	w := m.contentWidth()
	parts := []string{m.header(w), card.Width(w).Render(body)}
	if f := m.flashView(w); f != "" {
		parts = append(parts, f)
	}
	parts = append(parts, helpBar(help, w))
	return page.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

// header shows the app badge and screen title on the left and the model on
// the right.
func (m Model) header(w int) string {
	left := badge.Render("rezgen") + "  " + bold.Render(m.screenTitle())
	right := ""
	if m.conf != nil {
		right = faint.Render(m.conf.Model)
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left + "\n"
	}
	return left + strings.Repeat(" ", gap) + right + "\n"
}

func (m Model) screenTitle() string {
	switch m.screen {
	case scrSetup:
		return "Setup"
	case scrHome:
		return "Applications"
	case scrNew, scrNewName:
		return "New application"
	case scrSettings:
		return "Settings"
	case scrViewer:
		return "Viewer"
	case scrQuestion, scrLearn:
		return "Questions"
	case scrWorking:
		if m.gen.active {
			return "Generating"
		}
		return "Working"
	}
	return "Application"
}

// clipPath shortens a long path by cutting out its middle, so the start
// (drive, data folder) and end (application folder, file) stay readable.
func clipPath(p string, n int) string {
	r := []rune(p)
	if len(r) <= n || n < 10 {
		return p
	}
	keep := n - 3
	head := keep / 3
	return string(r[:head]) + "..." + string(r[len(r)-(keep-head):])
}

// flashView shows the last status message with an icon.
func (m Model) flashView(w int) string {
	if m.flash == "" {
		return ""
	}
	icon, c := "✓", okColor
	if m.flashErr {
		icon, c = "✗", errColor
	}
	st := lipgloss.NewStyle().Foreground(c).Width(w).PaddingLeft(1)
	return st.Render(icon + " " + m.flash)
}

// helpBar renders "key desc · key desc" help text with the keys
// highlighted. A newline in help starts a second row.
func helpBar(help string, w int) string {
	var rows []string
	for _, line := range strings.Split(help, "\n") {
		var items []string
		for _, item := range strings.Split(line, " · ") {
			k, desc, _ := strings.Cut(strings.TrimSpace(item), " ")
			items = append(items, keyCap.Render(k)+" "+faint.Render(desc))
		}
		rows = append(rows, strings.Join(items, faint.Render("  •  ")))
	}
	return lipgloss.NewStyle().Width(w).PaddingLeft(1).PaddingTop(1).Render(strings.Join(rows, "\n"))
}
