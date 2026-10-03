// Package tui is rezgen's interactive app. The first launch runs setup (API
// key, model, data folder, importing older files, and drafting a profile if
// there isn't one). After that it opens on the list of applications: add one
// by pasting a posting link or text, open it to generate a tailored resume
// and cover letter, and come back to the list.
//
// It drives the same packages as the command-line subcommands (config,
// track, tailor, posting, pdf), so the two always behave the same.
package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/config"
	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
	"github.com/Blathe/rezgen/internal/track"
)

// Config holds what the app needs from the command line. Everything else
// comes from the saved config (see package config).
type Config struct {
	// Overrides from command-line flags; empty means use the saved config.
	ProfilePath string
	OutDir      string
	Model       string
	Effort      string
	NoPDF       bool
	// ImportDir is searched on first run for files from older versions
	// (.env, profile.json, applications/). Usually the current directory.
	ImportDir string

	// Hooks for tests; nil means the real thing.
	NewClient func(c *config.Config) llm.Client
	CheckKey  func(ctx context.Context, p config.Provider, key string) error
	Loader    posting.Loader
	Now       func() time.Time
	Open      func(path string) error
	Editor    func(path string) *exec.Cmd
}

func (c *Config) defaults() {
	if c.NewClient == nil {
		c.NewClient = func(conf *config.Config) llm.Client {
			return llm.NewAnthropic(conf.Model, conf.Effort, option.WithAPIKey(conf.APIKey))
		}
	}
	if c.CheckKey == nil {
		c.CheckKey = func(ctx context.Context, p config.Provider, key string) error {
			if p != config.Anthropic {
				return errors.New("only Anthropic keys can be checked so far")
			}
			return llm.CheckAnthropicKey(ctx, key)
		}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Open == nil {
		c.Open = openFile
	}
	if c.Editor == nil {
		c.Editor = editorCmd
	}
}

// Run starts the app and blocks until the user quits.
func Run(cfg Config) error {
	_, err := tea.NewProgram(New(cfg), tea.WithAltScreen()).Run()
	return err
}

type screen int

const (
	scrSetup    screen = iota // first-run setup; see setup.go
	scrHome                   // the application list
	scrNew                    // pasting a posting link or text
	scrNewName                // naming the new application
	scrApp                    // one application
	scrConfirm                // a yes/no question
	scrViewer                 // scrolling through a document
	scrWorking                // waiting on the network or the model
	scrQuestion               // answering the analysis questions
	scrLearn                  // whether to save answers to the profile
	scrSettings               // settings
)

// Model is the Bubble Tea model for the whole app.
type Model struct {
	cfg           Config
	conf          *config.Config // nil until setup is done
	width, height int
	screen        screen
	flash         string
	flashErr      bool

	setup   setupState
	initCmd tea.Cmd

	apps   []*track.App
	cursor int
	app    *track.App // the application being viewed

	textIn  textinput.Model
	area    textarea.Model
	picker  picker
	spin    spinner.Model
	viewer  viewport.Model
	confirm confirmState

	// Background work.
	working   string
	steps     []step
	started   time.Time
	workCtx   context.Context
	cancel    context.CancelFunc
	newPost   *posting.Posting
	gen       genState
	settings  settingsState
	banner    []string // shown on the application screen after generating
	bannerErr bool     // whether the banner reports a failure
}

// New returns the initial model: setup on first run, otherwise the list.
func New(cfg Config) Model {
	cfg.defaults()
	ti := textinput.New()
	ti.CharLimit = 2000
	ta := textarea.New()
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(12)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accent
	m := Model{cfg: cfg, textIn: ti, area: ta, spin: sp, viewer: viewport.New(80, 20)}

	conf, err := config.Load()
	switch {
	case errors.Is(err, config.ErrNotFound):
		m.screen = scrSetup
		m.setup.found = findImports(cfg.ImportDir)
	case err != nil:
		m.screen = scrSetup
		m.setFlash("Couldn't read your settings, so let's set up again: "+err.Error(), true)
	default:
		m.applyOverrides(conf)
		m.conf = conf
		m.screen = scrHome
		if !fileExists(m.profilePath()) {
			m.screen = scrSetup
			m.setup.step = stepProfile
		}
	}
	if m.screen == scrSetup {
		// Prepare the first step's input here, on the real model; Init has a
		// value receiver, so changes made there would be lost.
		m.initCmd = m.focusSetupInput()
	}
	return m
}

func (m *Model) applyOverrides(c *config.Config) {
	if m.cfg.Model != "" {
		c.Model = m.cfg.Model
	}
	if m.cfg.Effort != "" {
		c.Effort = m.cfg.Effort
	}
}

func (m Model) profilePath() string {
	if m.cfg.ProfilePath != "" {
		return m.cfg.ProfilePath
	}
	return m.conf.ProfilePath()
}

func (m Model) appsDir() string {
	if m.cfg.OutDir != "" {
		return m.cfg.OutDir
	}
	return m.conf.ApplicationsDir()
}

func (m Model) Init() tea.Cmd {
	if m.screen == scrHome {
		return m.loadApps("")
	}
	return m.initCmd
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.area.SetWidth(max(40, min(100, msg.Width-6)))
		m.viewer.Width = max(40, msg.Width-6)
		m.viewer.Height = max(5, msg.Height-9)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.stopWork()
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
		return m.gotApps(msg)
	case keyCheckedMsg:
		return m.gotKeyCheck(msg)
	case settingsKeyMsg:
		return m.gotSettingsKey(msg)
	case profileDraftedMsg:
		return m.gotProfileDraft(msg)
	case postingMsg:
		return m.gotPosting(msg)
	case analyzedMsg:
		return m.gotAnalysis(msg)
	case learnedMsg:
		return m.gotLearned(msg)
	case writtenMsg:
		return m.gotWritten(msg)
	case pdfMsg:
		return m.gotPDF(msg)
	case editedMsg:
		return m.gotEdited(msg)
	case openedMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error(), true)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scrSetup:
		return m.setupKey(k)
	case scrHome:
		return m.homeKey(k)
	case scrNew:
		return m.newKey(k)
	case scrNewName:
		return m.newNameKey(k)
	case scrApp:
		return m.appKey(k)
	case scrConfirm:
		return m.confirmKey(k)
	case scrViewer:
		return m.viewerKey(k)
	case scrWorking:
		if k.String() == "esc" {
			return m.cancelWork()
		}
	case scrQuestion:
		return m.questionKey(k)
	case scrLearn:
		return m.learnKey(k)
	case scrSettings:
		return m.settingsKey(k)
	}
	return m, nil
}

