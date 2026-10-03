package main

import (
	"bufio"
	"context"
	"encoding/json"
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
	profilePath := fs.String("profile", "profile.json", "path to the profile file")
	postingPath := fs.String("posting", "", "job posting: a URL, a text file, or - to read stdin (required)")
	outDir := fs.String("out", "applications", "folder to save the application in")
	model := fs.String("model", llm.DefaultModel, "Claude model to use")
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
			if err := learn(*profilePath, a, answers); err != nil {
				fmt.Fprintf(stderr, "Couldn't save answers to the profile: %v\n", err)
			} else {
				fmt.Fprintf(stderr, "Saved under learned_facts in %s.\n", *profilePath)
			}
		}
	}

	fmt.Fprintln(stderr, "Writing the resume and cover letter...")
	app := tailor.Application{Posting: post.Text, PostingSource: post.Source, Analysis: a, Answers: answers}
	d, err := t.Write(ctx, post.Text, a, answers)
	var de *tailor.DraftError
	switch {
	case errors.As(err, &de):
		dir, saveErr := tailor.Save(*outDir, p, app, now())
		if saveErr == nil {
			saveErr = writeRejected(dir, de)
		}
		fmt.Fprintln(stderr, err)
		if saveErr != nil {
			fmt.Fprintf(stderr, "also failed to save: %v\n", saveErr)
		} else {
			fmt.Fprintf(stderr, "The analysis and the rejected draft are in %s\n", dir)
		}
		return 1
	case err != nil:
		fmt.Fprintln(stderr, err)
		return 1
	}
	app.Draft = d

	dir, err := tailor.Save(*outDir, p, app, now())
	if err != nil {
		fmt.Fprintf(stderr, "save: %v\n", err)
		return 1
	}
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
	fmt.Fprintf(stdout, "Once you've sent it: rezgen status %s applied\n", filepath.Base(dir))
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

// learn saves answers to the profile's learned_facts.
func learn(profilePath string, a *tailor.Analysis, answers []tailor.Answer) error {
	ctx := strings.TrimSpace(strings.Join([]string{a.Company, a.Role}, ", "))
	ctx = strings.Trim(ctx, ", ")
	facts := make([]profile.LearnedFact, len(answers))
	for i, an := range answers {
		facts[i] = profile.LearnedFact{
			ID:       an.QuestionID,
			Question: an.Question,
			Answer:   an.Text,
			Learned:  now().Format("2006-01-02"),
			Context:  ctx,
		}
	}
	_, err := profile.AddLearnedFacts(profilePath, facts)
	return err
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func writeRejected(dir string, de *tailor.DraftError) error {
	data, err := json.MarshalIndent(struct {
		Problems []string      `json:"problems"`
		Draft    *tailor.Draft `json:"draft"`
	}{de.Problems, de.Draft}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "draft-rejected.json"), append(data, '\n'), 0o644)
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
