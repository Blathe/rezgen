// Package tailor turns a profile and a job posting into a tailored resume and
// cover letter. Analyze reads the posting against the profile and lists
// questions for the candidate; Write then drafts the resume and, in a second
// call that sees the finished resume, the cover letter. Every factual line
// cites the profile entries (or answers) it came from, and Write rejects a
// draft that cites anything that doesn't exist.
package tailor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/profile"
)

var (
	//go:embed schemas/analysis.json
	analysisSchemaJSON []byte
	//go:embed schemas/draft.json
	draftSchemaJSON []byte
	//go:embed schemas/cover_letter.json
	coverLetterSchemaJSON []byte

	analysisSchema    = mustSchema(analysisSchemaJSON)
	draftSchema       = mustSchema(draftSchemaJSON)
	coverLetterSchema = mustSchema(coverLetterSchemaJSON)
)

func mustSchema(data []byte) map[string]any {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(fmt.Sprintf("tailor: embedded schema: %v", err))
	}
	return m
}

// Analysis is what the model makes of a posting, measured against the profile.
type Analysis struct {
	Company          string     `json:"company"`
	Role             string     `json:"role"`
	Seniority        string     `json:"seniority"`
	MustHave         []string   `json:"must_have"`
	NiceToHave       []string   `json:"nice_to_have"`
	Keywords         []string   `json:"keywords"`
	Responsibilities []string   `json:"responsibilities"`
	Matches          []Match    `json:"matches"`
	Gaps             []string   `json:"gaps"`
	Questions        []Question `json:"questions"`
}

// Match is a posting requirement the profile already has evidence for.
type Match struct {
	Requirement string   `json:"requirement"`
	SourceIDs   []string `json:"source_ids"`
}

// Question asks the candidate for a fact the profile doesn't have.
type Question struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Why  string `json:"why"`
}

// Answer is the candidate's reply to a Question. Drafts cite it by the
// question's ID.
type Answer struct {
	QuestionID string `json:"question_id"`
	Question   string `json:"question"`
	Text       string `json:"text"`
}

// Draft is the tailored content. Contact details, dates, education and
// certifications are not in it: those are copied straight from the profile
// when rendering, so the model never retypes them.
type Draft struct {
	Headline    string        `json:"headline"`
	Summary     Line          `json:"summary"`
	Experience  []RoleDraft   `json:"experience"`
	Projects    []ProjectLine `json:"projects"`
	Skills      []SkillGroup  `json:"skills"`
	CoverLetter CoverLetter   `json:"cover_letter"`
	// Warnings are style problems that survived the retry, worth a look
	// before sending.
	Warnings []string `json:"warnings,omitempty"`
}

// Line is a piece of generated text and the IDs it was drawn from.
type Line struct {
	Text    string   `json:"text"`
	Sources []string `json:"sources"`
}

type RoleDraft struct {
	ID string `json:"id"`
	// Brief roles are old and unrelated to the posting: one line under
	// Earlier experience, no bullets.
	Brief   bool   `json:"brief,omitempty"`
	Bullets []Line `json:"bullets"`
}

type ProjectLine struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type SkillGroup struct {
	Group string   `json:"group"`
	Items []string `json:"items"`
}

type CoverLetter struct {
	Greeting   string `json:"greeting"`
	Paragraphs []Line `json:"paragraphs"`
	Closing    string `json:"closing"`
}

// Tailor runs the pipeline for one profile.
type Tailor struct {
	LLM     llm.Client
	Profile *profile.Profile
	system  string
}

// New returns a Tailor for p.
func New(c llm.Client, p *profile.Profile) (*Tailor, error) {
	sys, err := systemPrompt(p)
	if err != nil {
		return nil, err
	}
	return &Tailor{LLM: c, Profile: p, system: sys}, nil
}

