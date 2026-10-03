package posting

import (
	"encoding/json"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// FromHTML extracts the posting from a web page. Most job boards (Greenhouse,
// Lever, Ashby, many company career pages) embed a schema.org JobPosting as
// JSON-LD for search engines; when one is present its fields are used, since
// they hold just the posting. Otherwise FromHTML returns the page's visible
// text with navigation, headers, footers and scripts removed.
func FromHTML(page string) (string, error) {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return "", err
	}
	if jp := findJobPosting(doc); jp != nil {
		if text := jp.text(); len(text) >= minURLText {
			return text, nil
		}
	}
	return visibleText(doc), nil
}

// jobPosting holds the schema.org JobPosting fields worth keeping.
type jobPosting struct {
	Type               any             `json:"@type"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	HiringOrganization json.RawMessage `json:"hiringOrganization"`
	JobLocation        json.RawMessage `json:"jobLocation"`
	EmploymentType     any             `json:"employmentType"`
	BaseSalary         json.RawMessage `json:"baseSalary"`
}

func (jp *jobPosting) text() string {
	var b strings.Builder
	line := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			b.WriteString(label + v + "\n")
		}
	}
	line("", jp.Title)
	line("Company: ", nameOf(jp.HiringOrganization))
	line("Location: ", locationOf(jp.JobLocation))
	line("Employment type: ", joinAny(jp.EmploymentType))
	line("Salary: ", salaryOf(jp.BaseSalary))
	// Descriptions are usually HTML, and some sites entity-escape it
	// (&lt;p&gt;) inside the JSON.
	desc := jp.Description
	if !strings.Contains(desc, "<") {
		desc = html.UnescapeString(desc)
	}
	if strings.Contains(desc, "<") {
		if d, err := html.Parse(strings.NewReader(desc)); err == nil {
			desc = visibleText(d)
		}
	}
	if desc = strings.TrimSpace(desc); desc != "" {
		b.WriteString("\n" + desc + "\n")
	}
	return strings.TrimSpace(b.String())
}

func findJobPosting(doc *html.Node) *jobPosting {
	var found *jobPosting
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Script && attr(n, "type") == "application/ld+json" && n.FirstChild != nil {
			found = jobPostingIn([]byte(n.FirstChild.Data))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

// jobPostingIn finds a JobPosting in a JSON-LD block, which may be a single
// object, an array, or an object with an @graph array.
func jobPostingIn(data []byte) *jobPosting {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var search func(v any) *jobPosting
	search = func(v any) *jobPosting {
		switch v := v.(type) {
		case []any:
			for _, item := range v {
				if jp := search(item); jp != nil {
					return jp
				}
			}
		case map[string]any:
			if isType(v["@type"], "JobPosting") {
				b, _ := json.Marshal(v)
				var jp jobPosting
				if json.Unmarshal(b, &jp) == nil {
					return &jp
				}
			}
			if g, ok := v["@graph"]; ok {
				return search(g)
			}
		}
		return nil
	}
	return search(raw)
}

func isType(t any, want string) bool {
	switch t := t.(type) {
	case string:
		return t == want
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func nameOf(raw json.RawMessage) string {
	var org struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &org) == nil {
		return org.Name
	}
	var s string
	json.Unmarshal(raw, &s)
	return s
}

func locationOf(raw json.RawMessage) string {
	type place struct {
		Address struct {
			Locality string `json:"addressLocality"`
			Region   string `json:"addressRegion"`
			Country  any    `json:"addressCountry"`
		} `json:"address"`
	}
	var places []place
	if json.Unmarshal(raw, &places) != nil {
		var one place
		if json.Unmarshal(raw, &one) != nil {
			return ""
		}
		places = []place{one}
	}
	var out []string
	for _, p := range places {
		a := p.Address
		country := ""
		switch c := a.Country.(type) {
		case string:
			country = c
		case map[string]any:
			country, _ = c["name"].(string)
		}
		if s := joinNonEmpty(", ", a.Locality, a.Region, country); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "; ")
}

func salaryOf(raw json.RawMessage) string {
	var s struct {
		Currency string `json:"currency"`
		Value    struct {
			Min      any    `json:"minValue"`
			Max      any    `json:"maxValue"`
			Value    any    `json:"value"`
			UnitText string `json:"unitText"`
		} `json:"value"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	v := s.Value
	amount := joinNonEmpty(" - ", str(v.Min), str(v.Max))
	if amount == "" {
		amount = str(v.Value)
	}
	if amount == "" {
		return ""
	}
	return joinNonEmpty(" ", s.Currency, amount, strings.ToLower(v.UnitText))
}

func str(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		b, _ := json.Marshal(v)
		return string(b)
	}
	return ""
}

func joinAny(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, x := range v {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// skipped elements never hold posting text.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Svg: true, atom.Iframe: true, atom.Nav: true, atom.Header: true,
	atom.Footer: true, atom.Form: true, atom.Button: true, atom.Head: true,
}

// blocks start a new line.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Br: true, atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Tr: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Blockquote: true, atom.Pre: true, atom.Hr: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
}

var (
	spaces     = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// visibleText renders the text a reader would see, one block per line and
// list items as "- " bullets.
func visibleText(doc *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			if skipped[n.DataAtom] || attr(n, "aria-hidden") == "true" || hasAttr(n, "hidden") {
				return
			}
			switch {
			case n.DataAtom == atom.Li:
				b.WriteString("\n- ")
			case blocks[n.DataAtom]:
				b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blocks[n.DataAtom] {
			b.WriteString("\n")
		}
	}
	walk(doc)

	var lines []string
	bullet := false // a "- " whose text landed on a later line, e.g. <li><p>text</p></li>
	for _, l := range strings.Split(b.String(), "\n") {
		l = strings.TrimSpace(spaces.ReplaceAllString(l, " "))
		switch {
		case l == "-":
			bullet = true
			continue
		case bullet && l != "":
			l = "- " + l
			bullet = false
		}
		// Keep consecutive list items together even when a block inside an
		// item left a blank line between them.
		if strings.HasPrefix(l, "- ") {
			n := len(lines)
			for n > 0 && lines[n-1] == "" {
				n--
			}
			if n > 0 && n < len(lines) && strings.HasPrefix(lines[n-1], "- ") {
				lines = lines[:n]
			}
		}
		lines = append(lines, l)
	}
	text := strings.Join(lines, "\n")
	return strings.TrimSpace(blankLines.ReplaceAllString(text, "\n\n"))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
