package format

import "testing"

func TestToWhatsApp(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bold", "**hi**", "*hi*"},
		{"bold underscores", "__hi__", "*hi*"},
		{"italic star", "*hi*", "_hi_"},
		{"italic underscore", "_hi_", "_hi_"},
		{"strike", "~~hi~~", "~hi~"},
		{"inline code", "use `go test` now", "use ```go test``` now"},
		{"link", "see [docs](https://x.io)", "see docs (https://x.io)"},
		{"heading", "# Title", "*Title*"},
		{"bullet dash", "- item", "• item"},
		{"mixed", "**bold** and `code`", "*bold* and ```code```"},
		{"unmatched is literal", "a ** b", "a ** b"},
		{"plain untouched", "just text", "just text"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToWhatsApp(c.in); got != c.want {
				t.Errorf("ToWhatsApp(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestToWhatsAppCodeBlock(t *testing.T) {
	in := "before\n```\nline1\nline2\n```\nafter"
	want := "before\n```\nline1\nline2\n```\nafter"
	if got := ToWhatsApp(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToTelegramHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bold", "**hi**", "<b>hi</b>"},
		{"italic", "*hi*", "<i>hi</i>"},
		{"strike", "~~hi~~", "<s>hi</s>"},
		{"inline code", "`x<y`", "<code>x&lt;y</code>"},
		{"link", "[docs](https://x.io)", `<a href="https://x.io">docs</a>`},
		{"link escapes href", `[x](https://x.io?a="b")`, `<a href="https://x.io?a=&quot;b&quot;">x</a>`},
		{"heading", "## Title", "<b>Title</b>"},
		{"bullet", "- item", "• item"},
		{"escapes plain html", "a < b & c", "a &lt; b &amp; c"},
		{"bold with special", "**a<b**", "<b>a&lt;b</b>"},
		{"thematic break dropped (no invalid tag)", "a\n\n---\n\nb", "a\n\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToTelegramHTML(c.in); got != c.want {
				t.Errorf("ToTelegramHTML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTelegramHTMLCodeBlockEscapes(t *testing.T) {
	got := ToTelegramHTML("```\nif a < b {\n```")
	want := "<pre><code>if a &lt; b {\n</code></pre>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A message split mid-construct must still render safely (no broken HTML).
func TestPartialInputStaysBalanced(t *testing.T) {
	if got := ToTelegramHTML("strong **start of bold"); got != "strong **start of bold" {
		t.Errorf("unmatched bold should render literally, got %q", got)
	}
}
