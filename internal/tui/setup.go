package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/config"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
)

type setupStep int

const (
	stepWelcome       setupStep = iota // offer to import files from an older version
	stepKey                            // API key
	stepModel                          // model picker
	stepModelOther                     // typing a model ID
	stepDataDir                        // where to keep the profile and applications
	stepProfile                        // no profile yet: paste a resume or use a file
	stepProfilePaste                   // pasting resume text
	stepProfilePath                    // path to an existing profile
	stepProfileReview                  // reviewing the drafted profile
)

type setupState struct {
	step     setupStep
	found    imports
	doImport bool
	draft    config.Config
	profile  *profile.Profile
	notes    []string
}

// imports are files from an older version found in the starting folder.
type imports struct {
	envKey   string // an API key from .env
	profile  string // path to profile.json
	apps     string // path to applications/
	appCount int
}

func (i imports) any() bool { return i.envKey != "" || i.profile != "" || i.appCount > 0 }

// findImports looks in dir for a .env with an API key, a profile.json and an
// applications folder.
func findImports(dir string) imports {
	var f imports
	if dir == "" {
		return f
	}
	if file, err := os.Open(filepath.Join(dir, ".env")); err == nil {
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export ")
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "ANTHROPIC_API_KEY" {
				v = strings.Trim(strings.TrimSpace(v), `"'`)
				if strings.HasPrefix(v, "sk-") {
					f.envKey = v
				}
			}
		}
		file.Close()
	}
	if p := filepath.Join(dir, "profile.json"); fileExists(p) {
		if _, err := profile.Load(p); err == nil {
			f.profile = p
		}
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "applications")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				f.appCount++
			}
		}
		if f.appCount > 0 {
			f.apps = filepath.Join(dir, "applications")
		}
	}
	return f
}

// enterStep moves setup to step and prepares its input.
func (m Model) enterStep(step setupStep) (tea.Model, tea.Cmd) {
	m.setup.step = step
	m.screen = scrSetup
	return m, m.focusSetupInput()
}

func (m *Model) focusSetupInput() tea.Cmd {
	m.textIn.Blur()
	m.area.Blur()
	m.textIn.EchoMode = textinput.EchoNormal
	switch m.setup.step {
	case stepWelcome:
		if !m.setup.found.any() {
			m.setup.step = stepKey
			return m.focusSetupInput()
		}
	case stepKey:
		m.textIn.Reset()
		m.textIn.EchoMode = textinput.EchoPassword
		m.textIn.Placeholder = "sk-ant-..."
		if m.setup.doImport && m.setup.found.envKey != "" {
			m.textIn.SetValue(m.setup.found.envKey)
		}
		return m.textIn.Focus()
	case stepModel:
		var ids, notes []string
		for _, mo := range config.Models(m.setup.draft.Provider) {
			ids, notes = append(ids, mo.ID), append(notes, mo.Note)
		}
		m.picker = newPicker(append(ids, "Other..."), append(notes, "type a model ID"), 0)
	case stepModelOther:
		m.textIn.Reset()
		m.textIn.Placeholder = "model ID, e.g. claude-opus-5-5"
		return m.textIn.Focus()
	case stepDataDir:
		m.textIn.Reset()
		m.textIn.SetValue(config.DefaultDataDir())
		return m.textIn.Focus()
	case stepProfile:
		m.picker = newPicker([]string{
			"Paste my resume or LinkedIn profile",
			"Use an existing profile.json file",
		}, []string{"Claude drafts your profile from it", ""}, 0)
	case stepProfilePaste:
		m.area.Reset()
		m.area.Placeholder = "Paste your resume or LinkedIn profile here (copy all the text from the page or document)"
		return m.area.Focus()
	case stepProfilePath:
		m.textIn.Reset()
		m.textIn.Placeholder = "path/to/profile.json"
		return m.textIn.Focus()
	}
	return nil
}

