package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blathe/rezgen/internal/track"
)

func trackFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := now
	now = func() time.Time { return time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })

	for name, r := range map[string]*track.Record{
		"2026-10-01-northwind-freight-ai-solutions-engineer": track.New("Northwind Freight", "AI Solutions Engineer", "https://example.com/1", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		"2026-10-03-acme-automation-engineer":                track.New("Acme", "Automation Engineer", "", time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)),
	} {
		dir := filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)
		if err := track.Save(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestStatusUpdateAndList(t *testing.T) {
	root := trackFixture(t)
	var stdout, stderr bytes.Buffer

	code := run([]string{"status", "northwind", "applied", "-out", root, "-note", "via referral"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	for _, want := range []string{"Status:  applied", "2026-10-01  draft", "2026-10-03  applied  via referral", "Posting: https://example.com/1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("status output missing %q:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	if code := run([]string{"list", "-out", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	out := stdout.String()
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "STATUS") || !strings.HasPrefix(lines[1], "draft") || !strings.Contains(lines[1], "Acme") {
		t.Errorf("newest application should be listed first:\n%s", out)
	}
	if !strings.Contains(out, "applied  2026-10-03 (today)") || !strings.Contains(out, "2 applications: 1 draft, 1 applied") {
		t.Errorf("unexpected list:\n%s", out)
	}

	stdout.Reset()
	run([]string{"list", "-out", root, "-status", "applied"}, &stdout, &stderr)
	if strings.Contains(stdout.String(), "Acme") || !strings.Contains(stdout.String(), "Northwind") {
		t.Errorf("filter not applied:\n%s", stdout.String())
	}
}

func TestStatusShowAndErrors(t *testing.T) {
	root := trackFixture(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"status", "-out", root, "acme"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Status:  draft") {
		t.Errorf("show: exit %d\n%s%s", code, stdout.String(), stderr.String())
	}
	if code := run([]string{"status", "acme", "ghosted", "-out", root}, &stdout, &stderr); code != 2 {
		t.Errorf("bad status: exit %d", code)
	}
	if code := run([]string{"status", "engineer", "applied", "-out", root}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "matches 2 applications") {
		t.Errorf("ambiguous: exit %d, %s", code, stderr.String())
	}
	if code := run([]string{"status", "acme", "applied", "-date", "10/03", "-out", root}, &stdout, &stderr); code != 2 {
		t.Errorf("bad date: exit %d", code)
	}
}

func TestListEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "-out", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "No applications") {
		t.Errorf("exit %d: %s", code, stdout.String())
	}
}
