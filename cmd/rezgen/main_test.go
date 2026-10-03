package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-profile", "../../examples/profile.example.json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "is valid: 2 roles, 3 highlights") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestValidateMissingFile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"validate", "-profile", "nope.json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"bogus"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
