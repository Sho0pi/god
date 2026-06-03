package webextract

import (
	"strings"
	"testing"
)

func TestHtmlToMarkdown(t *testing.T) {
	src := `<!doctype html><html><head><title> Hello Page </title>
		<style>.x{color:red}</style></head>
		<body>
		<h1>Main Heading</h1>
		<p>Some <a href="https://x.test">link text</a> here.</p>
		<ul><li>one</li><li>two</li></ul>
		<script>alert('xss')</script>
		</body></html>`

	title, body := htmlToMarkdown(src)
	if title != "Hello Page" {
		t.Errorf("title = %q, want Hello Page", title)
	}
	checks := map[string]bool{
		"# Main Heading":              true,
		"[link text](https://x.test)": true,
		"- one":                       true,
		"- two":                       true,
		"alert":                       false, // script dropped
		"color:red":                   false, // style dropped
	}
	for sub, want := range checks {
		if got := strings.Contains(body, sub); got != want {
			t.Errorf("body contains %q = %v, want %v\nbody:\n%s", sub, got, want, body)
		}
	}
}

func TestStripBase64Images(t *testing.T) {
	in := "before data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB after"
	out := stripBase64Images(in)
	if strings.Contains(out, "base64") {
		t.Errorf("base64 not stripped: %q", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Errorf("surrounding text lost: %q", out)
	}
}

func TestCollapseBlankLines(t *testing.T) {
	got := collapseBlankLines("a\n\n\n\n\nb   \n   \nc")
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("3+ newlines not collapsed: %q", got)
	}
}
