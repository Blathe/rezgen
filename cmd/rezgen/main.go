// Command rezgen tailors resumes and cover letters to job postings.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Blathe/rezgen/internal/pdf"
	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/profile"
)

const usage = `rezgen tailors resumes and cover letters to job postings.

Usage:
  rezgen                                                open the interactive app (same as rezgen ui)
  rezgen ui [-profile path] [-out dir]                  open the interactive app
  rezgen validate [-profile path]                       check a profile file
  rezgen generate -posting url|file [-profile path]     tailor a resume and cover letter
  rezgen posting url|file                               print the posting text rezgen would send
  rezgen pdf file.md...                                 render Markdown (e.g. an edited resume.md) to PDF
  rezgen list [-out dir]                                list applications
  rezgen schema                                         print the profile JSON Schema

Run "rezgen generate -h" for all generate options.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if !isTerminal() {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return ui(nil, stdout, stderr)
	}
	switch args[0] {
	case "ui":
		return ui(args[1:], stdout, stderr)
	case "validate":
		return validate(args[1:], stdout, stderr)
	case "generate":
		return generate(args[1:], stdout, stderr)
	case "posting":
		return showPosting(args[1:], stdout, stderr)
	case "pdf":
		return pdfCommand(args[1:], stdout, stderr)
	case "list", "ls":
		return list(args[1:], stdout, stderr)
	case "schema":
		stdout.Write(profile.Schema())
		return 0
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func validate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("profile", "", "path to the profile file (default: from settings, else ./profile.json)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	defaults(path, nil, nil)
	p, err := profile.Load(*path)
	if err != nil {
		var inv *profile.InvalidError
		if errors.As(err, &inv) {
			fmt.Fprintf(stderr, "%s: %v\n", *path, inv)
		} else {
			fmt.Fprintf(stderr, "%s: %v\n", *path, err)
		}
		return 1
	}
	highlights := 0
	for _, e := range p.Experience {
		highlights += len(e.Highlights)
	}
	fmt.Fprintf(stdout, "%s is valid: %d roles, %d highlights, %d projects, %d certifications\n",
		*path, len(p.Experience), highlights, len(p.Projects), len(p.Certifications))
	return 0
}

// pdfCommand re-renders Markdown files to PDF, for after hand edits.
func pdfCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: rezgen pdf file.md...")
		return 2
	}
	code := 0
	for _, path := range args {
		out, pages, err := pdf.ConvertFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "pdf %s: %v\n", path, err)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "%s (%d page%s)\n", out, pages, plural(pages))
	}
	return code
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// showPosting prints the text extracted from a posting, so a URL can be
// checked before spending an API call on it.
func showPosting(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: rezgen posting url|file")
		return 2
	}
	p, err := posting.Loader{Stdin: stdin}.Load(context.Background(), args[0])
	if err != nil {
		fmt.Fprintf(stderr, "read posting: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, p.Text)
	return 0
}
