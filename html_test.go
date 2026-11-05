package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The --md HTML must read in Apple Mail dark mode. Mail turns text white in
// dark mode, so any page background or text colour pinned on html/body gives
// white-on-white. These checks catch that class of bug.
func TestMarkdownHTMLDarkModeSafe(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	md := filepath.Join(t.TempDir(), "in.md")
	src := "# Title\n\nHello **world**.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n`code`\n\n> quote\n"
	if err := os.WriteFile(md, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, out, err := buildBodies("", md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "Hello") || !strings.Contains(out, "<strong>world</strong>") {
		t.Fatalf("markdown not rendered:\nplain=%q\nhtml=%s", plain, out)
	}

	if !regexp.MustCompile(`(?i)<meta name="color-scheme" content="light dark">`).MatchString(out) {
		t.Errorf("missing color-scheme meta:\n%s", out)
	}

	// No style or bgcolor attribute on <html> or <body>.
	if m := regexp.MustCompile(`(?i)<(html|body)\b[^>]*\b(style|bgcolor|text)=`).FindString(out); m != "" {
		t.Errorf("html/body tag sets colours inline: %s", m)
	}

	// No CSS rule aimed at html or body may set color or background.
	css := strings.Join(regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`).FindAllString(out, -1), "\n")
	rule := regexp.MustCompile(`([^{}]+)\{([^}]*)\}`)
	target := regexp.MustCompile(`(?i)(^|[\s,>+~])(html|body|:root)([\s,.:#\[>+~]|$)`)
	prop := regexp.MustCompile(`(?i)(^|;)\s*(color|background|background-color)\s*:`)
	for _, r := range rule.FindAllStringSubmatch(css, -1) {
		if target.MatchString(r[1]) && prop.MatchString(r[2]) {
			t.Errorf("rule %q sets colour or background: %s", strings.TrimSpace(r[1]), r[2])
		}
	}

	// No near-black text or light solid background anywhere in our CSS. Those
	// vanish against Mail's dark canvas or its white text.
	for _, bad := range []string{"#000", "#1a1a1a", "#fdfdfd", "#fff", "#fafafa", "#f3f4f5"} {
		if strings.Contains(strings.ToLower(css), bad) {
			t.Errorf("CSS contains fixed light/dark colour %s", bad)
		}
	}
}

// The --body HTML must read like a normal mail: proportional font, with the
// author's paragraphs and line breaks kept. A <pre> wrapper made Gmail show
// the whole body in monospace (2026-11-05).
func TestPlainBodyHTMLProportional(t *testing.T) {
	body := "Hi Ashish,\r\n\r\nLine one\r\nLine two\n\n\n" +
		"See https://example.com/a?b=1&c=2. Or (https://x.io/p).\n\n" +
		"<b>not bold</b> & stuff"
	plain, out, err := buildBodies(body, "")
	if err != nil {
		t.Fatal(err)
	}
	if plain != body {
		t.Errorf("plain part changed:\n%q", plain)
	}
	if regexp.MustCompile(`(?i)<pre\b`).MatchString(out) {
		t.Errorf("html has <pre>:\n%s", out)
	}
	if regexp.MustCompile(`(?i)monospace|courier|consolas|menlo`).MatchString(out) {
		t.Errorf("html sets a monospace font:\n%s", out)
	}
	for _, want := range []string{
		"<p>Hi Ashish,</p>",
		"<p>Line one<br>\nLine two</p>",
		`<a href="https://example.com/a?b=1&amp;c=2">https://example.com/a?b=1&amp;c=2</a>.`,
		`(<a href="https://x.io/p">https://x.io/p</a>).`,
		"<p>&lt;b&gt;not bold&lt;/b&gt; &amp; stuff</p>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "<p>"); n != 4 {
		t.Errorf("want 4 paragraphs, got %d:\n%s", n, out)
	}
}
