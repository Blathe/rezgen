package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/store"
	"github.com/Blathe/rezgen/internal/tailor"
)

func (m Model) openApp(a *store.Application) (tea.Model, tea.Cmd) {
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
	st := m.store()
	generated := a.Status() == store.Generated
	switch k.String() {
	case "esc", "q", "left", "h", "backspace":
		m.banner = nil
		m.screen = scrHome
		return m, m.loadApps(a.ID)
	case "g":
		if generated {
			prompt := fmt.Sprintf("Regenerate %s?\n\nThis replaces the current resume and cover letter", bold.Render(a.Name))
			if a.ResumeEdit != "" || a.CoverLetterEdit != "" {
				prompt += ", including the edits you made"
			}
			return m.ask(prompt+".", func(m Model) (tea.Model, tea.Cmd) { return m.startGenerate() })
		}
		return m.startGenerate()
	case "t":
		return m.showDoc("Posting: "+a.Name, a.Posting)
	case "p":
		if strings.HasPrefix(a.Source, "http") {
			return m, m.open(a.Source)
		}
		return m.showDoc("Posting: "+a.Name, a.Posting)
	case "d":
		return m.askDelete(a)
	}
	if !generated {
		return m, nil
	}
	switch k.String() {
	case "o":
		return m.openPDF(st.ResumePDF(a))
	case "c":
		return m.openPDF(st.CoverLetterPDF(a))
	case "f":
		return m.openPDF(st.DocsDir(a))
	case "e":
		return m.edit(editResume)
	case "l":
		return m.edit(editCoverLetter)
	case "v":
		if a.Draft == nil {
			m.setFlash("No sources to show: these documents were imported from an earlier version.", true)
			return m, nil
		}
		text := tailor.RenderSources(a.Draft)
		if a.ResumeEdit != "" || a.CoverLetterEdit != "" {
			text = "Note: you've edited these documents since they were generated; this shows the generated version.\n\n" + text
		}
		return m.showDoc("Sources: where each line came from", text)
	}
	return m, nil
}

// openPDF opens a document, exporting the PDFs first if they're missing
// (for example after a -no-pdf run, or if the files were deleted).
func (m Model) openPDF(path string) (tea.Model, tea.Cmd) {
	if !m.store().HasPDFs(m.app) {
		p, err := m.loadProfile()
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		if _, err := m.store().Export(m.app, p); err != nil {
			m.setFlash("Couldn't build the PDFs: "+err.Error(), true)
			return m, nil
		}
	}
	return m, m.open(path)
}

func (m Model) appView() (string, string) {
	a := m.app
	if a == nil {
		return "", ""
	}
	st := m.store()
	w := m.innerWidth()
	var b strings.Builder
	b.WriteString(bold.Render(clip(a.Name, w)) + "\n")
	switch {
	case a.Role != "" && a.Company != "":
		b.WriteString(faint.Render(clip(a.Role+" at "+a.Company, w)) + "\n")
	case a.Role+a.Company != "":
		b.WriteString(faint.Render(clip(a.Role+a.Company, w)) + "\n")
	}
	b.WriteString("\n")
	status := statusPill(a.Status())
	if a.Generated != nil {
		status += faint.Render("  on " + a.Generated.Format("2006-01-02"))
	}
	if a.ResumeEdit != "" || a.CoverLetterEdit != "" {
		status += faint.Render(", edited")
	}
	b.WriteString(field("Status", status) + "\n")
	b.WriteString(field("Added", a.Created.Format("2006-01-02")) + "\n")
	if strings.HasPrefix(a.Source, "http") {
		b.WriteString(field("Posting", link(a.Source, accent.Render(clip(a.Source, w-10)))) + "\n")
	}

	var help string
	if a.Status() == store.Generated {
		dir := st.DocsDir(a)
		b.WriteString(field("Folder", link(fileURL(dir), clipPath(dir, w-10))) + "\n")
		if st.HasPDFs(a) {
			files := link(fileURL(st.ResumePDF(a)), accent.Render("resume.pdf")) + "  " +
				link(fileURL(st.CoverLetterPDF(a)), accent.Render("cover-letter.pdf"))
			b.WriteString(field("Files", files) + "\n")
		}
		help = "o open resume · c open cover letter · e edit resume · l edit letter · v sources\n" +
			"g regenerate · t view posting · f folder · d delete · esc back"
	} else {
		if a.Rejected != nil {
			b.WriteString("\n" + errStyle.Render("The last draft failed the source checks:") + "\n")
			for _, p := range a.Rejected.Problems {
				b.WriteString(faint.Render("  • "+clip(p, w-4)) + "\n")
			}
			b.WriteString("\nPress " + keyCap.Render("g") + " to try again.\n")
		} else {
			b.WriteString("\nPress " + keyCap.Render("g") + " to generate a tailored resume and cover letter for this posting.\n")
		}
		help = "g generate · t view posting · p open posting · d delete · esc back"
	}

	if len(m.banner) > 0 {
		p := okPanel
		if m.bannerErr {
			p = errPanel
		}
		b.WriteString("\n" + p.Width(w-2).Render(strings.Join(m.banner, "\n")) + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), help
}

// Editing: write the document to a temporary Markdown file, open it in an
// editor, then save the result as a hand edit and rebuild the PDFs.

type editKind int

const (
	editResume editKind = iota
	editCoverLetter
)

type editedMsg struct {
	kind   editKind
	path   string // the temporary file
	before string // its contents before editing
	err    error
}

func (m Model) edit(kind editKind) (tea.Model, tea.Cmd) {
	p, err := m.loadProfile()
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	name, text := "resume", m.app.ResumeMarkdown(p)
	if kind == editCoverLetter {
		name, text = "cover-letter", m.app.CoverLetterMarkdown(p)
	}
	f, err := os.CreateTemp("", "rezgen-"+name+"-*.md")
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		m.setFlash(err.Error(), true)
		return m, nil
	}
	path := f.Name()
	return m, tea.ExecProcess(m.cfg.Editor(path), func(err error) tea.Msg {
		return editedMsg{kind: kind, path: path, before: text, err: err}
	})
}

