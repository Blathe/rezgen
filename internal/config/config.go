// Package config stores rezgen's per-user settings: which model provider and
// key to use, and where the profile and applications live. It is written by
// the first-run setup in the interactive app and lives in the user's config
// directory (%AppData%\rezgen on Windows, ~/.config/rezgen on Linux,
// ~/Library/Application Support/rezgen on macOS).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Provider names a model API.
type Provider string

const (
	Anthropic Provider = "anthropic"
	OpenAI    Provider = "openai"
)

// Config is the contents of config.json.
type Config struct {
	Provider Provider `json:"provider"`
	APIKey   string   `json:"api_key"`
	Model    string   `json:"model"`
	Effort   string   `json:"effort,omitempty"`
	// DataDir holds profile.json and the applications folder.
	DataDir string `json:"data_dir"`
}

// ProfilePath is where the profile lives.
func (c *Config) ProfilePath() string { return filepath.Join(c.DataDir, "profile.json") }

// ApplicationsDir is where application folders live.
func (c *Config) ApplicationsDir() string { return filepath.Join(c.DataDir, "applications") }

// Path returns the config file's location. REZGEN_CONFIG overrides it, which
// tests use to stay out of the real config directory.
func Path() (string, error) {
	if p := os.Getenv("REZGEN_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rezgen", "config.json"), nil
}

// ErrNotFound means setup hasn't been run yet.
var ErrNotFound = errors.New("rezgen is not set up yet")

// Load reads the config file. It returns ErrNotFound if there isn't one.
// An API key in the environment (ANTHROPIC_API_KEY or OPENAI_API_KEY, for the
// configured provider) takes precedence over the stored one.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if key := os.Getenv(envKey(c.Provider)); key != "" {
		c.APIKey = key
	}
	return &c, nil
}

// Save writes c to the config file, readable only by the current user since
// it holds the API key.
func Save(c *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func envKey(p Provider) string {
	if p == OpenAI {
		return "OPENAI_API_KEY"
	}
	return "ANTHROPIC_API_KEY"
}

// DetectProvider guesses the provider from an API key's prefix: Anthropic
// keys start with "sk-ant-", OpenAI keys with "sk-".
func DetectProvider(key string) (Provider, error) {
	key = strings.TrimSpace(key)
	switch {
	case strings.HasPrefix(key, "sk-ant-"):
		return Anthropic, nil
	case strings.HasPrefix(key, "sk-"):
		return OpenAI, nil
	}
	return "", errors.New("that doesn't look like an Anthropic (sk-ant-...) or OpenAI (sk-...) API key")
}

// Model is a choice offered in the model picker.
type Model struct {
	ID   string
	Note string
}

// Models lists the recommended models for a provider, best first.
func Models(p Provider) []Model {
	switch p {
	case OpenAI:
		return []Model{
			{"gpt-5", "most capable"},
			{"gpt-5-mini", "faster and cheaper"},
		}
	default:
		return []Model{
			{"claude-opus-5-5", "best writing quality (recommended)"},
			{"claude-sonnet-5-5", "faster and about half the cost"},
			{"claude-haiku-4-5", "fastest and cheapest"},
		}
	}
}

// Supported reports whether rezgen can generate documents with p yet.
func Supported(p Provider) bool { return p == Anthropic }

// DefaultDataDir suggests where to keep the profile and applications:
// Documents/rezgen in the user's home folder.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "rezgen"
	}
	if docs := filepath.Join(home, "Documents"); isDir(docs) {
		return filepath.Join(docs, "rezgen")
	}
	return filepath.Join(home, "rezgen")
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Mask shows just enough of a key to recognize it.
func Mask(key string) string {
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:7] + "..." + key[len(key)-4:]
}
