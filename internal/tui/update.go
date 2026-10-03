package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
	"github.com/Blathe/rezgen/internal/track"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		return m.key(msg)

	case spinner.TickMsg:
		if m.screen != scrWorking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case appsMsg:
		if msg.err != nil {
			return m.fail(msg.err)
		}
		m.apps = msg.apps
		if msg.selectDir != "" {
			for i, a := range m.apps {
				if a.Dir == msg.selectDir {
					m.cursor = i
				}
			}
		}
		m.cursor = clamp(m.cursor, len(m.apps))
		return m, nil

	case postingMsg:
		if m.screen != scrWorking {
			return m, nil // cancelled
		}
		if msg.err != nil {
			return m.backToPosting(msg.err)
		}
		m.post = msg.post
		m.working = "Analyzing the posting against your profile..."
		return m, m.analyze(m.ctx())

	case analyzedMsg:
		if m.screen != scrWorking {
			return m, nil
		}
		if msg.err != nil {
			return m.backToPosting(msg.err)
		}
		m.analysis = msg.analysis
		if len(m.analysis.Questions) == 0 {
			return m.startWriting()
		}
		m.screen, m.question = scrQuestion, 0
		m.input.Reset()
		m.input.Placeholder = "answer with facts, or press Enter to skip"
		return m, m.input.Focus()

	case learnedMsg:
		if msg.err != nil {
			m.setFlash(fmt.Sprintf("Couldn't save answers to the profile: %v", msg.err), true)
		} else {
			m.setFlash(fmt.Sprintf("Saved %s to your profile.", plural(len(m.answers), "answer")), false)
		}
		return m.startWriting()

	case writtenMsg:
		if m.screen != scrWorking {
			return m, nil
		}
		m.stopWork()
		if msg.err != nil && msg.dir == "" {
			m.screen = scrList
			return m.fail(msg.err)
		}
		m.result, m.screen = msg, scrDone
		return m, m.loadApps(msg.dir)

	case openedMsg:
		if msg.err != nil {
			return m.fail(msg.err)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scrList:
		return m.listKey(k)
	case scrDetail:
		return m.detailKey(k)
	case scrStatus:
		return m.statusKey(k)
	case scrNote:
		return m.noteKey(k)
	case scrPosting:
		return m.postingKey(k)
	case scrWorking:
		if k.String() == "esc" {
			m.stopWork()
			m.screen = scrList
			m.setFlash("Cancelled.", false)
		}
		return m, nil
	case scrQuestion:
		return m.questionKey(k)
	case scrLearn:
		return m.learnKey(k)
	case scrDone:
		return m.doneKey(k)
	}
	return m, nil
}

func (m Model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		if m.selected() != nil {
			m.screen = scrDetail
		}
	case "s":
		m.fromDetail = false
		return m.startStatus()
	case "o":
		if a := m.selected(); a != nil {
			return m, m.open(firstExisting(a.Dir, "resume.pdf", "resume.md"))
		}
	case "n":
		return m.startNew()
	case "r":
		return m, m.loadApps("")
	}
	return m, nil
}

func (m Model) detailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	a := m.selected()
	if a == nil {
		m.screen = scrList
		return m, nil
	}
	switch k.String() {
	case "esc", "q", "backspace", "left", "h":
		m.screen = scrList
	case "s":
		m.fromDetail = true
		return m.startStatus()
	case "a":
		// A note without a status change: record the current status again.
		m.fromDetail = true
		m.newStatus = a.Status
		return m.startNote()
	case "o":
		return m, m.open(firstExisting(a.Dir, "resume.pdf", "resume.md"))
	case "c":
		return m, m.open(firstExisting(a.Dir, "cover-letter.pdf", "cover-letter.md"))
	case "f":
		return m, m.open(a.Dir)
	case "p":
		if a.Source != "" && strings.HasPrefix(a.Source, "http") {
			return m, m.open(a.Source)
		}
		return m, m.open(firstExisting(a.Dir, "posting.md"))
	}
	return m, nil
}

func (m Model) startStatus() (tea.Model, tea.Cmd) {
	a := m.selected()
	if a == nil {
		return m, nil
	}
	m.statusCursor = 0
	for i, st := range track.Statuses {
		if st == a.Status {
			m.statusCursor = i
		}
	}
	// Default to the next step along: a draft is most likely now applied.
	if m.statusCursor < len(track.Statuses)-1 && a.Status != track.Rejected && a.Status != track.Withdrawn && a.Status != track.Offer {
		m.statusCursor++
	}
	m.screen = scrStatus
	return m, nil
}

func (m Model) statusKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.screen = m.back()
	case "up", "k":
		m.statusCursor = clamp(m.statusCursor-1, len(track.Statuses))
	case "down", "j":
		m.statusCursor = clamp(m.statusCursor+1, len(track.Statuses))
	case "enter":
		m.newStatus = track.Statuses[m.statusCursor]
		return m.startNote()
	default:
		// Number keys pick a status directly.
		if s := k.String(); len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(track.Statuses) {
			m.newStatus = track.Statuses[s[0]-'1']
			return m.startNote()
		}
	}
	return m, nil
}

