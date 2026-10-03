package profile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed profile.schema.json
var schemaJSON []byte

// Schema returns the JSON Schema that profile files are validated against.
func Schema() []byte { return schemaJSON }

var (
	compiled = mustCompile()
	printer  = message.NewPrinter(language.English)
)

func mustCompile() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		panic(fmt.Sprintf("profile: embedded schema is not JSON: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("profile.schema.json", doc); err != nil {
		panic(fmt.Sprintf("profile: add schema: %v", err))
	}
	s, err := c.Compile("profile.schema.json")
	if err != nil {
		panic(fmt.Sprintf("profile: compile schema: %v", err))
	}
	return s
}

// Problem is one reason a profile is invalid, located by JSON pointer.
type Problem struct {
	Path    string
	Message string
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// InvalidError lists every problem found in a profile.
type InvalidError struct {
	Problems []Problem
}

func (e *InvalidError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = "  " + p.String()
	}
	return fmt.Sprintf("profile has %d problem(s):\n%s", len(e.Problems), strings.Join(lines, "\n"))
}

// Load reads and validates the profile at path.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse validates data against the schema and the cross-field rules, then
// decodes it. All problems are reported together.
func Parse(data []byte) (*Profile, error) {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("profile is not valid JSON: %w", err)
	}
	if err := compiled.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return nil, &InvalidError{Problems: schemaProblems(ve)}
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if probs := p.check(); len(probs) > 0 {
		return nil, &InvalidError{Problems: probs}
	}
	return &p, nil
}

func schemaProblems(ve *jsonschema.ValidationError) []Problem {
	var out []Problem
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			path := ""
			if len(e.InstanceLocation) > 0 {
				path = "/" + strings.Join(e.InstanceLocation, "/")
			}
			out = append(out, Problem{Path: path, Message: e.ErrorKind.LocalizedString(printer)})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// check enforces rules JSON Schema can't express: IDs unique across the
// whole profile, and end dates not before start dates.
func (p *Profile) check() []Problem {
	var probs []Problem
	seen := map[string]string{}
	claim := func(id, path string) {
		if prev, ok := seen[id]; ok {
			probs = append(probs, Problem{Path: path, Message: fmt.Sprintf("id %q is already used at %s", id, prev)})
			return
		}
		seen[id] = path
	}
	for i, e := range p.Experience {
		base := fmt.Sprintf("/experience/%d", i)
		claim(e.ID, base+"/id")
		if e.End != nil && *e.End < e.Start {
			probs = append(probs, Problem{Path: base + "/end", Message: fmt.Sprintf("end %s is before start %s", *e.End, e.Start)})
		}
		for j, h := range e.Highlights {
			claim(h.ID, fmt.Sprintf("%s/highlights/%d/id", base, j))
		}
	}
	for i, pr := range p.Projects {
		claim(pr.ID, fmt.Sprintf("/projects/%d/id", i))
	}
	for i, s := range p.CoverLetterStories {
		claim(s.ID, fmt.Sprintf("/cover_letter_stories/%d/id", i))
	}
	return probs
}
