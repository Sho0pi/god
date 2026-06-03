package webextract

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// base64ImageRe matches inline data: image URIs, which bloat token counts with
// no value to the model. Mirrors Hermes' clean_base64_images.
var base64ImageRe = regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)

// stripBase64Images removes inline base64 image data from text.
func stripBase64Images(s string) string {
	return base64ImageRe.ReplaceAllString(s, "[image]")
}

// htmlToMarkdown converts an HTML document to a compact markdown-ish text:
// headings as #, links as [text](href), list items as "- ", paragraphs and
// block elements separated by blank lines. script/style/noscript and their
// contents are dropped. Returns the page <title> separately.
func htmlToMarkdown(htmlSrc string) (title, body string) {
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		// Parsing HTML effectively never errors (html.Parse is lenient), but if
		// it does, fall back to the raw source with tags stripped crudely.
		return "", stripBase64Images(htmlSrc)
	}

	var sb strings.Builder
	r := &renderer{sb: &sb}
	r.walk(doc)

	body = collapseBlankLines(strings.TrimSpace(sb.String()))
	body = stripBase64Images(body)
	return strings.TrimSpace(r.title), body
}

type renderer struct {
	sb    *strings.Builder
	title string
}

func (r *renderer) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		text := strings.TrimSpace(strings.Join(strings.Fields(n.Data), " "))
		if text != "" {
			r.sb.WriteString(text)
			r.sb.WriteByte(' ')
		}
		return
	case html.ElementNode:
		// Drop non-content elements entirely.
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Head:
			if n.DataAtom == atom.Head {
				// Still want the <title> from inside <head>.
				r.extractTitle(n)
			}
			return
		}

		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			level := int(n.Data[1] - '0')
			r.sb.WriteString("\n\n" + strings.Repeat("#", level) + " ")
			r.walkChildren(n)
			r.sb.WriteString("\n")
			return
		case atom.A:
			href := attr(n, "href")
			text := strings.TrimSpace(r.childrenText(n))
			if text == "" && href == "" {
				return
			}
			r.sb.WriteString("[" + text + "]")
			if href != "" {
				r.sb.WriteString("(" + href + ")")
			}
			r.sb.WriteByte(' ')
			return
		case atom.Li:
			r.sb.WriteString("\n- ")
			r.walkChildren(n)
			return
		case atom.P, atom.Div, atom.Section, atom.Article, atom.Br,
			atom.Ul, atom.Ol, atom.Table, atom.Tr, atom.Header, atom.Footer:
			r.sb.WriteString("\n")
			r.walkChildren(n)
			r.sb.WriteString("\n")
			return
		}
	}
	r.walkChildren(n)
}

// childrenText renders a node's children into a string using a sub-renderer
// that shares the title sink. Used for inline elements (links) where trailing
// whitespace must be trimmed before wrapping in markdown syntax.
func (r *renderer) childrenText(n *html.Node) string {
	var sb strings.Builder
	sub := &renderer{sb: &sb}
	sub.walkChildren(n)
	if sub.title != "" {
		r.title = sub.title
	}
	return sb.String()
}

func (r *renderer) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walk(c)
	}
}

func (r *renderer) extractTitle(head *html.Node) {
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Title && c.FirstChild != nil {
			r.title = strings.TrimSpace(c.FirstChild.Data)
			return
		}
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// collapseBlankLines squeezes 3+ consecutive newlines to 2 and trims trailing
// spaces on each line.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}
