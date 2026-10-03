package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Blathe/rezgen/internal/config"
)

// TestMain keeps tests away from the real settings file on this machine.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rezgen-cmd-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("REZGEN_CONFIG", filepath.Join(dir, "config.json"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

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

func TestDefaultsFromSettings(t *testing.T) {
	data := t.TempDir()
	t.Setenv("REZGEN_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("ANTHROPIC_API_KEY", "")

	var p, o, m string
	defaults(&p, &o, &m)
	if p != "profile.json" || o != "applications" || m != "claude-opus-5-5" {
		t.Errorf("without settings: %q %q %q", p, o, m)
	}

	config.Save(&config.Config{Provider: config.Anthropic, APIKey: "sk-ant-saved", Model: "claude-sonnet-5-5", DataDir: data})
	p, o, m = "", "", ""
	defaults(&p, &o, &m)
	if p != filepath.Join(data, "profile.json") || o != filepath.Join(data, "applications") || m != "claude-sonnet-5-5" {
		t.Errorf("with settings: %q %q %q", p, o, m)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "sk-ant-saved" {
		t.Error("saved key not used")
	}

	p = "mine.json"
	defaults(&p, nil, nil)
	if p != "mine.json" {
		t.Error("a flag should win over settings")
	}
}
