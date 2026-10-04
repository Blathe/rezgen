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
	"github.com/Blathe/rezgen/internal/profile"
	"github.com/Blathe/rezgen/internal/store"
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
	letterFixture   = "../../internal/tailor/testdata/cover_letter.json"
	exampleProfile  = "../../examples/profile.example.json"
)

func TestGenerate(t *testing.T) {
	// Answer the first question, skip the second.
	fake := useFake(t, "No, only Docker.\n\n", analysisFixture, draftFixture, letterFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	st, a := onlyApp(t, out)
	dir := filepath.Join(out, "2026-10-02-000000-northwind-freight-ai-solutions-engineer")
	if st.DocsDir(a) != dir || !strings.Contains(stdout.String(), dir) {
		t.Errorf("stdout doesn't name %s:\n%s", dir, stdout.String())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("the documents folder should hold only the two PDFs, has %d entries", len(entries))
	}
	for _, f := range []string{"resume.pdf", "cover-letter.pdf"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("%s missing or not a PDF: %v", f, err)
		}
	}
	if a.Draft == nil || len(a.Answers) != 1 || a.Answers[0].Text != "No, only Docker." {
		t.Errorf("record: %+v", a)
	}
	if !strings.Contains(fake.Requests[1].Prompt, "No, only Docker.") {
		t.Error("answer not passed to the write call")
	}
}

func TestGenerateLearnsAnswers(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "profile.json")
	data, _ := os.ReadFile(exampleProfile)
	os.WriteFile(prof, data, 0o644)

	// Answer the first question, skip the second, accept saving.
	useFake(t, "No, only Docker.\n\n\n", analysisFixture, draftFixture, letterFixture)
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", prof, "-posting", writePosting(t), "-out", t.TempDir(), "-no-pdf"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Save 1 answer to") {
		t.Errorf("no save prompt:\n%s", stderr.String())
	}
	p, err := profile.Load(prof)
	if err != nil {
		t.Fatal(err)
	}
	want := profile.LearnedFact{
		ID: "q-kubernetes", Question: "Have you deployed services on Kubernetes?", Answer: "No, only Docker.",
		Learned: "2026-10-02", Context: "Northwind Freight, AI Solutions Engineer",
	}
	if len(p.LearnedFacts) != 1 || p.LearnedFacts[0] != want {
		t.Errorf("learned facts: %+v", p.LearnedFacts)
	}

	// The next run sends the learned fact to the model with the profile.
	fake := useFake(t, "", analysisFixture, draftFixture, letterFixture)
	if code := run([]string{"generate", "-profile", prof, "-posting", writePosting(t), "-out", t.TempDir(), "-no-pdf", "-no-questions"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second run exit %d", code)
	}
	if !strings.Contains(fake.Requests[0].System, "No, only Docker.") {
		t.Error("learned fact not in the system prompt")
	}
}

func TestGenerateDeclinesLearning(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "profile.json")
	data, _ := os.ReadFile(exampleProfile)
	os.WriteFile(prof, data, 0o644)

	useFake(t, "No, only Docker.\n\nn\n", analysisFixture, draftFixture, letterFixture)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "-profile", prof, "-posting", writePosting(t), "-out", t.TempDir(), "-no-pdf"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	after, _ := os.ReadFile(prof)
	if !bytes.Equal(data, after) {
		t.Error("profile changed after declining")
	}
}

func TestGenerateNoQuestions(t *testing.T) {
	fake := useFake(t, "", analysisFixture, draftFixture, letterFixture)
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
	useFake(t, "", analysisFixture, draftFixture, letterFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", writePosting(t), "-out", out, "-no-questions", "-no-pdf"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	st, a := onlyApp(t, out)
	if st.HasPDFs(a) || a.Status() != store.Generated {
		t.Error("-no-pdf should save the documents without exporting PDFs")
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
	_, a := onlyApp(t, out)
	if a.Status() != store.NotStarted || a.Rejected == nil || !strings.Contains(strings.Join(a.Rejected.Problems, " "), "fact-99") {
		t.Errorf("rejected draft not recorded: %+v\nstderr:\n%s", a, stderr.String())
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

	fake := useFake(t, "", analysisFixture, draftFixture, letterFixture)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-profile", exampleProfile, "-posting", srv.URL + "/jobs/1", "-out", out, "-no-questions"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	if p := fake.Requests[0].Prompt; !strings.Contains(p, "<posting>\nAI Solutions Engineer\n\nNorthwind Freight builds") || strings.Contains(p, "Jobs") {
		t.Errorf("page text not extracted cleanly:\n%s", p)
	}
	if _, a := onlyApp(t, out); a.Source != srv.URL+"/jobs/1" || !strings.HasPrefix(a.Posting, "AI Solutions Engineer") {
		t.Errorf("source/posting not recorded: %q %q", a.Source, a.Posting)
	}
}

func TestGenerateRequiresPosting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "-profile", exampleProfile}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// onlyApp returns the single application saved under out.
func onlyApp(t *testing.T, out string) (*store.Store, *store.Application) {
	t.Helper()
	st := store.New(out)
	apps, err := st.List()
	if err != nil || len(apps) != 1 {
		t.Fatalf("want one application, got %d (%v)", len(apps), err)
	}
	return st, apps[0]
}
