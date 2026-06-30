package web

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// md renders Markdown to HTML. It uses goldmark's default configuration, which
// does NOT enable raw-HTML passthrough — so HTML embedded in a note's source is
// escaped rather than executed — plus a linkSanitizer transformer that strips
// dangerous URL schemes (e.g. javascript:) from links and images.
var md = goldmark.New(
	goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(linkSanitizer{}, 100)),
	),
)

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

// linkSanitizer is a goldmark AST transformer that neutralizes link and image
// destinations whose URL scheme is not allow-listed. goldmark escapes raw HTML
// by default but leaves URL-scheme sanitization to the caller, so without this
// a note like [x](javascript:alert(document.cookie)) would render a clickable
// link that executes script in the app origin.
type linkSanitizer struct{}

func (linkSanitizer) Transform(node *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	// Autolinks are collected and rewritten after the walk: an autolink node has
	// no settable destination, so it must be replaced, and mutating the tree
	// mid-walk would corrupt sibling iteration.
	var unsafeAutoLinks []*ast.AutoLink
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Link:
			if !safeLinkScheme(t.Destination) {
				t.Destination = []byte("#")
			}
		case *ast.Image:
			if !safeLinkScheme(t.Destination) {
				t.Destination = []byte("")
			}
		case *ast.AutoLink:
			if t.AutoLinkType == ast.AutoLinkURL && !safeLinkScheme(t.URL(source)) {
				unsafeAutoLinks = append(unsafeAutoLinks, t)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, al := range unsafeAutoLinks {
		// Render the raw URL as escaped text instead of a clickable href.
		if p := al.Parent(); p != nil {
			p.ReplaceChild(p, al, ast.NewString(al.URL(source)))
		}
	}
}

// safeLinkScheme reports whether a link or image destination is safe to emit as
// an href/src. Scheme-less (relative) destinations are allowed; absolute ones
// are allowed only for http, https, and mailto.
func safeLinkScheme(dest []byte) bool {
	for i := 0; i < len(dest); i++ {
		switch dest[i] {
		case ':':
			return allowedScheme(string(dest[:i]))
		case '/', '?', '#':
			// A path separator, query, or fragment before any ':' means the
			// destination is relative and carries no scheme.
			return true
		}
	}
	return true // no ':' at all → relative
}

// allowedScheme compares a URL scheme against the allow-list with ASCII
// whitespace and control characters removed, because browsers ignore those when
// resolving a scheme (e.g. "java\tscript:" still executes as javascript:).
func allowedScheme(scheme string) bool {
	var b strings.Builder
	for i := 0; i < len(scheme); i++ {
		if c := scheme[i]; c > ' ' && c != 0x7f {
			b.WriteByte(c)
		}
	}
	switch strings.ToLower(b.String()) {
	case "http", "https", "mailto":
		return true
	default:
		return false
	}
}
