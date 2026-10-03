package posting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var longDescription = strings.Repeat("Build LLM automations with operations teams. ", 10)

const pageWithJSONLD = `<!doctype html>
<html><head><title>Careers</title>
<script type="application/ld+json">
{"@context": "https://schema.org", "@graph": [
  {"@type": "Organization", "name": "Northwind"},
  {"@type": "JobPosting",
   "title": "AI Solutions Engineer",
   "hiringOrganization": {"@type": "Organization", "name": "Northwind Freight"},
   "jobLocation": [{"@type": "Place", "address": {"addressLocality": "Boise", "addressRegion": "ID", "addressCountry": "US"}}],
   "employmentType": ["FULL_TIME"],
   "baseSalary": {"@type": "MonetaryAmount", "currency": "USD", "value": {"@type": "QuantitativeValue", "minValue": 120000, "maxValue": 150000, "unitText": "YEAR"}},
   "description": "&lt;p&gt;DESC&lt;/p&gt;&lt;ul&gt;&lt;li&gt;Python&lt;/li&gt;&lt;li&gt;Claude API&lt;/li&gt;&lt;/ul&gt;"}
]}
</script></head>
<body><nav>Home | Jobs</nav><p>Page chrome that should not appear.</p></body></html>`

func TestFromHTMLPrefersJobPosting(t *testing.T) {
	page := strings.Replace(pageWithJSONLD, "DESC", longDescription, 1)
	text, err := FromHTML(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"AI Solutions Engineer\nCompany: Northwind Freight\nLocation: Boise, ID, US\nEmployment type: FULL_TIME\nSalary: USD 120000 - 150000 year\n",
		"Build LLM automations",
		"- Python\n- Claude API",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Page chrome") || strings.Contains(text, "<p>") {
		t.Errorf("page chrome or markup leaked in:\n%s", text)
	}
}

func TestFromHTMLVisibleText(t *testing.T) {
	page := `<html><head><style>p{}</style><script>var x = 1;</script></head><body>
<header>Logo</header><nav>Menu</nav>
<main><h1>AI   Solutions Engineer</h1>
<p>We automate&nbsp;operations.</p>
<ul><li><p>Python</p></li><li>Claude <b>API</b></li></ul>
<div hidden>secret</div><button>Apply</button></main>
<footer>Copyright</footer></body></html>`
	text, err := FromHTML(page)
	if err != nil {
		t.Fatal(err)
	}
	want := "AI Solutions Engineer\n\nWe automate operations.\n\n- Python\n- Claude API"
	if text != want {
		t.Errorf("got:\n%q\nwant:\n%q", text, want)
	}
}

func TestLoadURL(t *testing.T) {
	page := strings.Replace(pageWithJSONLD, "DESC", longDescription, 1)
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		switch r.URL.Path {
		case "/job":
			w.Write([]byte(page))
		case "/spa":
			w.Write([]byte(`<html><body><div id="root"></div><script src="app.js"></script></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	l := Loader{HTTP: srv.Client()}

	p, err := l.Load(context.Background(), srv.URL+"/job")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != srv.URL+"/job" || !strings.HasPrefix(p.Text, "AI Solutions Engineer") {
		t.Errorf("unexpected posting: %+v", p)
	}
	if !strings.HasPrefix(ua, "rezgen") {
		t.Errorf("user agent %q", ua)
	}

	for path, want := range map[string]string{"/spa": "JavaScript", "/missing": "404"} {
		_, err := l.Load(context.Background(), srv.URL+path)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "Copy the posting text") {
			t.Errorf("%s: got %v, want error mentioning %q and the fallback", path, err, want)
		}
	}
}

func TestLoadFileAndStdin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.txt")
	os.WriteFile(path, []byte("  file posting \n"), 0o644)
	p, err := Loader{}.Load(context.Background(), path)
	if err != nil || p.Text != "file posting" || p.Source != path {
		t.Errorf("file: %+v, %v", p, err)
	}

	p, err = Loader{Stdin: strings.NewReader("stdin posting")}.Load(context.Background(), "-")
	if err != nil || p.Text != "stdin posting" || p.Source != "stdin" {
		t.Errorf("stdin: %+v, %v", p, err)
	}

	if _, err := (Loader{Stdin: strings.NewReader("  ")}).Load(context.Background(), "-"); err == nil {
		t.Error("empty posting should fail")
	}
}
