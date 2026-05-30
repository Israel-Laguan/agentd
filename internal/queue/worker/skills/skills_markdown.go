package wskills

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ---------------------------------------------------------------------------
// Markdown helpers (private copies; shared logic with instructions.go)
// ---------------------------------------------------------------------------

func headingText(heading *ast.Heading, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(heading, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if textNode, ok := node.(*ast.Text); ok {
				b.Write(textNode.Value(source))
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

func splitH2Sections(content string) map[string]string {
	sections := make(map[string]string)
	md := goldmark.New()
	source := []byte(content)
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	var currentHeading string
	var startOffset int

	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if heading, ok := n.(*ast.Heading); ok && heading.Level == 2 {
			if currentHeading != "" && heading.Lines().Len() > 0 {
				stopOffset := heading.Lines().At(0).Start
				for stopOffset > startOffset && source[stopOffset-1] != '\n' {
					stopOffset--
				}
				if stopOffset > startOffset {
					sections[currentHeading] = strings.TrimSpace(string(source[startOffset:stopOffset]))
				} else {
					sections[currentHeading] = ""
				}
			}
			currentHeading = headingText(heading, source)
			if heading.Lines().Len() > 0 {
				startOffset = heading.Lines().At(heading.Lines().Len() - 1).Stop
				if startOffset < len(source) && source[startOffset] == '\n' {
					startOffset++
				}
			}
		}
	}

	if currentHeading != "" {
		if startOffset < len(source) {
			sections[currentHeading] = strings.TrimSpace(string(source[startOffset:]))
		} else {
			sections[currentHeading] = ""
		}
	}

	return sections
}
