package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/pdf"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
	"github.com/Blathe/rezgen/internal/track"
)

func (m Model) openApp(a *track.App) (tea.Model, tea.Cmd) {
	m.app = a
	m.banner = nil
	m.flash = ""
	m.screen = scrApp
	return m, nil
}

func (m Model) appKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.app
	if a == nil {
		m.screen = scrHome
		return m, nil
	}
	m.flash = ""
	generated := a.Status == track.Generated
	switch k.String() {
	case "esc", "q", "left", "h", "backspace":
		m.banner = nil
		m.screen = scrHome
		return m, m.loadApps(a.Dir)
	case "g":
		if generated {
			return m.ask(fmt.Sprintf("Regenerate %s?\n\nThe current documents move to a v1, v2, ... subfolder, so nothing is lost.", bold.Render(a.Name)),
				func(m Model) (tea.Model, tea.Cmd) {
					if _, err := m.app.Archive(); err != nil {
						m.setFlash(err.Error(), true)
						return m, nil
					}
					return m.startGenerate()
				})
		}
		return m.startGenerate()
	case "t":
		text, err := a.Posting()
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		return m.showDoc("Posting: "+a.Name, text)
	case "p":
		if strings.HasPrefix(a.Source, "http") {
			return m, m.open(a.Source)
		}
		return m, m.open(filepath.Join(a.Dir, "posting.md"))
	case "f":
		return m, m.open(a.Dir)
	case "d":
		return m.askDelete(a)
	}
	if !generated {
		return m, nil
	}
	switch k.String() {
	case "o":
		return m, m.open(firstExisting(a.Dir, "resume.pdf", "resume.md"))
	case "c":
		return m, m.open(firstExisting(a.Dir, "cover-letter.pdf", "cover-letter.md"))
	case "e":
		return m.edit(filepath.Join(a.Dir, "resume.md"))
	case "l":
		return m.edit(filepath.Join(a.Dir, "cover-letter.md"))
	case "v":
		data, err := os.ReadFile(filepath.Join(a.Dir, "sources.md"))
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		return m.showDoc("Sources: where each line came from", string(data))
	}
	return m, nil
}

func (m Model) appView() (string, string) {
	a := m.app
	if a == nil {
		return "", ""
	}
	var b strings.Builder
	b.WriteString(bold.Render(a.Name) + "\n")
	switch {
	case a.Role != "" && a.Company != "":
		b.WriteString(faint.Render(a.Role+" at "+a.Company) + "\n")
	case a.Role+a.Company != "":
		b.WriteString(faint.Render(a.Role+a.Company) + "\n")
	}
	b.WriteString("\n")
	status := statusStyle(a.Status).Bold(true).Render(string(a.Status))
	if a.Status == track.Generated {
		status += faint.Render(" on " + a.Generated)
	}
	b.WriteString("Status:  " + status + "\n")
	b.WriteString("Added:   " + a.Created + "\n")
	if strings.HasPrefix(a.Source, "http") {
		b.WriteString("Posting: " + link(a.Source, clip(a.Source, 80)) + "\n")
	}
	b.WriteString("Folder:  " + link(fileURL(a.Dir), a.Dir) + "\n")

	if len(m.banner) > 0 {
		b.WriteString("\n" + panel.Render(strings.Join(m.banner, "\n")) + "\n")
	}

	var help string
	if a.Status == track.Generated {
		var files []string
		for _, f := range []string{"resume.pdf", "cover-letter.pdf", "resume.md", "cover-letter.md", "sources.md"} {
			if a.Has(f) {
				files = append(files, link(fileURL(filepath.Join(a.Dir, f)), f))
			}
		}
		if len(m.banner) == 0 && len(files) > 0 {
			b.WriteString("\nFiles:   " + strings.Join(files, faint.Render(", ")) + "\n")
		}
		if versions := oldVersions(a); len(versions) > 0 {
			b.WriteString(faint.Render("Earlier versions: "+strings.Join(versions, ", ")) + "\n")
		}
		help = "o open resume · c open cover letter · e edit resume · l edit letter · v sources\n" +
			"g regenerate · t view posting · f folder · d delete · esc back"
	} else {
		if a.Has("draft-rejected.json") {
			b.WriteString("\n" + errStyle.Render("The last attempt's draft failed the source checks; see draft-rejected.json. Press g to try again.") + "\n")
		} else {
			b.WriteString("\nPress " + bold.Render("g") + " to generate a tailored resume and cover letter for this posting.\n")
		}
		help = "g generate · t view posting · p open posting · f folder · d delete · esc back"
	}
	return strings.TrimRight(b.String(), "\n"), help
}

func oldVersions(a *track.App) []string {
	var out []string
	for n := 1; ; n++ {
		v := fmt.Sprintf("v%d", n)
		if st, err := os.Stat(filepath.Join(a.Dir, v)); err != nil || !st.IsDir() {
			return out
		}
		out = append(out, v)
	}
}

func firstExisting(dir string, names ...string) string {
	for _, n := range names {
		if p := filepath.Join(dir, n); fileExists(p) {
			return p
		}
	}
	return dir
}

