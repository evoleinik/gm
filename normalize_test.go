package main

import "testing"

func TestNormalizeMD(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bold header followed by description",
			in:   "**Profound** ($58.5M raised) — profound.ai\nMarket leader in AI visibility.",
			want: "**Profound** ($58.5M raised) — profound.ai\n\nMarket leader in AI visibility.",
		},
		{
			name: "bold header already has blank line",
			in:   "**Profound** — profound.ai\n\nMarket leader.",
			want: "**Profound** — profound.ai\n\nMarket leader.",
		},
		{
			name: "multiple bold headers with descriptions",
			in:   "**Profound** — profound.ai\nFirst company.\n\n**Lemrock** — lemrock.com\nSecond company.",
			want: "**Profound** — profound.ai\n\nFirst company.\n\n**Lemrock** — lemrock.com\n\nSecond company.",
		},
		{
			name: "bold header followed by list",
			in:   "**Features:**\n- Item one\n- Item two",
			want: "**Features:**\n\n- Item one\n- Item two",
		},
		{
			name: "bold header followed by heading",
			in:   "**Summary**\n## Next Section",
			want: "**Summary**\n\n## Next Section",
		},
		{
			name: "inline bold not at start of line",
			in:   "This has **bold** in the middle.\nNext line.",
			want: "This has **bold** in the middle.\nNext line.",
		},
		{
			name: "list after paragraph (existing rule)",
			in:   "Some text.\n- Item one\n- Item two",
			want: "Some text.\n\n- Item one\n- Item two",
		},
		{
			name: "heading after paragraph (existing rule)",
			in:   "Some text.\n## Heading",
			want: "Some text.\n\n## Heading",
		},
		{
			name: "numbered list after paragraph",
			in:   "Some text.\n1. First\n2. Second",
			want: "Some text.\n\n1. First\n2. Second",
		},
		{
			name: "already correct spacing preserved",
			in:   "## Heading\n\nSome text.\n\n- Item one\n- Item two",
			want: "## Heading\n\nSome text.\n\n- Item one\n- Item two",
		},
		{
			name: "bold line followed by blank then text",
			in:   "**Header**\n\nDescription.",
			want: "**Header**\n\nDescription.",
		},
		{
			name: "consecutive bold lines not separated",
			in:   "**Line one**\n**Line two**",
			want: "**Line one**\n**Line two**",
		},
		{
			name: "bold with parens and dash followed by text",
			in:   "**ReFiBuy** (ChannelAdvisor founder) — refibuy.com\nCommerce Intelligence Engine.",
			want: "**ReFiBuy** (ChannelAdvisor founder) — refibuy.com\n\nCommerce Intelligence Engine.",
		},
		{
			name: "real world tier section",
			in:   "## Tier 1\n\n**Profound** — profound.ai\nMarket leader.\n\n**Lemrock** — lemrock.com\nMiddleware.",
			want: "## Tier 1\n\n**Profound** — profound.ai\n\nMarket leader.\n\n**Lemrock** — lemrock.com\n\nMiddleware.",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "single line",
			in:   "Just text.",
			want: "Just text.",
		},
		{
			name: "table rows not affected",
			in:   "| **Company** | Funding |\n| Profound | $58M |",
			want: "| **Company** | Funding |\n| Profound | $58M |",
		},
		{
			name: "bold key-value not broken",
			in:   "**Key gap:** monitoring only.\n**Our edge:** self-serve.",
			want: "**Key gap:** monitoring only.\n**Our edge:** self-serve.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMD(tt.in)
			if got != tt.want {
				t.Errorf("normalizeMD():\n  input: %q\n  got:   %q\n  want:  %q", tt.in, got, tt.want)
			}
		})
	}
}
