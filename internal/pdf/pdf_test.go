package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const resume = `# Jordan Example

**AI Solutions Engineer**

jordan@example.com | 555-0100 | Denver, CO

## Experience

### Senior Software Engineer, Acme Logistics

Remote | Mar 2022 - Present

- Built a Claude-based triage service that routes 1,200 support tickets a week, cutting first-response time 40%.
- Automated invoice entry - saved 30 hours a week \| with *review*.

## Projects

- **[rezgen](https://github.com/Blathe/rezgen)**: Go CLI that tailors resumes.
`

func render(t *testing.T, md string) (string, int) {
	t.Helper()
	compress = false
	t.Cleanup(func() { compress = true })
	var buf bytes.Buffer
	pages, err := Render([]byte(md), &buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf.String(), pages
}

func TestRender(t *testing.T) {
	out, pages := render(t, resume)
	if !strings.HasPrefix(out, "%PDF-") {
		t.Fatal("output is not a PDF")
	}
	if pages != 1 {
		t.Errorf("pages = %d, want 1", pages)
	}
	// Text is written as PDF string literals; check for pieces that don't
	// wrap across lines.
	for _, want := range []string{"Jordan Example", "AI Solutions Engineer", "Senior Software Engineer, Acme Logistics", "rezgen", "review"} {
		if !strings.Contains(out, "("+want) && !strings.Contains(out, want) {
			t.Errorf("PDF text missing %q", want)
		}
	}
	for _, unwanted := range []string{"**", "](", "\\|", "# "} {
		if strings.Contains(out, "("+unwanted) {
			t.Errorf("Markdown syntax %q leaked into the PDF", unwanted)
		}
	}
	if !strings.Contains(out, "/URI (https://github.com/Blathe/rezgen)") {
		t.Error("link is not clickable")
	}
	if !strings.Contains(out, "/Helvetica-Bold") {
		t.Error("bold font not used")
	}
}

func TestRenderPageCount(t *testing.T) {
	long := "# Long\n\n" + strings.Repeat("- A bullet that takes up a line of the page.\n", 120)
	if _, pages := render(t, long); pages < 2 {
		t.Errorf("pages = %d, want at least 2", pages)
	}
}

func TestConvertFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.md")
	if err := os.WriteFile(path, []byte(resume), 0o644); err != nil {
		t.Fatal(err)
	}
	out, pages, err := ConvertFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out != strings.TrimSuffix(path, ".md")+".pdf" || pages != 1 {
		t.Errorf("got %s, %d pages", out, pages)
	}
	data, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %v", err)
	}
}