// Analyze reads the posting against the profile.
func (t *Tailor) Analyze(ctx context.Context, posting string) (*Analysis, error) {
	out, err := t.LLM.JSON(ctx, llm.Request{
		System:    t.system,
		Prompt:    analyzePrompt(posting),
		Schema:    analysisSchema,
		MaxTokens: 8000,
	})
	if err != nil {
		return nil, fmt.Errorf("analyze posting: %w", err)
	}
	var a Analysis
	if err := json.Unmarshal(out, &a); err != nil {
		return nil, fmt.Errorf("analyze posting: decode response: %w", err)
	}
	dedupeQuestionIDs(a.Questions)
	return &a, nil
}

// maxAttempts is how many times Write asks for each document before giving
// up on one that keeps failing the source checks.
const maxAttempts = 2

// Write drafts the resume, then the cover letter in a second call that sees
// the finished resume. Each document is checked: problems (citations that
// don't exist, skills the candidate doesn't have) and style warnings (fact
// dumps, semicolon run-ons, stock openings) are sent back for one retry.
// Problems that survive the retry fail the draft with a *DraftError; warnings
// that survive are kept in Draft.Warnings.
func (t *Tailor) Write(ctx context.Context, posting string, a *Analysis, answers []Answer) (*Draft, error) {
	cat := newCatalog(t.Profile, answers)

	d := new(Draft)
	resumeWarnings, err := t.attempt(ctx, "write resume", resumePrompt(posting, a, answers), draftSchema, d,
		func() findings { return cat.checkResume(d) })
	if de, ok := err.(*DraftError); ok {
		de.Draft = d
		return nil, de
	}
	if err != nil {
		return nil, err
	}

	cl := new(CoverLetter)
	letterWarnings, err := t.attempt(ctx, "write cover letter", coverLetterPrompt(posting, a, answers, d), coverLetterSchema, cl,
		func() findings { return cat.checkLetter(cl, posting) })
	d.CoverLetter = *cl
	if de, ok := err.(*DraftError); ok {
		de.Draft = d
		return nil, de
	}
	if err != nil {
		return nil, err
	}
	d.Warnings = append(resumeWarnings, letterWarnings...)
	return d, nil
}

// attempt asks for a document up to maxAttempts times, decoding each
// response into out and checking it. It returns the warnings left after the
// last attempt, or a *DraftError if problems remain.
func (t *Tailor) attempt(ctx context.Context, what, prompt string, schema map[string]any, out any, check func() findings) ([]string, error) {
	var f findings
	for n := 1; n <= maxAttempts; n++ {
		p := prompt
		if n > 1 {
			p = retryPrompt(prompt, out, f.problems, f.warnings)
		}
		data, err := t.LLM.JSON(ctx, llm.Request{System: t.system, Prompt: p, Schema: schema, MaxTokens: 16000})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		reflect.ValueOf(out).Elem().SetZero() // nothing carries over from the last attempt
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("%s: decode response: %w", what, err)
		}
		f = check()
		if len(f.problems) == 0 && (len(f.warnings) == 0 || n == maxAttempts) {
			return f.warnings, nil
		}
	}
	return nil, &DraftError{Problems: f.problems}
}

// DraftError means the model's draft still failed the source checks after a
// retry. The draft is kept so the caller can save it for inspection.
type DraftError struct {
	Draft    *Draft
	Problems []string
}

func (e *DraftError) Error() string {
	return fmt.Sprintf("draft failed %d check(s) after %d attempts:\n  %s",
		len(e.Problems), maxAttempts, strings.Join(e.Problems, "\n  "))
}

// dedupeQuestionIDs makes question IDs unique so answers can be cited
// unambiguously.
func dedupeQuestionIDs(qs []Question) {
	seen := map[string]int{}
	for i := range qs {
		id := qs[i].ID
		if id == "" {
			id = "q"
		}
		seen[id]++
		if n := seen[id]; n > 1 {
			id = fmt.Sprintf("%s-%d", id, n)
		}
		qs[i].ID = id
	}
}
