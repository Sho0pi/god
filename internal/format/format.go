// Package format converts the Markdown that LLMs emit into the native markup of
// each chat platform, at send time. Parsing is done by goldmark (CommonMark);
// the platform rendering is a local AST walk per target:
//
//   - Telegram: HTML using only the tag subset Telegram allows (b/i/s/code/pre/
//     a/blockquote). Always balanced and never errors, so a reply split across
//     the 4096-char chunk boundary can't produce broken HTML.
//   - WhatsApp: WhatsApp's *_~``` markup.
//
// We render ourselves (rather than via a third-party renderer) so unsupported
// constructs degrade to plain text instead of emitting invalid tags.
package format

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	xast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// mdParser parses Markdown including GFM strikethrough.
var mdParser = goldmark.New(goldmark.WithExtensions(extension.Strikethrough)).Parser()

var blankLines = regexp.MustCompile(`\n{3,}`)

// htmlText escapes the three characters Telegram requires in text/PCDATA;
// htmlAttr also escapes the attribute-value quote.
var (
	htmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	htmlAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

func parse(md string) (ast.Node, []byte) {
	src := []byte(md)
	return mdParser.Parse(text.NewReader(src)), src
}

func tidy(s string) string {
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n\n"))
}

// ToTelegramHTML renders Markdown as Telegram HTML (send with ParseMode "HTML").
func ToTelegramHTML(md string) string {
	doc, src := parse(md)
	var b strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch node := n.(type) {
		case *ast.Text:
			if entering {
				b.WriteString(htmlText.Replace(string(node.Segment.Value(src))))
				if node.SoftLineBreak() || node.HardLineBreak() {
					b.WriteByte('\n')
				}
			}
		case *ast.String:
			if entering {
				b.WriteString(htmlText.Replace(string(node.Value)))
			}
		case *ast.Emphasis:
			tag := "i"
			if node.Level == 2 {
				tag = "b"
			}
			if entering {
				b.WriteString("<" + tag + ">")
			} else {
				b.WriteString("</" + tag + ">")
			}
		case *xast.Strikethrough:
			tagPair(&b, "s", entering)
		case *ast.CodeSpan:
			tagPair(&b, "code", entering) // child Text is escaped by the Text case
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				b.WriteString("<pre><code>")
				writeEscapedLines(&b, src, n)
				b.WriteString("</code></pre>\n")
			}
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			if entering {
				u := string(node.URL(src))
				b.WriteString(`<a href="` + htmlAttr.Replace(u) + `">` + htmlText.Replace(u) + `</a>`)
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			if entering {
				b.WriteString(`<a href="` + htmlAttr.Replace(string(node.Destination)) + `">`)
			} else {
				b.WriteString("</a>")
			}
		case *ast.Image:
			return ast.WalkSkipChildren, nil // no images in Telegram text
		case *ast.Heading:
			tagPair(&b, "b", entering)
			if !entering {
				b.WriteString("\n")
			}
		case *ast.Blockquote:
			tagPair(&b, "blockquote", entering)
			if !entering {
				b.WriteString("\n")
			}
		case *ast.ListItem:
			if entering {
				b.WriteString("• ")
			} else {
				b.WriteString("\n")
			}
		case *ast.Paragraph, *ast.TextBlock:
			if !entering {
				b.WriteString("\n\n")
			}
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil // drop unsupported raw HTML
		}
		return ast.WalkContinue, nil
	})
	return tidy(b.String())
}

// ToWhatsApp renders Markdown using WhatsApp markup: *bold*, _italic_, ~strike~,
// ```mono```, links as "label (url)".
func ToWhatsApp(md string) string {
	doc, src := parse(md)
	var b strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch node := n.(type) {
		case *ast.Text:
			if entering {
				b.Write(node.Segment.Value(src))
				if node.SoftLineBreak() || node.HardLineBreak() {
					b.WriteByte('\n')
				}
			}
		case *ast.String:
			if entering {
				b.Write(node.Value)
			}
		case *ast.Emphasis:
			if node.Level == 2 {
				b.WriteString("*") // bold
			} else {
				b.WriteString("_") // italic
			}
		case *xast.Strikethrough:
			b.WriteString("~")
		case *ast.CodeSpan:
			b.WriteString("```")
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering {
				b.WriteString("```\n")
				writeLines(&b, src, n)
				b.WriteString("```\n")
			}
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			if entering {
				b.Write(node.URL(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			if !entering {
				b.WriteString(" (")
				b.Write(node.Destination)
				b.WriteString(")")
			}
		case *ast.Image:
			return ast.WalkSkipChildren, nil
		case *ast.Heading:
			b.WriteString("*")
			if !entering {
				b.WriteString("\n")
			}
		case *ast.ListItem:
			if entering {
				b.WriteString("• ")
			} else {
				b.WriteString("\n")
			}
		case *ast.Paragraph, *ast.TextBlock:
			if !entering {
				b.WriteString("\n")
			}
		}
		return ast.WalkContinue, nil
	})
	return tidy(b.String())
}

func tagPair(b *strings.Builder, tag string, entering bool) {
	if entering {
		b.WriteString("<" + tag + ">")
	} else {
		b.WriteString("</" + tag + ">")
	}
}

func writeLines(b *strings.Builder, src []byte, n ast.Node) {
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(seg.Value(src))
	}
}

func writeEscapedLines(b *strings.Builder, src []byte, n ast.Node) {
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.WriteString(htmlText.Replace(string(seg.Value(src))))
	}
}
