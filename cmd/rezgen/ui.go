package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/tui"
)

// runTUI starts the interactive app; tests replace it.
var runTUI = tui.Run

// ui opens the interactive app. Its settings come from the first-run setup;
// flags override them for one session.
func ui(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profilePath := fs.String("profile", "", "profile file to use instead of the one in your data folder")
	outDir := fs.String("out", "", "applications folder to use instead of the one in your data folder")
	model := fs.String("model", "", "Claude model to use instead of the one in settings")
	effort := fs.String("effort", "", "effort level: low, medium, high, xhigh or max")
	noPDF := fs.Bool("no-pdf", false, "write Markdown only, without PDFs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cwd, _ := os.Getwd()
	err := runTUI(tui.Config{
		ProfilePath: *profilePath,
		OutDir:      *outDir,
		Model:       *model,
		Effort:      *effort,
		NoPDF:       *noPDF,
		ImportDir:   cwd,
		Loader:      posting.Loader{},
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
