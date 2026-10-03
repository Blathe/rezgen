package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := `# comment
REZGEN_TEST_PLAIN=abc
export REZGEN_TEST_EXPORTED = def
REZGEN_TEST_DOUBLE="has spaces"
REZGEN_TEST_SINGLE='x=y'

REZGEN_TEST_PRESET=from-file
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REZGEN_TEST_PRESET", "from-shell")
	for _, k := range []string{"REZGEN_TEST_PLAIN", "REZGEN_TEST_EXPORTED", "REZGEN_TEST_DOUBLE", "REZGEN_TEST_SINGLE"} {
		t.Setenv(k, "") // registers cleanup
		os.Unsetenv(k)
	}

	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"REZGEN_TEST_PLAIN":    "abc",
		"REZGEN_TEST_EXPORTED": "def",
		"REZGEN_TEST_DOUBLE":   "has spaces",
		"REZGEN_TEST_SINGLE":   "x=y",
		"REZGEN_TEST_PRESET":   "from-shell",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}

func TestLoadBadLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("NOT A PAIR\n"), 0o600)
	if err := Load(path); err == nil {
		t.Fatal("want error")
	}
}