// Editing: open the Markdown in an editor, then rebuild its PDF.

type editedMsg struct {
	path string
	err  error
}

func (m Model) edit(path string) (tea.Model, tea.Cmd) {
	cmd := m.cfg.Editor(path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{path: path, err: err} })
}

func (m Model) gotEdited(msg editedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setFlash("Editor: "+msg.err.Error(), true)
		return m, nil
	}
	if m.cfg.NoPDF {
		return m, nil
	}
	out, pages, err := pdf.ConvertFile(msg.path)
	if err != nil {
		m.setFlash("Couldn't rebuild the PDF: "+err.Error(), true)
		return m, nil
	}
	m.banner = nil
	m.setFlash(fmt.Sprintf("Rebuilt %s (%s).", filepath.Base(out), plural(pages, "page")), false)
	return m, nil
}

// Generation: analyze, ask, write, build PDFs.

type genState struct {
	active   bool
	profile  *profile.Profile
	tailor   *tailor.Tailor
	text     string
	analysis *tailor.Analysis
	answers  []tailor.Answer
	question int
}

const (
	genAnalyze = iota
	genQuestions
	genWrite
	genPDF
)

type (
	analyzedMsg struct {
		analysis *tailor.Analysis
		err      error
	}
	learnedMsg struct{ err error }
	writtenMsg struct {
		pages    int
		rejected *tailor.DraftError
		pdfErr   error
		err      error
	}
)

func (m Model) startGenerate() (tea.Model, tea.Cmd) {
	t, p, err := m.newTailor()
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	text, err := m.app.Posting()
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	m.banner = nil
	m.gen = genState{active: true, profile: p, tailor: t, text: text}
	ctx := m.startWork("")
	m.steps = []step{
		{label: "Analyze the posting against your profile", state: stepRunning},
		{label: "Questions"},
		{label: "Write the resume and cover letter, and check every line against your profile"},
		{label: "Build the PDFs"},
	}
	if m.cfg.NoPDF {
		m.steps[genPDF].state = stepSkipped
	}
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		a, err := t.Analyze(ctx, text)
		return analyzedMsg{analysis: a, err: err}
	})
}

func (m Model) gotAnalysis(msg analyzedMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active || m.screen != scrWorking {
		return m, nil
	}
	if msg.err != nil {
		return m.genFailed(msg.err)
	}
	a := msg.analysis
	m.gen.analysis = a
	m.steps[genAnalyze].state = stepDone
	m.steps[genAnalyze].note = fmt.Sprintf("%d matched, %d gaps", len(a.Matches), len(a.Gaps))
	if len(a.Questions) == 0 {
		m.steps[genQuestions].state = stepSkipped
		m.steps[genQuestions].note = "none needed"
		return m.startWriting()
	}
	m.screen = scrQuestion
	m.gen.question = 0
	m.textIn.Reset()
	m.textIn.EchoMode = textinput.EchoNormal
	m.textIn.Placeholder = "answer with facts, or press Enter to skip"
	return m, m.textIn.Focus()
}

func (m Model) questionKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.gen
	qs := g.analysis.Questions
	switch k.String() {
	case "esc":
		return m.afterQuestions()
	case "enter":
		q := qs[g.question]
		if text := strings.TrimSpace(m.textIn.Value()); text != "" {
			g.answers = append(g.answers, tailor.Answer{QuestionID: q.ID, Question: q.Text, Text: text})
		}
		g.question++
		m.textIn.Reset()
		if g.question >= len(qs) {
			return m.afterQuestions()
		}
		return m, nil
	}
	return m.updateTextIn(k)
}

func (m Model) afterQuestions() (tea.Model, tea.Cmd) {
	m.textIn.Blur()
	m.steps[genQuestions].state = stepDone
	m.steps[genQuestions].note = fmt.Sprintf("%s answered", plural(len(m.gen.answers), "question"))
	if len(m.gen.answers) > 0 {
		m.screen = scrLearn
		return m, nil
	}
	return m.startWriting()
}

func (m Model) learnKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(k.String()) {
	case "y", "enter":
		path, a, answers, on := m.profilePath(), m.gen.analysis, m.gen.answers, m.cfg.Now()
		m.screen = scrWorking
		return m, tea.Batch(m.spin.Tick, func() tea.Msg { return learnedMsg{err: tailor.LearnAnswers(path, a, answers, on)} })
	case "n":
		return m.startWriting()
	}
	return m, nil
}

func (m Model) gotLearned(msg learnedMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active {
		return m, nil
	}
	if msg.err != nil {
		m.setFlash("Couldn't save answers to your profile: "+msg.err.Error(), true)
	} else {
		m.steps[genQuestions].note += ", saved to your profile"
	}
	return m.startWriting()
}