// Messages from background work.
type (
	appsMsg struct {
		apps      []*track.App
		err       error
		selectDir string
	}
	openedMsg struct{ err error }
)

func (m Model) loadApps(selectDir string) tea.Cmd {
	root := m.appsDir()
	return func() tea.Msg {
		apps, err := track.List(root)
		return appsMsg{apps: apps, err: err, selectDir: selectDir}
	}
}

func (m Model) gotApps(msg appsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setFlash(msg.err.Error(), true)
		return m, nil
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
	// Keep the open application's record current.
	if m.app != nil {
		for _, a := range m.apps {
			if a.Dir == m.app.Dir {
				m.app = a
			}
		}
	}
	return m, nil
}

func (m Model) open(path string) tea.Cmd {
	open := m.cfg.Open
	return func() tea.Msg { return openedMsg{err: open(path)} }
}

// startWork shows the working screen with a spinner, and returns a context
// that Esc cancels.
func (m *Model) startWork(message string) context.Context {
	m.stopWork()
	m.screen = scrWorking
	m.working = message
	m.steps = nil
	m.started = m.cfg.Now()
	m.workCtx, m.cancel = context.WithCancel(context.Background())
	return m.workCtx
}

func (m *Model) stopWork() {
	if m.cancel != nil {
		m.cancel()
	}
	m.workCtx, m.cancel = nil, nil
}

// ctx returns the current work's context.
func (m *Model) ctx() context.Context {
	if m.workCtx == nil {
		m.workCtx, m.cancel = context.WithCancel(context.Background())
	}
	return m.workCtx
}

func (m Model) cancelWork() (tea.Model, tea.Cmd) {
	m.stopWork()
	m.steps = nil
	m.setFlash("Cancelled.", false)
	switch {
	case m.conf == nil:
		m.screen = scrSetup
	case m.app != nil && m.gen.active:
		m.gen = genState{}
		m.screen = scrApp
	default:
		m.screen = scrHome
	}
	return m, nil
}

func (m *Model) setFlash(s string, isErr bool) { m.flash, m.flashErr = s, isErr }

func (m Model) loadProfile() (*profile.Profile, error) {
	p, err := profile.Load(m.profilePath())
	if err != nil {
		return nil, errors.New(m.profilePath() + ": " + err.Error())
	}
	return p, nil
}

func (m Model) newTailor() (*tailor.Tailor, *profile.Profile, error) {
	p, err := m.loadProfile()
	if err != nil {
		return nil, nil, err
	}
	t, err := tailor.New(m.cfg.NewClient(m.conf), p)
	return t, p, err
}

// openFile opens path or a URL with the operating system's default app.
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// editorCmd opens path in $VISUAL or $EDITOR, falling back to Notepad on
// Windows, TextEdit on macOS and nano elsewhere. The app waits for it.
func editorCmd(path string) *exec.Cmd {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(env); e != "" {
			return exec.Command(e, path)
		}
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("notepad", path)
	case "darwin":
		return exec.Command("open", "-W", "-t", path)
	}
	return exec.Command("nano", path)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// fileURL makes a clickable file:// link for a local path.
func fileURL(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.ToSlash(p)
	if len(p) > 0 && p[0] != '/' {
		p = "/" + p // Windows drive letters: file:///C:/...
	}
	return "file://" + p
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
