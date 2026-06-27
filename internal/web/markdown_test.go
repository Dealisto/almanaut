package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	out := string(renderMarkdown("# Title\n\nsome **bold** text"))
	if !strings.Contains(out, "<h1") || !strings.Contains(out, "Title") {
		t.Errorf("heading not rendered: %q", out)
	}
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("bold not rendered: %q", out)
	}
}

func TestRenderMarkdownEscapesRawHTML(t *testing.T) {
	out := string(renderMarkdown("hello <script>alert(1)</script>"))
	if strings.Contains(out, "<script>") {
		t.Errorf("raw HTML must be escaped, got: %q", out)
	}
}
