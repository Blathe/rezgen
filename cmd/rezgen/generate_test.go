package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blathe/rezgen/internal/llm"
	"github.com/Blathe/rezgen/internal/llm/llmtest"
)

// useFake points generate at a scripted model, canned stdin and a fixed date
// for the duration of the test.
func useFake(t *testing.T, input string, responses ...string) *llmtest.Fake {
	t.Helper()
	fake := &llmtest.Fake{}
	for _, r := range responses {
		data, err := os.ReadFile(r)
		if err != nil {
			t.Fatal(err)
		}
		fake.Responses = append(fake.Responses, data)
	}
	oldClient, oldStdin, oldNow := newClient, stdin, now
	newClient = func(string, string) llm.Client { return fake }
	stdin = strings.NewReader(input)
	now = func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { newClient, stdin, now = oldClient, oldStdin, oldNow })
	return fake
}

func writePosting(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "posting.txt")
	if err := os.WriteFile(path, []byte("Northwind Freight is hiring an AI Solutions Engineer."), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	analysisFixture = "../../internal/tailor/testdata/analysis.json"
	draftFixture    = "../../internal/tailor/testdata/draft.json"
	exampleProfile  = "../../examples/profile.example.json"
)

func TestGenerate(t *testing.T) {
	// Answer the first question, skip the second.
	fake := useFake(t, "No, only Docker.\n\n", analysisFixture, draftFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	dir := filepath.Join(out, "2026-10-02-northwind-freight-ai-solutions-engineer")
	if !strings.Contains(stdout.String(), dir) {
		t.Errorf("stdout doesn't name %s:\n%s", dir, stdout.String())
	}
	resume, err := os.ReadFile(filepath.Join(dir, "resume.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resume), "# Jordan Example") {
		t.Errorf("unexpected resume:\n%s", resume)
	}
	for _, f := range []string{"resume.pdf", "cover-letter.pdf"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("%s missing or not a PDF: %v", f, err)
		}
	}
	answers, err := os.ReadFile(filepath.Join(dir, "answers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(answers), "No, only Docker.") || strings.Contains(string(answers), "q-kubernetes-2") {
		t.Errorf("unexpected answers:\n%s", answers)
	}
	if !strings.Contains(fake.Requests[1].Prompt, "No, only Docker.") {
		t.Error("answer not passed to the write call")
	}
}

func TestGenerateNoQuestions(t *testing.T) {
	fake := useFake(t, "", analysisFixture, draftFixture)
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", t.TempDir(), "-no-questions"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if strings.Contains(fake.Requests[1].Prompt, "<answers>") {
		t.Error("answers sent despite -no-questions")
	}
}

func TestGenerateNoPDF(t *testing.T) {
	useFake(t, "", analysisFixture, draftFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", out, "-no-questions", "-no-pdf"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "2026-10-02-northwind-freight-ai-solutions-engineer", "resume.pdf")); err == nil {
		t.Error("resume.pdf written despite -no-pdf")
	}
}

func TestPDFCommand(t *testing.T) {
	md := filepath.Join(t.TempDir(), "resume.md")
	os.WriteFile(md, []byte("# Jordan Example\n\n- One bullet\n"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"pdf", md}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "resume.pdf (1 page)") {
		t.Errorf("unexpected output: %s", stdout.String())
	}
	if code := run([]string{"pdf", "missing.md"}, &stdout, &stderr); code != 1 {
		t.Errorf("missing file: exit %d, want 1", code)
	}
}

func TestGenerateSavesRejectedDraft(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.json")
	draft, _ := os.ReadFile(draftFixture)
	if err := os.WriteFile(bad, bytes.Replace(draft, []byte(`"fact-1", "fact-2"`), []byte(`"fact-99"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	useFake(t, "", analysisFixture, bad, bad)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", out, "-no-questions"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	rejected := filepath.Join(out, "2026-10-02-northwind-freight-ai-solutions-engineer", "draft-rejected.json")
	data, err := os.ReadFile(rejected)
	if err != nil {
		t.Fatalf("rejected draft not saved: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(string(data), "fact-99") {
		t.Errorf("unexpected rejected draft:\n%s", data)
	}
}

func TestGenerateFromURL(t *testing.T) {
	body := "<html><body><nav>Jobs</nav><main><h1>AI Solutions Engineer</h1><p>" +
		strings.Repeat("Northwind Freight builds LLM automations for operations. ", 10) +
		"</p></main></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()

	fake := useFake(t, "", analysisFixture, draftFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", srv.URL + "/jobs/1", "-out", out, "-no-questions"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if p := fake.Requests[0].Prompt; !strings.Contains(p, "<posting>\nAI Solutions Engineer\n\nNorthwind Freight builds") || strings.Contains(p, "Jobs") {
		t.Errorf("page text not extracted cleanly:\n%s", p)
	}
	saved, err := os.ReadFile(filepath.Join(out, "2026-10-02-northwind-freight-ai-solutions-engineer", "posting.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(saved), "Source: "+srv.URL+"/jobs/1\n\n") {
		t.Errorf("posting.md doesn't record the URL:\n%s", saved)
	}
}

func TestGenerateRequiresPosting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "-profile", exampleProfile}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