// back is where to return after a status change or note: the detail screen
// if that's where it started, otherwise the list.
func (m Model) back() screen {
	if m.fromDetail {
		return scrDetail
	}
	return scrList
}

func (m Model) startNote() (tea.Model, tea.Cmd) {
	m.screen = scrNote
	m.note.Reset()
	return m, m.note.Focus()
}

func (m Model) noteKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.note.Blur()
		m.screen = m.back()
		return m, nil
	case "enter":
		m.note.Blur()
		a := m.selected()
		if a == nil {
			m.screen = scrList
			return m, nil
		}
		before := a.Status
		a.Set(m.newStatus, m.note.Value(), m.cfg.Now())
		if err := track.Save(a.Dir, a.Record); err != nil {
			m.screen = scrList
			return m.fail(err)
		}
		if before == m.newStatus {
			m.setFlash("Note added.", false)
		} else {
			m.setFlash(fmt.Sprintf("%s: %s -> %s", label(*a), before, m.newStatus), false)
		}
		m.screen = m.back()
		return m, m.loadApps(a.Dir)
	}
	var cmd tea.Cmd
	m.note, cmd = m.note.Update(k)
	return m, cmd
}

func (m Model) startNew() (tea.Model, tea.Cmd) {
	p, err := profile.Load(m.cfg.ProfilePath)
	if err != nil {
		return m.fail(fmt.Errorf("%s: %w", m.cfg.ProfilePath, err))
	}
	t, err := tailor.New(m.cfg.NewClient(m.cfg.Model, m.cfg.Effort), p)
	if err != nil {
		return m.fail(err)
	}
	m.profile, m.tailor = p, t
	m.post, m.analysis, m.answers = nil, nil, nil
	m.flash = ""
	m.screen = scrPosting
	m.input.Reset()
	m.input.Placeholder = "https://... job posting link, or path/to/posting.txt"
	return m, m.input.Focus()
}

func (m Model) backToPosting(err error) (tea.Model, tea.Cmd) {
	m.stopWork()
	m.screen = scrPosting
	m.setFlash(err.Error(), true)
	return m, m.input.Focus()
}

func (m Model) postingKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.input.Blur()
		m.screen = scrList
		return m, nil
	case "enter":
		src := strings.Trim(strings.TrimSpace(m.input.Value()), `"'`)
		if src == "" {
			return m, nil
		}
		m.input.Blur()
		m.flash = ""
		m.screen = scrWorking
		m.working = "Reading the posting..."
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			m.working = "Fetching " + src + " ..."
		}
		m.stopWork()
		return m, tea.Batch(m.spin.Tick, m.fetchPosting(m.ctx(), src))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) questionKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	qs := m.analysis.Questions
	switch k.String() {
	case "esc":
		// Skip the rest of the questions.
		return m.afterQuestions()
	case "enter":
		q := qs[m.question]
		if text := strings.TrimSpace(m.input.Value()); text != "" {
			m.answers = append(m.answers, tailor.Answer{QuestionID: q.ID, Question: q.Text, Text: text})
		}
		m.question++
		m.input.Reset()
		if m.question >= len(qs) {
			return m.afterQuestions()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) afterQuestions() (tea.Model, tea.Cmd) {
	m.input.Blur()
	if len(m.answers) > 0 {
		m.screen = scrLearn
		return m, nil
	}
	return m.startWriting()
}

func (m Model) learnKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(k.String()) {
	case "y", "enter":
		m.screen = scrWorking
		m.working = "Saving your answers..."
		return m, tea.Batch(m.spin.Tick, m.learn())
	case "n":
		return m.startWriting()
	}
	return m, nil
}

func (m Model) startWriting() (tea.Model, tea.Cmd) {
	m.screen = scrWorking
	m.working = "Writing your resume and cover letter..."
	return m, tea.Batch(m.spin.Tick, m.write(m.ctx()))
}

func (m Model) doneKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := m.result
	switch k.String() {
	case "o":
		return m, m.open(firstExisting(r.dir, "resume.pdf", "resume.md"))
	case "c":
		return m, m.open(firstExisting(r.dir, "cover-letter.pdf", "cover-letter.md"))
	case "f":
		return m, m.open(r.dir)
	case "enter", "esc", "q":
		m.screen = scrList
	}
	return m, nil
}

// ctx returns the context for the in-flight new-application work, starting
// one if needed. Esc or Ctrl+C cancels it.
func (m *Model) ctx() context.Context {
	if m.workCtx == nil {
		m.workCtx, m.cancel = context.WithCancel(context.Background())
	}
	return m.workCtx
}

func (m *Model) stopWork() {
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel, m.workCtx = nil, nil
}

func (m Model) fail(err error) (tea.Model, tea.Cmd) {
	m.setFlash(err.Error(), true)
	return m, nil
}

func (m *Model) setFlash(s string, isErr bool) {
	m.flash, m.flashErr = s, isErr
}

func (m Model) selected() *track.App {
	if m.cursor < 0 || m.cursor >= len(m.apps) {
		return nil
	}
	return &m.apps[m.cursor]
}

func clamp(i, n int) int {
	if n == 0 || i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func label(a track.App) string {
	if a.Company != "" {
		return a.Company
	}
	return a.Name
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