func (m Model) startWriting() (tea.Model, tea.Cmd) {
	m.screen = scrWorking
	m.steps[genWrite].state = stepRunning
	m.started = m.cfg.Now()
	g, app, on := m.gen, m.app, m.cfg.Now()
	ctx := m.ctx()
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		d, err := g.tailor.Write(ctx, g.text, g.analysis, g.answers)
		var de *tailor.DraftError
		if err != nil && !errors.As(err, &de) {
			return writtenMsg{err: err}
		}
		res := tailor.Results{Analysis: g.analysis, Answers: g.answers, Draft: d}
		if err := tailor.WriteResults(app, g.profile, res, on); err != nil {
			return writtenMsg{err: err}
		}
		if de != nil {
			if err := tailor.SaveRejected(app.Dir, de); err != nil {
				return writtenMsg{err: err}
			}
		}
		return writtenMsg{rejected: de}
	})
}

type pdfMsg struct {
	pages int
	err   error
}

func (m Model) gotWritten(msg writtenMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active || m.screen != scrWorking {
		return m, nil
	}
	if msg.err != nil {
		return m.genFailed(msg.err)
	}
	if msg.rejected != nil || m.cfg.NoPDF {
		return m.finishGen(msg.rejected, 0, nil)
	}
	m.steps[genWrite].state = stepDone
	m.steps[genPDF].state = stepRunning
	m.started = m.cfg.Now()
	dir := m.app.Dir
	return m, func() tea.Msg {
		var msg pdfMsg
		for _, f := range []string{"resume.md", "cover-letter.md"} {
			_, pages, err := pdf.ConvertFile(filepath.Join(dir, f))
			if err != nil {
				msg.err = err
				break
			}
			if f == "resume.md" {
				msg.pages = pages
			}
		}
		return msg
	}
}

func (m Model) gotPDF(msg pdfMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active || m.screen != scrWorking {
		return m, nil
	}
	return m.finishGen(nil, msg.pages, msg.err)
}

// finishGen returns to the application screen with a summary of what was
// made and where.
func (m Model) finishGen(rejected *tailor.DraftError, pages int, pdfErr error) (tea.Model, tea.Cmd) {
	maxPages := 0
	if m.gen.profile != nil {
		maxPages = m.gen.profile.Preferences.MaxPages
	}
	m.stopWork()
	m.gen = genState{}
	m.steps = nil
	m.screen = scrApp
	a := m.app
	if rejected != nil {
		m.banner = []string{
			errStyle.Render("The draft failed the source checks twice, so no resume was made."),
		}
		for _, p := range rejected.Problems {
			m.banner = append(m.banner, "  • "+p)
		}
		m.banner = append(m.banner, "", "The rejected draft is in "+link(fileURL(a.Dir), a.Dir)+". Press g to try again.")
		return m, m.loadApps(a.Dir)
	}
	m.banner = []string{okStyle.Render("Done! Your documents are ready.")}
	for _, f := range []string{"resume.pdf", "cover-letter.pdf"} {
		if p := filepath.Join(a.Dir, f); fileExists(p) {
			m.banner = append(m.banner, "  "+link(fileURL(p), p))
		}
	}
	m.banner = append(m.banner, "  Folder: "+link(fileURL(a.Dir), a.Dir))
	if strings.HasPrefix(a.Source, "http") {
		m.banner = append(m.banner, "  Apply:  "+link(a.Source, clip(a.Source, 70)))
	}
	if pdfErr != nil {
		m.banner = append(m.banner, errStyle.Render("PDF export failed: "+pdfErr.Error()))
	}
	if pages > 0 && maxPages > 0 && pages > maxPages {
		m.banner = append(m.banner, errStyle.Render(fmt.Sprintf("The resume is %d pages; your profile asks for %d. Press e to trim it.", pages, maxPages)))
	}
	return m, m.loadApps(a.Dir)
}

func (m Model) genFailed(err error) (tea.Model, tea.Cmd) {
	m.stopWork()
	m.gen = genState{}
	m.steps = nil
	m.screen = scrApp
	if errors.Is(err, context.Canceled) {
		m.setFlash("Cancelled.", false)
	} else {
		m.setFlash(err.Error(), true)
	}
	return m, nil
}

func (m Model) questionView() string {
	g := m.gen
	a := g.analysis
	q := a.Questions[g.question]
	var b strings.Builder
	b.WriteString(bold.Render(m.app.Name) + "\n")
	b.WriteString(okStyle.Render(fmt.Sprintf("%d requirements matched", len(a.Matches))))
	if len(a.Gaps) > 0 {
		b.WriteString(faint.Render(" · ") + errStyle.Render(fmt.Sprintf("%d gaps: %s", len(a.Gaps), clip(strings.Join(a.Gaps, ", "), 80))))
	}
	b.WriteString("\n\n")
	b.WriteString(faint.Render(fmt.Sprintf("Question %d of %d", g.question+1, len(a.Questions))) + "\n")
	b.WriteString(bold.Render(q.Text) + "\n")
	if q.Why != "" {
		b.WriteString(faint.Render(q.Why) + "\n")
	}
	b.WriteString("\n" + m.textIn.View())
	return b.String()
}

func (m Model) learnView() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Save %s to your profile so rezgen won't ask again?\n\n", plural(len(m.gen.answers), "answer")))
	for _, an := range m.gen.answers {
		b.WriteString(faint.Render("  "+an.Question) + "\n  " + an.Text + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
