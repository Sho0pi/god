// Package format converts the Markdown that LLMs emit into the native markup of
// each chat platform, at send time. Parsing is done by goldmark (CommonMark);
// only the platform rendering is local:
//
//   - Telegram: goldmark + the telegold renderer (Telegram-flavored HTML).
//   - WhatsApp: a small walk over goldmark's AST emitting WhatsApp's *_~``` markup
//     (no maintained library exists for WhatsApp formatting).
package format

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/leonid-shevtsov/telegold"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	xast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// telegramMD renders Markdown to Telegram-compatible HTML via telegold.
var telegramMD = goldmark.New(
	goldmark.WithExtensions(extension.Strikethrough),
	goldmark.WithRenderer(telegold.NewRenderer()),
)

// whatsappParser parses Markdown (incl. GFM strikethrough) for the WhatsApp walk.
var whatsappParser = goldmark.New(goldmark.WithExtensions(extension.Strikethrough)).Parser()

var blankLines = regexp.MustCompile(`\n{3,}`)

// ToTelegramHTML renders Markdown as Telegram HTML (send with ParseMode "HTML").
// Falls back to the raw text if rendering fails.
func ToTelegramHTML(md string) string {
	var buf bytes.Buffer
	if err := telegramMD.Convert([]byte(md), &buf); err != nil {
		return md
	}
	return strings.TrimSpace(buf.String())
}

// ToWhatsApp renders Markdown using WhatsApp markup: *bold*, _italic_, ~strike~,
// ```mono```, links as "label (url)".
func ToWhatsApp(md string) string {
	src := []byte(md)
	doc := whatsappParser.Parse(text.NewReader(src))

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
			mark := "_" // level 1 = italic
			if node.Level == 2 {
				mark = "*" // level 2 = bold
			}
			b.WriteString(mark)
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
			// children render the label; append the URL after it.
			if !entering {
				b.WriteString(" (")
				b.Write(node.Destination)
				b.WriteString(")")
			}
		case *ast.Heading:
			b.WriteString("*") // render headings as bold
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

	out := blankLines.ReplaceAllString(b.String(), "\n\n")
	return strings.TrimSpace(out)
}

func writeLines(b *strings.Builder, src []byte, n ast.Node) {
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		b.Write(line.Value(src))
	}
}
