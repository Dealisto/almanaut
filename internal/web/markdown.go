package web

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
)

// md renders Markdown to HTML. It uses goldmark's default configuration, which
// does NOT enable raw-HTML passthrough — so HTML embedded in a note's source is
// escaped rather than executed.
var md = goldmark.New()

// renderMarkdown converts Markdown source to HTML safe to embed in a page.
// (This is the single sanctioned use of template.HTML in the codebase; see the
// plan's Global Constraints.)
func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		// On the unlikely conversion error, fall back to escaped plain text.
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
