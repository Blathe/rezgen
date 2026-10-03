// Package tailor turns a profile and a job posting into a tailored resume and
// cover letter. It runs in two model calls: Analyze reads the posting against
// the profile and lists questions for the candidate, then Write drafts the
// documents. Every drafted line cites the profile entries (or answers) it came
// from, and Write rejects a draft that cites anything that doesn't exist.
package tailor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/profile"
)

var (
	//go:embed schemas/analysis.json
	analysisSchemaJSON []byte
	//go:embed schemas/draft.json
	draftSchemaJSON []byte

	analysisSchema = mustSchema(analysisSchemaJSON)
	draftSchema    = mustSchema(draftSchemaJSON)
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
}

// Line is a piece of generated text and the IDs it was drawn from.
type Line struct {
	Text    string   `json:"text"`
	Sources []string `json:"sources"`
}

type RoleDraft struct {
	ID      string `json:"id"`
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

// maxAttempts is how many drafts Write requests before giving up on one that
// keeps citing sources that don't exist.
const maxAttempts = 2

// Write drafts the resume and cover letter. If a draft fails the source
// checks, Write asks once more with the problems listed; if the second draft
// fails too, it returns a *DraftError holding that draft.
func (t *Tailor) Write(ctx context.Context, posting string, a *Analysis, answers []Answer) (*Draft, error) {
	cat := newCatalog(t.Profile, answers)
	prompt := writePrompt(posting, a, answers)
	var (
		d     *Draft
		probs []string
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		p := prompt
		if attempt > 1 {
			p = retryPrompt(prompt, d, probs)
		}
		out, err := t.LLM.JSON(ctx, llm.Request{
			System:    t.system,
			Prompt:    p,
			Schema:    draftSchema,
			MaxTokens: 16000,
		})
		if err != nil {
			return nil, fmt.Errorf("write draft: %w", err)
		}
		d = new(Draft)
		if err := json.Unmarshal(out, d); err != nil {
			return nil, fmt.Errorf("write draft: decode response: %w", err)
		}
		if probs = cat.check(d); len(probs) == 0 {
			return d, nil
		}
	}
	return nil, &DraftError{Draft: d, Problems: probs}
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
