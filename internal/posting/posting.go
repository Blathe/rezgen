// Package posting reads a job posting from a file, stdin or a web page and
// returns it as plain text.
package posting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Posting is the text of a job posting and where it came from.
type Posting struct {
	Text   string
	Source string // file path, "stdin", or URL
}

// minURLText is the shortest page text treated as a real posting. Shorter
// usually means the page builds its content with JavaScript, which a plain
// fetch can't see.
const minURLText = 300

// maxPageBytes caps how much of a page is read.
const maxPageBytes = 10 << 20

// Loader reads postings. The zero value is ready to use.
type Loader struct {
	// HTTP fetches URLs; nil means a client with a 30-second timeout.
	HTTP *http.Client
	// Stdin is read when src is "-"; nil means os.Stdin.
	Stdin io.Reader
}

// IsURL reports whether src should be fetched rather than read from disk.
func IsURL(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

// Load reads the posting at src: an http(s) URL, "-" for stdin, or a file
// path.
func (l Loader) Load(ctx context.Context, src string) (*Posting, error) {
	var (
		text string
		err  error
	)
	switch {
	case IsURL(src):
		text, err = l.fetch(ctx, src)
	case src == "-":
		in := l.Stdin
		if in == nil {
			in = os.Stdin
		}
		var data []byte
		data, err = io.ReadAll(in)
		text = string(data)
		src = "stdin"
	default:
		var data []byte
		data, err = os.ReadFile(src)
		text = string(data)
	}
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("posting is empty")
	}
	return &Posting{Text: text, Source: src}, nil
}

func (l Loader) fetch(ctx context.Context, url string) (string, error) {
	client := l.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rezgen (+https://github.com/Blathe/rezgen)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: %s%s", url, resp.Status, saveHint)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}
	text, err := FromHTML(string(body))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", url, err)
	}
	if len(text) < minURLText {
		return "", fmt.Errorf("%s has almost no readable text (the site probably loads the posting with JavaScript)%s", url, saveHint)
	}
	return text, nil
}

const saveHint = "\nCopy the posting text into a file and pass that to -posting instead."
