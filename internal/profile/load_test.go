package profile

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExampleProfileIsValid(t *testing.T) {
	p, err := Load("../../examples/profile.example.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !p.Experience[0].Current() {
		t.Error("first role should be current")
	}
	if p.Experience[1].Current() {
		t.Error("second role should have ended")
	}
}

func TestParseReportsProblems(t *testing.T) {
	base, err := os.ReadFile("../../examples/profile.example.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		from, to  string
		wantPath  string
		wantInMsg string
	}{
		{"bad month", `"start": "2022-03"`, `"start": "2022-13"`, "/experience/0/start", ""},
		{"unknown field", `"context":`, `"contxt":`, "/experience/0", "contxt"},
		{"bad email", `"jordan@example.com"`, `"jordan"`, "/contact/email", ""},
		{"duplicate id", `"id": "brightpath-scheduling"`, `"id": "acme-ticket-triage"`, "/experience/1/highlights/0/id", "already used"},
		{"end before start", `"end": "2022-02"`, `"end": "2017-01"`, "/experience/1/end", "before start"},
		{"missing name", `"name": "Jordan Example",`, ``, "/contact", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := strings.Replace(string(base), tt.from, tt.to, 1)
			if data == string(base) {
				t.Fatalf("replacement %q not found in example", tt.from)
			}
			_, err := Parse([]byte(data))
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("want InvalidError, got %v", err)
			}
			for _, p := range inv.Problems {
				if p.Path == tt.wantPath && strings.Contains(p.Message, tt.wantInMsg) {
					return
				}
			}
			t.Errorf("no problem at %s containing %q; got:\n%v", tt.wantPath, tt.wantInMsg, inv)
		})
	}
}

func TestParseRejectsNonJSON(t *testing.T) {
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Fatal("want error")
	}
}