func (m Model) setupKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.setup
	switch s.step {
	case stepWelcome:
		switch strings.ToLower(k.String()) {
		case "y", "enter":
			s.doImport = true
			return m.enterStep(stepKey)
		case "n":
			s.doImport = false
			return m.enterStep(stepKey)
		}
		return m, nil

	case stepKey:
		if k.String() != "enter" {
			return m.updateTextIn(k)
		}
		key := strings.TrimSpace(m.textIn.Value())
		p, err := config.DetectProvider(key)
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		if !config.Supported(p) {
			m.setFlash("That's an OpenAI key. OpenAI support is coming in the next version; for now, use an Anthropic key from console.anthropic.com.", true)
			return m, nil
		}
		m.flash = ""
		s.draft.Provider, s.draft.APIKey = p, key
		ctx := m.startWork("Checking your API key...")
		check := m.cfg.CheckKey
		return m, tea.Batch(m.spin.Tick, func() tea.Msg { return keyCheckedMsg{err: check(ctx, p, key)} })

	case stepModel:
		if !m.picker.update(k) {
			return m, nil
		}
		models := config.Models(s.draft.Provider)
		if m.picker.cursor >= len(models) {
			return m.enterStep(stepModelOther)
		}
		s.draft.Model = models[m.picker.cursor].ID
		return m.enterStep(stepDataDir)

	case stepModelOther:
		switch k.String() {
		case "esc":
			return m.enterStep(stepModel)
		case "enter":
			if id := strings.TrimSpace(m.textIn.Value()); id != "" {
				s.draft.Model = id
				return m.enterStep(stepDataDir)
			}
			return m, nil
		}
		return m.updateTextIn(k)

	case stepDataDir:
		if k.String() != "enter" {
			return m.updateTextIn(k)
		}
		return m.finishConfig(expandHome(strings.TrimSpace(m.textIn.Value())))

	case stepProfile:
		if !m.picker.update(k) {
			return m, nil
		}
		if m.picker.cursor == 0 {
			return m.enterStep(stepProfilePaste)
		}
		return m.enterStep(stepProfilePath)

	case stepProfilePaste:
		switch k.String() {
		case "esc":
			return m.enterStep(stepProfile)
		case "ctrl+s":
			text := m.area.Value()
			if strings.TrimSpace(text) == "" {
				return m, nil
			}
			ctx := m.startWork("Drafting your profile from that text. This takes a minute or two...")
			client := m.cfg.NewClient(m.conf)
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				p, notes, err := tailor.DraftProfile(ctx, client, text)
				return profileDraftedMsg{profile: p, notes: notes, err: err}
			})
		}
		var cmd tea.Cmd
		m.area, cmd = m.area.Update(k)
		return m, cmd

	case stepProfilePath:
		switch k.String() {
		case "esc":
			return m.enterStep(stepProfile)
		case "enter":
			src := strings.Trim(strings.TrimSpace(m.textIn.Value()), `"'`)
			if _, err := profile.Load(src); err != nil {
				m.setFlash(err.Error(), true)
				return m, nil
			}
			if err := copyFile(src, m.profilePath()); err != nil {
				m.setFlash(err.Error(), true)
				return m, nil
			}
			return m.finishSetup("Profile copied to " + m.profilePath() + ".")
		}
		return m.updateTextIn(k)

	case stepProfileReview:
		switch k.String() {
		case "enter", "y":
			if err := profile.Save(m.profilePath(), s.profile); err != nil {
				m.setFlash(err.Error(), true)
				return m, nil
			}
			return m.finishSetup("Profile saved to " + m.profilePath() + ". Edit it any time to add detail.")
		case "esc":
			return m.enterStep(stepProfilePaste)
		}
	}
	return m, nil
}

func (m Model) updateTextIn(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textIn, cmd = m.textIn.Update(k)
	return m, cmd
}

type (
	keyCheckedMsg     struct{ err error }
	profileDraftedMsg struct {
		profile *profile.Profile
		notes   []string
		err     error
	}
)

func (m Model) gotKeyCheck(msg keyCheckedMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrWorking {
		return m, nil
	}
	m.stopWork()
	if msg.err != nil {
		m.setFlash("That key didn't work: "+msg.err.Error(), true)
		return m.enterStep(stepKey)
	}
	m.setFlash("Key works.", false)
	return m.enterStep(stepModel)
}

// finishConfig saves the settings, imports older files if asked, and moves
// on to the profile if there isn't one.
func (m Model) finishConfig(dataDir string) (tea.Model, tea.Cmd) {
	if dataDir == "" {
		return m, nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		m.setFlash(err.Error(), true)
		return m, nil
	}
	c := m.setup.draft
	c.DataDir, c.Effort = dataDir, "high"
	if err := config.Save(&c); err != nil {
		m.setFlash("Couldn't save settings: "+err.Error(), true)
		return m, nil
	}
	m.applyOverrides(&c)
	m.conf = &c

	var notes []string
	if m.setup.doImport {
		f := m.setup.found
		if f.profile != "" && !fileExists(m.profilePath()) {
			if err := copyFile(f.profile, m.profilePath()); err != nil {
				notes = append(notes, "couldn't import profile.json: "+err.Error())
			} else {
				notes = append(notes, "imported profile.json")
			}
		}
		if f.apps != "" {
			n, err := copyApps(f.apps, m.appsDir())
			if err != nil {
				notes = append(notes, "couldn't import applications: "+err.Error())
			} else if n > 0 {
				notes = append(notes, fmt.Sprintf("imported %s", plural(n, "application")))
			}
		}
	}
	msg := "Settings saved."
	if len(notes) > 0 {
		msg += " " + strings.ToUpper(notes[0][:1]) + notes[0][1:]
		if len(notes) > 1 {
			msg += "; " + strings.Join(notes[1:], "; ")
		}
		msg += "."
	}
	if !fileExists(m.profilePath()) {
		m.setFlash(msg, false)
		return m.enterStep(stepProfile)
	}
	return m.finishSetup(msg)
}

