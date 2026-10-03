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

	"github.com/Blathe/rezgen/internal/llm"
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
	postingPath := fs.String("posting", "", "path to the job posting text, or - to read stdin (required)")
	outDir := fs.String("out", "applications", "folder to save the application in")
	model := fs.String("model", llm.DefaultModel, "Claude model to use")
	effort := fs.String("effort", "high", "effort level: low, medium, high, xhigh or max")
	noQuestions := fs.Bool("no-questions", false, "skip the follow-up questions")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *postingPath == "" {
		fmt.Fprintln(stderr, "generate: -posting is required (a file path, or - for stdin)")
		return 2
	}

	p, err := profile.Load(*profilePath)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", *profilePath, err)
		return 1
	}
	posting, err := readPosting(*postingPath)
	if err != nil {
		fmt.Fprintf(stderr, "read posting: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	t, err := tailor.New(newClient(*model, *effort), p)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintln(stderr, "Analyzing the posting...")
	a, err := t.Analyze(ctx, posting)
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
		answers = ask(a.Questions, stdin, stderr)
	}

	fmt.Fprintln(stderr, "Writing the resume and cover letter...")
	app := tailor.Application{Posting: posting, Analysis: a, Answers: answers}
	d, err := t.Write(ctx, posting, a, answers)
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
	fmt.Fprintf(stdout, "Saved to %s\n", dir)
	for _, f := range []string{"resume.md", "cover-letter.md", "sources.md"} {
		fmt.Fprintf(stdout, "  %s\n", filepath.Join(dir, f))
	}
	return 0
}

func readPosting(path string) (string, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "", errors.New("posting is empty")
	}
	return s, nil
}

// ask puts each question to the candidate and returns the non-empty answers.
func ask(qs []tailor.Question, in io.Reader, out io.Writer) []tailor.Answer {
	fmt.Fprintf(out, "\n%d question(s) that could strengthen this application. Answer only with facts; press Enter to skip.\n", len(qs))
	sc := bufio.NewScanner(in)
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