func (m Model) gotEdited(msg editedMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.setFlash("Editor: "+msg.err.Error(), true)
		return m, nil
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	text := string(data)
	if text == msg.before {
		m.setFlash("No changes.", false)
		return m, nil
	}
	if strings.TrimSpace(text) == "" {
		m.setFlash("The document was empty, so the edit wasn't saved.", true)
		return m, nil
	}
	a, st := m.app, m.store()
	if msg.kind == editResume {
		a.ResumeEdit = text
	} else {
		a.CoverLetterEdit = text
	}
	if err := st.Save(a); err != nil {
		m.setFlash("Couldn't save the edit: "+err.Error(), true)
		return m, nil
	}
	m.banner = nil
	if m.cfg.NoPDF {
		m.setFlash("Edit saved.", false)
		return m, nil
	}
	p, err := m.loadProfile()
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	pages, err := st.Export(a, p)
	if err != nil {
		m.setFlash("Edit saved, but the PDFs couldn't be rebuilt: "+err.Error(), true)
		return m, nil
	}
	m.setFlash(fmt.Sprintf("Edit saved and PDFs rebuilt (resume: %s).", plural(pages, "page")), false)
	return m, nil
}

// Generation: analyze, ask, write, export the PDFs.

type genState struct {
	active   bool
	profile  *profile.Profile
	tailor   *tailor.Tailor
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
	// writtenMsg carries an updated copy of the application, so background
	// work never changes the one the screen is drawing.
	writtenMsg struct {
		app      *store.Application
		rejected *tailor.DraftError
		err      error
	}
	pdfMsg struct {
		pages int
		err   error
	}
)

func (m Model) startGenerate() (tea.Model, tea.Cmd) {
	t, p, err := m.newTailor()
	if err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	m.banner = nil
	m.gen = genState{active: true, profile: p, tailor: t}
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
	text := m.app.Posting
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
	g, st, on := m.gen, m.store(), m.cfg.Now()
	app := *m.app
	ctx := m.ctx()
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		d, err := g.tailor.Write(ctx, app.Posting, g.analysis, g.answers)
		var de *tailor.DraftError
		if err != nil && !errors.As(err, &de) {
			return writtenMsg{err: err}
		}
		res := store.Results{Analysis: g.analysis, Answers: g.answers, Draft: d, Rejected: de}
		if err := st.SetResults(&app, res, on); err != nil {
			return writtenMsg{err: err}
		}
		return writtenMsg{app: &app, rejected: de}
	})
}

func (m Model) gotWritten(msg writtenMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active || m.screen != scrWorking {
		return m, nil
	}
	if msg.err != nil {
		return m.genFailed(msg.err)
	}
	m.app = msg.app
	if msg.rejected != nil || m.cfg.NoPDF {
		return m.finishGen(msg.rejected, 0, nil)
	}
	m.steps[genWrite].state = stepDone
	m.steps[genPDF].state = stepRunning
	m.started = m.cfg.Now()
	st, p, app := m.store(), m.gen.profile, *m.app
	return m, func() tea.Msg {
		pages, err := st.Export(&app, p)
		return pdfMsg{pages: pages, err: err}
	}
}

func (m Model) gotPDF(msg pdfMsg) (tea.Model, tea.Cmd) {
	if !m.gen.active || m.screen != scrWorking {
		return m, nil
	}
	return m.finishGen(nil, msg.pages, msg.err)
}

// finishGen returns to the application screen with the outcome.
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
		m.bannerErr = true
		m.banner = []string{
			errStyle.Bold(true).Render("✗ The draft failed the source checks twice, so no documents were made."),
			"The problems are listed above. Press " + keyCap.Render("g") + " to try again.",
		}
		return m, m.loadApps(a.ID)
	}
	m.bannerErr = false
	m.banner = []string{
		okStyle.Bold(true).Render("✓ Done! Your resume and cover letter are ready."),
		"Press " + keyCap.Render("o") + " to open the resume or " + keyCap.Render("c") + " for the cover letter, or click the files above.",
	}
	if strings.HasPrefix(a.Source, "http") {
		m.banner = append(m.banner, "Apply at "+link(a.Source, accent.Render(clip(a.Source, m.innerWidth()-14))))
	}
	if pdfErr != nil {
		m.bannerErr = true
		m.banner = append(m.banner, errStyle.Render("PDF export failed: "+pdfErr.Error()))
	}
	if pages > 0 && maxPages > 0 && pages > maxPages {
		m.banner = append(m.banner, errStyle.Render(fmt.Sprintf("The resume is %d pages; your profile asks for %d. Press e to trim it.", pages, maxPages)))
	}
	return m, m.loadApps(a.ID)
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
	b.WriteString(bold.Render(clip(m.app.Name, m.innerWidth())) + "\n")
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
