package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rezgen", "config.json")
	t.Setenv("REZGEN_CONFIG", path)
	t.Setenv("ANTHROPIC_API_KEY", "")

	if _, err := Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	c := &Config{Provider: Anthropic, APIKey: "sk-ant-stored", Model: "claude-opus-5-5", DataDir: filepath.Join("x", "rezgen")}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if *got != *c {
		t.Errorf("got %+v, want %+v", got, c)
	}
	if got.ProfilePath() != filepath.Join("x", "rezgen", "profile.json") || got.ApplicationsDir() != filepath.Join("x", "rezgen", "applications") {
		t.Errorf("paths: %s, %s", got.ProfilePath(), got.ApplicationsDir())
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("config mode %v, want 0600", st.Mode().Perm())
		}
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	if got, _ := Load(); got.APIKey != "sk-ant-from-env" {
		t.Errorf("env key should win, got %q", got.APIKey)
	}
}

func TestDetectProvider(t *testing.T) {
	for key, want := range map[string]Provider{"sk-ant-api03-abc": Anthropic, " sk-proj-abc ": OpenAI, "sk-abc": OpenAI} {
		if got, err := DetectProvider(key); err != nil || got != want {
			t.Errorf("DetectProvider(%q) = %q, %v", key, got, err)
		}
	}
	if _, err := DetectProvider("hello"); err == nil {
		t.Error("want error for a non-key")
	}
}

func TestMask(t *testing.T) {
	if got := Mask("sk-ant-api03-abcdefghijklmnop"); got != "sk-ant-...mnop" {
		t.Errorf("Mask = %q", got)
	}
	if got := Mask("short"); got != "*****" {
		t.Errorf("Mask short = %q", got)
	}
}
