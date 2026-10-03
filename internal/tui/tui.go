// Package tui is rezgen's terminal interface: a list of applications to
// browse and update, and a guided flow for tailoring a new one. It drives the
// same packages as the command-line subcommands (tailor, track, posting,
// pdf), so the two always behave the same.
package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/pdf"
	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
	"github.com/Blathe/rezgen/internal/track"
)

// Config holds what the TUI needs from the command line.
type Config struct {
	ProfilePath string
	OutDir      string
	Model       string
	Effort      string
	NoPDF       bool

	// Hooks for tests; nil means the real thing.
	NewClient func(model, effort string) llm.Client
	Loader    posting.Loader
	Now       func() time.Time
	Open      func(path string) error
}

func (c *Config) defaults() {
	if c.NewClient == nil {
		c.NewClient = func(model, effort string) llm.Client { return llm.NewAnthropic(model, effort) }
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Open == nil {
		c.Open = openFile
	}
}

// Run starts the TUI and blocks until the user quits.
func Run(cfg Config) error {
	_, err := tea.NewProgram(New(cfg), tea.WithAltScreen()).Run()
	return err
}

type screen int

const (
	scrList     screen = iota // all applications
	scrDetail                 // one application's history and files
	scrStatus                 // choosing a new status
	scrNote                   // optional note for a status change
	scrPosting                // entering a posting URL or path
	scrWorking                // waiting on the network or the model
	scrQuestion               // answering the analysis questions
	scrLearn                  // whether to save answers to the profile
	scrDone                   // the new application was saved
)

// Model is the Bubble Tea model for the whole TUI.
type Model struct {
	cfg           Config
	width, height int
	screen        screen

	apps   []track.App
	cursor int

	statusCursor int
	newStatus    track.Status
	note         textinput.Model
	fromDetail   bool // whether the status/note screens were opened from the detail screen

	flash    string // one-line message at the bottom
	flashErr bool

	// New-application flow.
	input    textinput.Model
	spin     spinner.Model
	working  string
	workCtx  context.Context
	cancel   context.CancelFunc
	profile  *profile.Profile
	tailor   *tailor.Tailor
	post     *posting.Posting
	analysis *tailor.Analysis
	answers  []tailor.Answer
	question int
	result   writtenMsg
}

// New returns the initial model, showing the application list.
func New(cfg Config) Model {
	cfg.defaults()
	note := textinput.New()
	note.Placeholder = "optional, e.g. phone screen Tuesday"
	note.CharLimit = 300
	in := textinput.New()
	in.CharLimit = 2000
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accent
	return Model{cfg: cfg, note: note, input: in, spin: sp}
}

func (m Model) Init() tea.Cmd { return m.loadApps("") }

// Messages from background work.
type (
	appsMsg struct {
		apps      []track.App
		err       error
		selectDir string // folder to put the cursor on, if any
	}
	postingMsg struct {
		post *posting.Posting
		err  error
	}
	analyzedMsg struct {
		analysis *tailor.Analysis
		err      error
	}
	learnedMsg struct{ err error }
	writtenMsg struct {
		dir      string
		pages    int
		rejected *tailor.DraftError
		err      error
	}
	openedMsg struct{ err error }
)

func (m Model) loadApps(selectDir string) tea.Cmd {
	root := m.cfg.OutDir
	return func() tea.Msg {
		apps, err := track.List(root)
		return appsMsg{apps: apps, err: err, selectDir: selectDir}
	}
}

func (m Model) fetchPosting(ctx context.Context, src string) tea.Cmd {
	loader := m.cfg.Loader
	return func() tea.Msg {
		p, err := loader.Load(ctx, src)
		return postingMsg{post: p, err: err}
	}
}

func (m Model) analyze(ctx context.Context) tea.Cmd {
	t, text := m.tailor, m.post.Text
	return func() tea.Msg {
		a, err := t.Analyze(ctx, text)
		return analyzedMsg{analysis: a, err: err}
	}
}

func (m Model) learn() tea.Cmd {
	path, a, answers, on := m.cfg.ProfilePath, m.analysis, m.answers, m.cfg.Now()
	return func() tea.Msg {
		return learnedMsg{err: tailor.LearnAnswers(path, a, answers, on)}
	}
}

func (m Model) write(ctx context.Context) tea.Cmd {
	t, p, post, a, answers := m.tailor, m.profile, m.post, m.analysis, m.answers
	out, on, noPDF := m.cfg.OutDir, m.cfg.Now(), m.cfg.NoPDF
	return func() tea.Msg {
		app := tailor.Application{Posting: post.Text, PostingSource: post.Source, Analysis: a, Answers: answers}
		d, err := t.Write(ctx, post.Text, a, answers)
		var de *tailor.DraftError
		if errors.As(err, &de) {
			dir, serr := tailor.Save(out, p, app, on)
			if serr == nil {
				serr = tailor.SaveRejected(dir, de)
			}
			if serr != nil {
				return writtenMsg{err: serr}
			}
			return writtenMsg{dir: dir, rejected: de}
		}
		if err != nil {
			return writtenMsg{err: err}
		}
		app.Draft = d
		dir, err := tailor.Save(out, p, app, on)
		if err != nil {
			return writtenMsg{err: err}
		}
		msg := writtenMsg{dir: dir}
		if !noPDF {
			for _, f := range []string{"resume.md", "cover-letter.md"} {
				_, pages, err := pdf.ConvertFile(filepath.Join(dir, f))
				if err != nil {
					msg.err = err
					return msg
				}
				if f == "resume.md" {
					msg.pages = pages
				}
			}
		}
		return msg
	}
}

func (m Model) open(path string) tea.Cmd {
	open := m.cfg.Open
	return func() tea.Msg { return openedMsg{err: open(path)} }
}

// openFile opens path with the operating system's default app.
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// firstExisting returns the first of names that exists in dir, or dir itself.
func firstExisting(dir string, names ...string) string {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return dir
}
