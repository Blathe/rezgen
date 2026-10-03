package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

// AddLearnedFacts appends facts to the learned_facts list in the profile file
// at path and returns them with the IDs they were saved under (an ID already
// used in the profile gets a numeric suffix).
//
// Only the learned_facts value is rewritten; the rest of the file keeps its
// exact bytes, so hand formatting and keys such as "$schema" survive. The
// result must still pass Parse before it replaces the original.
func AddLearnedFacts(path string, facts []LearnedFact) ([]LearnedFact, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(data)
	if err != nil {
		return nil, err
	}

	used := p.ids()
	saved := make([]LearnedFact, len(facts))
	for i, f := range facts {
		f.ID = uniqueID(f.ID, used)
		used[f.ID] = true
		saved[i] = f
	}
	if data, err = setLearnedFacts(data, append(p.LearnedFacts, saved...)); err != nil {
		return nil, err
	}

	if _, err := Parse(data); err != nil {
		return nil, fmt.Errorf("profile would be invalid after adding learned facts: %w", err)
	}
	return saved, writeFileAtomic(path, data)
}

// setLearnedFacts replaces the learned_facts value in the profile document
// with facts, indented to match a top-level key, leaving every other byte of
// the document as it was. If the key is missing it is added at the end.
func setLearnedFacts(doc []byte, facts []LearnedFact) ([]byte, error) {
	compact, err := json.Marshal(facts)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(pretty.PrettyOptions(compact, &pretty.Options{Width: 100, Indent: "  "}))), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "  " + lines[i]
	}
	value := strings.Join(lines, "\n")

	if gjson.GetBytes(doc, "learned_facts").Exists() {
		return sjson.SetRawBytes(doc, "learned_facts", []byte(value))
	}
	body := strings.TrimRightFunc(string(doc), unicode.IsSpace)
	if !strings.HasSuffix(body, "}") {
		return nil, fmt.Errorf("profile does not end with }")
	}
	body = strings.TrimRightFunc(strings.TrimSuffix(body, "}"), unicode.IsSpace)
	return []byte(body + ",\n  \"learned_facts\": " + value + "\n}\n"), nil
}

// Save writes p as a new profile file at path, creating its folder. It
// refuses to overwrite an existing file.
func Save(path string, p *Profile) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ids returns every ID used anywhere in the profile.
func (p *Profile) ids() map[string]bool {
	used := map[string]bool{}
	for _, e := range p.Experience {
		used[e.ID] = true
		for _, h := range e.Highlights {
			used[h.ID] = true
		}
	}
	for _, pr := range p.Projects {
		used[pr.ID] = true
	}
	for _, s := range p.CoverLetterStories {
		used[s.ID] = true
	}
	for _, f := range p.LearnedFacts {
		used[f.ID] = true
	}
	return used
}

func uniqueID(id string, used map[string]bool) string {
	if id == "" {
		id = "fact"
	}
	if !used[id] {
		return id
	}
	for n := 2; ; n++ {
		if c := fmt.Sprintf("%s-%d", id, n); !used[c] {
			return c
		}
	}
}

// writeFileAtomic replaces path with data via a temporary file, so a crash
// can't leave a half-written profile.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".profile-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