func (m Model) gotProfileDraft(msg profileDraftedMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrWorking {
		return m, nil
	}
	m.stopWork()
	if msg.err != nil {
		m.setFlash(msg.err.Error(), true)
		m.setup.step = stepProfilePaste
		m.screen = scrSetup
		return m, m.area.Focus() // keep the pasted text
	}
	m.setup.profile, m.setup.notes = msg.profile, msg.notes
	m.flash = ""
	return m.enterStep(stepProfileReview)
}

func (m Model) finishSetup(msg string) (tea.Model, tea.Cmd) {
	m.setup = setupState{}
	m.screen = scrHome
	m.setFlash(msg, false)
	return m, m.loadApps("")
}

func (m Model) setupView() (string, string) {
	s := m.setup
	var b strings.Builder
	switch s.step {
	case stepWelcome:
		f := s.found
		b.WriteString(bold.Render("Welcome to rezgen.") + " Found files from an earlier version in this folder:\n\n")
		if f.envKey != "" {
			b.WriteString("  • an API key in .env (" + config.Mask(f.envKey) + ")\n")
		}
		if f.profile != "" {
			b.WriteString("  • " + f.profile + "\n")
		}
		if f.appCount > 0 {
			b.WriteString(fmt.Sprintf("  • %s in %s\n", plural(f.appCount, "application"), f.apps))
		}
		b.WriteString("\nImport them? They're copied, so the originals stay where they are.")
		return b.String(), "y/enter import · n start fresh"

	case stepKey:
		b.WriteString(bold.Render("Welcome to rezgen.") + " First, your API key.\n\n")
		b.WriteString("rezgen uses Claude to tailor your resume. Create a key at console.anthropic.com\n")
		b.WriteString("(Settings > API keys) and paste it here. It's stored in your user settings only.\n\n")
		b.WriteString(m.textIn.View())
		return b.String(), "enter check key · ctrl+c quit"

	case stepModel:
		b.WriteString(bold.Render("Which model should write your documents?") + "\n\n" + m.picker.view())
		b.WriteString("\n\n" + faint.Render("You can change this later in settings."))
		return b.String(), "↑/↓ or number choose · enter select"

	case stepModelOther:
		return bold.Render("Model ID") + "\n\n" + m.textIn.View(), "enter continue · esc back"

	case stepDataDir:
		b.WriteString(bold.Render("Where should rezgen keep your profile and applications?") + "\n\n")
		b.WriteString(m.textIn.View() + "\n\n")
		b.WriteString(faint.Render("Your profile goes in profile.json and each application gets a folder under applications/."))
		return b.String(), "enter continue"

	case stepProfile:
		b.WriteString(bold.Render("Now your profile.") + " It's everything rezgen knows about you: jobs, accomplishments,\n")
		b.WriteString("skills and education. Every resume is drawn from it, and nothing is ever made up.\n\n")
		b.WriteString(m.picker.view())
		return b.String(), "↑/↓ or number choose · enter select"

	case stepProfilePaste:
		b.WriteString(bold.Render("Paste your resume or LinkedIn profile") + "\n")
		b.WriteString(faint.Render("Include everything: every job, dates, accomplishments, skills, education. More detail makes better resumes.") + "\n\n")
		b.WriteString(m.area.View())
		return b.String(), "ctrl+s draft my profile · esc back"

	case stepProfilePath:
		return bold.Render("Path to your profile.json") + "\n\n" + m.textIn.View(), "enter use this file · esc back"

	case stepProfileReview:
		p := s.profile
		highlights := 0
		for _, e := range p.Experience {
			highlights += len(e.Highlights)
		}
		b.WriteString(bold.Render("Here's your draft profile") + "\n\n")
		b.WriteString(fmt.Sprintf("  %s, %s\n", p.Contact.Name, p.Contact.Email))
		b.WriteString(fmt.Sprintf("  %s, %s, %s, %s\n",
			plural(len(p.Experience), "role"), plural(highlights, "highlight"),
			plural(len(p.Projects), "project"), plural(len(p.Certifications), "certification")))
		for _, e := range p.Experience {
			end := "present"
			if e.End != nil {
				end = *e.End
			}
			b.WriteString(faint.Render(fmt.Sprintf("    %s, %s (%s to %s)", e.Title, e.Company, e.Start, end)) + "\n")
		}
		if len(s.notes) > 0 {
			b.WriteString("\n" + bold.Render("Worth adding") + faint.Render(" (edit profile.json any time)") + "\n")
			for _, n := range s.notes {
				b.WriteString("  • " + n + "\n")
			}
		}
		b.WriteString("\nSave it to " + m.profilePath() + "?")
		return b.String(), "enter save · esc back to the text"
	}
	return "", ""
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyApps copies each application folder under src into dst, skipping any
// that already exist there, and returns how many it copied.
func copyApps(src, dst string) (int, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		to := filepath.Join(dst, e.Name())
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := copyDir(filepath.Join(src, e.Name()), to); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}
