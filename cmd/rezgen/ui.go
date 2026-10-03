package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/Blathe/rezgen/internal/dotenv"
	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/tui"
)

// runTUI starts the interactive app; tests replace it.
var runTUI = tui.Run

// ui opens the interactive app.
func ui(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profilePath := fs.String("profile", "profile.json", "path to the profile file")
	outDir := fs.String("out", "applications", "folder that holds the applications")
	model := fs.String("model", llm.DefaultModel, "Claude model to use")
	effort := fs.String("effort", "high", "effort level: low, medium, high, xhigh or max")
	noPDF := fs.Bool("no-pdf", false, "write Markdown only, without PDFs")
	envFile := fs.String("env", ".env", "file to read ANTHROPIC_API_KEY and other settings from, if it exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := dotenv.Load(*envFile); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	err := runTUI(tui.Config{
		ProfilePath: *profilePath,
		OutDir:      *outDir,
		Model:       *model,
		Effort:      *effort,
		NoPDF:       *noPDF,
		NewClient:   newClient,
		Loader:      posting.Loader{},
		Now:         now,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func isTerminal() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}
