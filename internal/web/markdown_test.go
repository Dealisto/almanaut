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

func TestRenderMarkdownStripsDangerousLinkSchemes(t *testing.T) {
	dangerous := []string{
		"[x](javascript:alert(1))",
		"[x](JavaScript:alert(1))",
		"[x](data:text/html,<script>alert(1)</script>)",
		"[x](vbscript:msgbox(1))",
		"<javascript:alert(1)>",
		"![x](javascript:alert(1))",
	}
	for _, src := range dangerous {
		out := string(renderMarkdown(src))
		for _, scheme := range []string{"javascript:", "data:text/html", "vbscript:"} {
			if strings.Contains(strings.ToLower(out), `href="`+strings.ToLower(scheme)) ||
				strings.Contains(strings.ToLower(out), `src="`+strings.ToLower(scheme)) {
				t.Errorf("dangerous scheme survived for %q: %q", src, out)
			}
		}
	}
}

func TestRenderMarkdownKeepsSafeLinks(t *testing.T) {
	cases := map[string]string{
		"[x](https://example.com)": `href="https://example.com"`,
		"[x](http://example.com)":  `href="http://example.com"`,
		"[x](/relative/path)":      `href="/relative/path"`,
		"[x](mailto:a@b.com)":      `href="mailto:a@b.com"`,
	}
	for src, want := range cases {
		out := string(renderMarkdown(src))
		if !strings.Contains(out, want) {
			t.Errorf("safe link mangled for %q: want %q in %q", src, want, out)
		}
	}
}
