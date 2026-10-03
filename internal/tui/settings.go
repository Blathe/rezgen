package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/config"
)

type settingsMode int

const (
	settingsMain settingsMode = iota
	settingsModel
	settingsModelOther
	settingsKey
)

type settingsState struct {
	mode settingsMode
}

func (m Model) openSettings() (tea.Model, tea.Cmd) {
	m.flash = ""
	m.settings = settingsState{}
	m.screen = scrSettings
	return m, nil
}

func (m Model) settingsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.settings
	switch s.mode {
	case settingsMain:
		m.flash = ""
		switch k.String() {
		case "esc", "q":
			m.screen = scrHome
			return m, m.loadApps("")
		case "m":
			var ids, notes []string
			cur := len(config.Models(m.conf.Provider))
			for i, mo := range config.Models(m.conf.Provider) {
				ids, notes = append(ids, mo.ID), append(notes, mo.Note)
				if mo.ID == m.conf.Model {
					cur = i
				}
			}
			m.picker = newPicker(append(ids, "Other..."), append(notes, "type a model ID"), cur)
			s.mode = settingsModel
		case "k":
			s.mode = settingsKey
			m.textIn.Reset()
			m.textIn.EchoMode = textinput.EchoPassword
			m.textIn.Placeholder = "sk-ant-..."
			return m, m.textIn.Focus()
		case "f":
			return m, m.open(m.conf.DataDir)
		case "p":
			return m, m.open(m.profilePath())
		}
		return m, nil

	case settingsModel:
		if k.String() == "esc" {
			s.mode = settingsMain
			return m, nil
		}
		if !m.picker.update(k) {
			return m, nil
		}
		models := config.Models(m.conf.Provider)
		if m.picker.cursor >= len(models) {
			s.mode = settingsModelOther
			m.textIn.Reset()
			m.textIn.EchoMode = textinput.EchoNormal
			m.textIn.SetValue(m.conf.Model)
			return m, m.textIn.Focus()
		}
		return m.saveModel(models[m.picker.cursor].ID)

	case settingsModelOther:
		switch k.String() {
		case "esc":
			s.mode = settingsMain
			m.textIn.Blur()
			return m, nil
		case "enter":
			if id := strings.TrimSpace(m.textIn.Value()); id != "" {
				m.textIn.Blur()
				return m.saveModel(id)
			}
			return m, nil
		}
		return m.updateTextIn(k)

	case settingsKey:
		switch k.String() {
		case "esc":
			s.mode = settingsMain
			m.textIn.Blur()
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.textIn.Value())
			p, err := config.DetectProvider(key)
			if err != nil {
				m.setFlash(err.Error(), true)
				return m, nil
			}
			if !config.Supported(p) {
				m.setFlash("OpenAI keys aren't supported yet; that's coming in the next version.", true)
				return m, nil
			}
			m.textIn.Blur()
			ctx := m.startWork("Checking your API key...")
			check := m.cfg.CheckKey
			return m, tea.Batch(m.spin.Tick, func() tea.Msg {
				err := check(ctx, p, key)
				return settingsKeyMsg{key: key, provider: p, err: err}
			})
		}
		return m.updateTextIn(k)
	}
	return m, nil
}

type settingsKeyMsg struct {
	key      string
	provider config.Provider
	err      error
}

func (m Model) gotSettingsKey(msg settingsKeyMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrWorking {
		return m, nil
	}
	m.stopWork()
	m.screen = scrSettings
	m.settings.mode = settingsMain
	if msg.err != nil {
		m.setFlash("That key didn't work: "+msg.err.Error(), true)
		return m, nil
	}
	m.conf.APIKey, m.conf.Provider = msg.key, msg.provider
	return m.saveConf("API key updated.")
}

func (m Model) saveModel(id string) (tea.Model, tea.Cmd) {
	m.settings.mode = settingsMain
	m.conf.Model = id
	return m.saveConf("Model set to " + id + ".")
}

func (m Model) saveConf(msg string) (tea.Model, tea.Cmd) {
	if err := config.Save(m.conf); err != nil {
		m.setFlash("Couldn't save settings: "+err.Error(), true)
		return m, nil
	}
	m.setFlash(msg, false)
	return m, nil
}

func (m Model) settingsView() (string, string) {
	c := m.conf
	switch m.settings.mode {
	case settingsModel:
		return bold.Render("Model") + "\n\n" + m.picker.view(), "↑/↓ or number choose · enter select · esc back"
	case settingsModelOther:
		return bold.Render("Model ID") + "\n\n" + m.textIn.View(), "enter save · esc back"
	case settingsKey:
		return bold.Render("New API key") + "\n\n" + m.textIn.View(), "enter check and save · esc back"
	}
	path, _ := config.Path()
	var b strings.Builder
	b.WriteString(bold.Render("Settings") + "\n\n")
	row := func(label, value string) { b.WriteString(faint.Render(label) + value + "\n") }
	row("Provider      ", string(c.Provider))
	row("Model         ", c.Model)
	row("API key       ", config.Mask(c.APIKey))
	row("Profile       ", link(fileURL(m.profilePath()), m.profilePath()))
	row("Applications  ", link(fileURL(m.appsDir()), m.appsDir()))
	row("Settings file ", link(fileURL(path), path))
	b.WriteString("\n" + faint.Render("To move your data, move the folder and change data_dir in the settings file."))
	return b.String(), "m change model · k change API key · p open profile · f open data folder · esc back"
}
