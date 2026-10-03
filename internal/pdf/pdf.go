// Package pdf renders rezgen's Markdown (headings, paragraphs, bullet lists,
// bold, italics and links) as a PDF. The layout is deliberately plain so
// applicant tracking systems can read it: one column, real selectable text
// in a standard font, no tables or images.
package pdf

import (
	"io"
	"os"
	"strings"

	"codeberg.org/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Layout, in points (72 per inch).
const (
	margin     = 54 // 0.75 in
	bodySize   = 10.5
	lineHeight = 14
	h1Size     = 20
	h2Size     = 12
	h3Size     = 11
	indent     = 12
	paraGap    = 8
	font       = "Helvetica"
)

// compress is turned off in tests so the page text can be searched.
var compress = true

// Render writes md as a US Letter PDF to w and returns the page count.
func Render(md []byte, w io.Writer) (int, error) {
	pdf := fpdf.New("P", "pt", "Letter", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(true, margin)
	pdf.SetCreator("rezgen", true)
	pdf.SetCompression(compress)
	pdf.AddPage()

	r := &renderer{
		pdf: pdf,
		src: md,
		// The standard PDF fonts use the Windows-1252 character set, which
		// covers curly quotes, dashes and accented Latin letters.
		tr: pdf.UnicodeTranslatorFromDescriptor(""),
	}
	doc := goldmark.New().Parser().Parse(text.NewReader(md))
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		r.block(n)
	}
	if err := pdf.Error(); err != nil {
		return 0, err
	}
	pages := pdf.PageNo()
	if err := pdf.Output(w); err != nil {
		return 0, err
	}
	return pages, nil
}

// ConvertFile renders the Markdown file at path to a PDF next to it (same
// name, .pdf extension) and returns the PDF's path and page count.
func ConvertFile(path string) (string, int, error) {
	md, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	out := strings.TrimSuffix(path, ".md") + ".pdf"
	f, err := os.Create(out)
	if err != nil {
		return "", 0, err
	}
	pages, err := Render(md, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(out)
		return "", 0, err
	}
	return out, pages, nil
}

type renderer struct {
	pdf     *fpdf.Fpdf
	src     []byte
	tr      func(string) string
	size    float64
	bold    int // nesting depth of strong emphasis
	italic  int
	started bool // whether any block has been written yet
}

// gap adds vertical space before a block, except at the top of the page.
func (r *renderer) gap(h float64) {
	if r.started && r.pdf.GetY() > margin+1 {
		r.pdf.Ln(h)
	}
	r.started = true
}

func (r *renderer) block(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		switch n.Level {
		case 1:
			r.gap(0)
			r.size = h1Size
			r.bold++
			r.inline(n, h1Size+4)
			r.bold--
		case 2:
			r.gap(10)
			r.size = h2Size
			r.bold++
			r.inline(n, h2Size+4)
			r.bold--
			// A thin rule under section headings.
			y := r.pdf.GetY() + 1
			w, _ := r.pdf.GetPageSize()
			r.pdf.SetLineWidth(0.5)
			r.pdf.Line(margin, y, w-margin, y)
			r.pdf.Ln(4)
		default:
			r.gap(7)
			r.size = h3Size
			r.bold++
			r.inline(n, h3Size+3)
			r.bold--
		}
	case *ast.Paragraph, *ast.TextBlock:
		// A line right under a role heading (its dates) stays close to it.
		if h, ok := n.PreviousSibling().(*ast.Heading); ok && h.Level >= 3 {
			r.gap(1)
		} else {
			r.gap(paraGap)
		}
		r.size = bodySize
		r.inline(n, lineHeight)
	case *ast.List:
		r.gap(3)
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			r.listItem(item)
		}
	case *ast.ThematicBreak:
		r.gap(6)
		w, _ := r.pdf.GetPageSize()
		y := r.pdf.GetY()
		r.pdf.Line(margin, y, w-margin, y)
		r.pdf.Ln(6)
	default:
		// Anything else (block quotes, code blocks) renders as body text.
		r.gap(5)
		r.size = bodySize
		r.inline(n, lineHeight)
	}
}

// listItem draws a bullet with a hanging indent so wrapped lines line up
// with the text, not the bullet.
func (r *renderer) listItem(item ast.Node) {
	left, top, right, _ := r.pdf.GetMargins()
	r.size = bodySize
	r.setFont()
	r.pdf.SetX(left + 2)
	r.pdf.Write(lineHeight, r.tr("•"))
	r.pdf.SetLeftMargin(left + indent)
	r.pdf.SetX(left + indent)
	for c := item.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.(type) {
		case *ast.List:
			r.block(c)
		default:
			r.inline(c, lineHeight)
		}
	}
	r.pdf.SetMargins(left, top, right)
	r.pdf.Ln(2)
}

// inline writes n's inline content as flowing text and ends the line.
func (r *renderer) inline(n ast.Node, lh float64) {
	r.setFont()
	r.inlineChildren(n, lh)
	r.pdf.Ln(lh)
}

func (r *renderer) inlineChildren(n ast.Node, lh float64) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			r.write(string(c.Value(r.src)), lh)
			if c.HardLineBreak() {
				r.pdf.Ln(lh)
			} else if c.SoftLineBreak() {
				r.write(" ", lh)
			}
		case *ast.String:
			r.write(string(c.Value), lh)
		case *ast.CodeSpan:
			r.inlineChildren(c, lh)
		case *ast.Emphasis:
			if c.Level >= 2 {
				r.bold++
			} else {
				r.italic++
			}
			r.setFont()
			r.inlineChildren(c, lh)
			if c.Level >= 2 {
				r.bold--
			} else {
				r.italic--
			}
			r.setFont()
		case *ast.Link:
			r.link(c, string(c.Destination), lh)
		case *ast.AutoLink:
			url := string(c.URL(r.src))
			r.pdf.WriteLinkString(lh, r.tr(url), url)
		default:
			r.inlineChildren(c, lh)
		}
	}
}

// link writes the link's text as a clickable link.
func (r *renderer) link(n ast.Node, url string, lh float64) {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Value(r.src))
		}
		for g := c.FirstChild(); g != nil; g = g.NextSibling() {
			if t, ok := g.(*ast.Text); ok {
				b.Write(t.Value(r.src))
			}
		}
	}
	r.pdf.WriteLinkString(lh, r.tr(b.String()), url)
}

func (r *renderer) write(s string, lh float64) {
	r.pdf.Write(lh, r.tr(s))
}

func (r *renderer) setFont() {
	style := ""
	if r.bold > 0 {
		style += "B"
	}
	if r.italic > 0 {
		style += "I"
	}
	r.pdf.SetFont(font, style, r.size)
}
