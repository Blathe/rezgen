package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/Blathe/rezgen/internal/dotenv"
	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/pdf"
	"github.com/Blathe/rezgen/internal/posting"
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/tailor"
	"github.com/Blathe/rezgen/internal/track"
)

// These are variables so tests can swap in a fake model, canned input and a
// fixed clock.
var (
	newClient           = func(model, effort string) llm.Client { return llm.NewAnthropic(model, effort) }
	stdin     io.Reader = os.Stdin
	now                 = time.Now
)

func generate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profilePath := fs.String("profile", "", "path to the profile file (default: from settings, else ./profile.json)")
	postingPath := fs.String("posting", "", "job posting: a URL, a text file, or - to read stdin (required)")
	outDir := fs.String("out", "", "folder to save the application in (default: from settings, else ./applications)")
	model := fs.String("model", "", "Claude model to use (default: from settings, else "+llm.DefaultModel+")")
	effort := fs.String("effort", "high", "effort level: low, medium, high, xhigh or max")
	noQuestions := fs.Bool("no-questions", false, "skip the follow-up questions")
	envFile := fs.String("env", ".env", "file to read ANTHROPIC_API_KEY and other settings from, if it exists")
	noPDF := fs.Bool("no-pdf", false, "write Markdown only, without PDFs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *postingPath == "" {
		fmt.Fprintln(stderr, "generate: -posting is required (a URL, a file path, or - for stdin)")
		return 2
	}
	if err := dotenv.Load(*envFile); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defaults(profilePath, outDir, model)

	p, err := profile.Load(*profilePath)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", *profilePath, err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if posting.IsURL(*postingPath) {
		fmt.Fprintf(stderr, "Fetching %s...\n", *postingPath)
	}
	post, err := posting.Loader{Stdin: stdin}.Load(ctx, *postingPath)
	if err != nil {
		fmt.Fprintf(stderr, "read posting: %v\n", err)
		return 1
	}

	t, err := tailor.New(newClient(*model, *effort), p)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintln(stderr, "Analyzing the posting...")
	a, err := t.Analyze(ctx, post.Text)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stderr, "%s at %s: %d requirements matched, %d gaps.\n",
		orUnknown(a.Role), orUnknown(a.Company), len(a.Matches), len(a.Gaps))

	var answers []tailor.Answer
	switch {
	case len(a.Questions) == 0:
	case *noQuestions:
		fmt.Fprintf(stderr, "Skipping %d question(s) (-no-questions).\n", len(a.Questions))
	case *postingPath == "-":
		fmt.Fprintf(stderr, "Skipping %d question(s): the posting came from stdin, so there's no way to answer.\n", len(a.Questions))
	default:
		in := bufio.NewScanner(stdin)
		answers = ask(a.Questions, in, stderr)
		if len(answers) > 0 && confirm(in, stderr, fmt.Sprintf("Save %s to %s so rezgen won't ask again?", countNoun(len(answers), "answer"), *profilePath)) {
			if err := tailor.LearnAnswers(*profilePath, a, answers, now()); err != nil {
				fmt.Fprintf(stderr, "Couldn't save answers to the profile: %v\n", err)
			} else {
				fmt.Fprintf(stderr, "Saved under learned_facts in %s.\n", *profilePath)
			}
		}
	}

	fmt.Fprintln(stderr, "Writing the resume and cover letter...")
	d, err := t.Write(ctx, post.Text, a, answers)
	var de *tailor.DraftError
	if err != nil && !errors.As(err, &de) {
		fmt.Fprintln(stderr, err)
		return 1
	}

	source := post.Source
	if source == "stdin" {
		source = ""
	}
	app, cerr := track.Create(*outDir, a.Name(), post.Text, source, now())
	if cerr == nil {
		cerr = tailor.WriteResults(app, p, tailor.Results{Analysis: a, Answers: answers, Draft: d}, now())
	}
	if de != nil {
		if cerr == nil {
			cerr = tailor.SaveRejected(app.Dir, de)
		}
		fmt.Fprintln(stderr, err)
		if cerr != nil {
			fmt.Fprintf(stderr, "also failed to save: %v\n", cerr)
		} else {
			fmt.Fprintf(stderr, "The analysis and the rejected draft are in %s\n", app.Dir)
		}
		return 1
	}
	if cerr != nil {
		fmt.Fprintf(stderr, "save: %v\n", cerr)
		return 1
	}
	dir := app.Dir
	code := 0
	if !*noPDF {
		code = writePDFs(stderr, p.Preferences.MaxPages, filepath.Join(dir, "resume.md"), filepath.Join(dir, "cover-letter.md"))
	}
	fmt.Fprintf(stdout, "Saved to %s\n", dir)
	files := []string{"resume.md", "cover-letter.md", "sources.md"}
	if !*noPDF {
		files = []string{"resume.pdf", "cover-letter.pdf", "resume.md", "cover-letter.md", "sources.md"}
	}
	for _, f := range files {
		fmt.Fprintf(stdout, "  %s\n", filepath.Join(dir, f))
	}
	return code
}

// writePDFs renders each Markdown file to a PDF beside it. maxPages, if set,
// is the resume page limit from the profile; a resume over it gets a
// warning, not a failure, since it can be trimmed by editing the Markdown.
func writePDFs(stderr io.Writer, maxPages int, paths ...string) int {
	code := 0
	for _, path := range paths {
		out, pages, err := pdf.ConvertFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "pdf %s: %v\n", path, err)
			code = 1
			continue
		}
		if maxPages > 0 && pages > maxPages && strings.HasPrefix(filepath.Base(path), "resume") {
			fmt.Fprintf(stderr, "Note: %s is %d pages; your profile asks for %d. Trim resume.md and run: rezgen pdf %s\n",
				out, pages, maxPages, path)
		}
	}
	return code
}

// ask puts each question to the candidate and returns the non-empty answers.
func ask(qs []tailor.Question, sc *bufio.Scanner, out io.Writer) []tailor.Answer {
	fmt.Fprintf(out, "\n%d question(s) that could strengthen this application. Answer only with facts; press Enter to skip.\n", len(qs))
	var answers []tailor.Answer
	for i, q := range qs {
		fmt.Fprintf(out, "\n%d. %s\n   (%s)\n> ", i+1, q.Text, q.Why)
		if !sc.Scan() {
			fmt.Fprintln(out)
			break
		}
		if text := strings.TrimSpace(sc.Text()); text != "" {
			answers = append(answers, tailor.Answer{QuestionID: q.ID, Question: q.Text, Text: text})
		}
	}
	fmt.Fprintln(out)
	return answers
}

// confirm asks a yes/no question that defaults to yes.
func confirm(sc *bufio.Scanner, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [Y/n] ", question)
	if !sc.Scan() {
		fmt.Fprintln(out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sc.Text())) {
	case "", "y", "yes":
		return true
	}
	return false
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
