package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Blathe/rezgen/internal/store"
)

func TestList(t *testing.T) {
	root := filepath.Join(t.TempDir(), "applications")
	old := now
	now = func() time.Time { return time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = old })

	st := store.New(root)
	st.Create("Northwind Freight - AI Solutions Engineer", "posting", "", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	acme, _ := st.Create("Acme - Automation Engineer", "posting", "", time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC))
	acme.ResumeEdit = "# Resume\n"
	st.Save(acme)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "-out", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[1], "generated    2026-10-03 (today)   Acme") || !strings.Contains(lines[1], st.DocsDir(acme)) {
		t.Errorf("newest first, with status and folder:\n%s", out)
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
