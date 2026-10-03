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

func TestList(t *testing.T) {
	root := t.TempDir()
	old := now
	now = func() time.Time { return time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })

	track.Create(root, "Northwind Freight - AI Solutions Engineer", "posting", "", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	acme, _ := track.Create(root, "Acme - Automation Engineer", "posting", "", time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	os.WriteFile(filepath.Join(acme.Dir, "resume.md"), []byte("x"), 0o644)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "-out", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[1], "generated    2026-10-03 (today)   Acme") {
		t.Errorf("newest first, with status:\n%s", out)
	}
	if !strings.Contains(out, "not started  2026-10-01 (2 days)  Northwind") || !strings.Contains(out, "2 applications: 1 generated, 1 not started") {
		t.Errorf("unexpected list:\n%s", out)
	}
}

func TestListEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "-out", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "No applications") {
		t.Errorf("exit %d: %s", code, stdout.String())
	}
}
