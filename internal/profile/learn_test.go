package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddLearnedFacts(t *testing.T) {
	path := copyExample(t)
	facts := []LearnedFact{
		{ID: "q-kubernetes", Question: "Have you used Kubernetes?", Answer: "No, only Docker.", Learned: "2026-10-02", Context: "Northwind Freight, AI Solutions Engineer"},
		{ID: "acme", Question: "Team size at Acme?", Answer: "12 engineers"}, // collides with a role id
	}
	saved, err := AddLearnedFacts(path, facts)
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].ID != "q-kubernetes" || saved[1].ID != "acme-2" {
		t.Errorf("saved IDs %q, %q", saved[0].ID, saved[1].ID)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatalf("profile no longer loads: %v", err)
	}
	if len(p.LearnedFacts) != 2 || p.LearnedFacts[0].Answer != "No, only Docker." || p.LearnedFacts[1].ID != "acme-2" {
		t.Errorf("learned facts: %+v", p.LearnedFacts)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	original, _ := os.ReadFile("../../examples/profile.example.json")
	head := strings.TrimRight(strings.TrimSpace(string(original)), "}")
	head = strings.TrimSpace(head)
	if !strings.HasPrefix(text, head) {
		t.Error("the rest of the profile was reformatted")
	}
	if !strings.Contains(text, "\n  \"learned_facts\": [\n    {\n      \"id\": \"q-kubernetes\",") || !strings.HasSuffix(text, "\n  ]\n}\n") {
		t.Errorf("learned_facts not indented as a top-level key:\n%s", text[len(head):])
	}

	// A second run appends to the existing list and avoids its IDs.
	saved, err = AddLearnedFacts(path, []LearnedFact{{ID: "q-kubernetes", Question: "Again?", Answer: "Still no."}})
	if err != nil {
		t.Fatal(err)
	}
	if saved[0].ID != "q-kubernetes-2" {
		t.Errorf("second save ID %q", saved[0].ID)
	}
	if p, _ := Load(path); len(p.LearnedFacts) != 3 {
		t.Errorf("want 3 learned facts, got %d", len(p.LearnedFacts))
	}
}

func TestAddLearnedFactsRejectsInvalid(t *testing.T) {
	path := copyExample(t)
	before, _ := os.ReadFile(path)
	if _, err := AddLearnedFacts(path, []LearnedFact{{ID: "q-x", Question: "Q?", Answer: ""}}); err == nil {
		t.Fatal("want error for an empty answer")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("profile changed despite the error")
	}
}
