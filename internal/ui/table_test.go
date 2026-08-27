package ui

import (
	"bytes"
	"strings"
	"testing"
)

func withColor(t *testing.T, enabled bool) {
	t.Helper()
	prev := ColorEnabled()
	SetColor(enabled)
	t.Cleanup(func() { SetColor(prev) })
}

func TestVisibleWidthIgnoresEscapeCodes(t *testing.T) {
	withColor(t, true)

	styled := Red.Render("hello")
	if len(styled) <= len("hello") {
		t.Fatal("expected the styled string to carry escape codes")
	}
	if got := TextWidth(styled); got != 5 {
		t.Fatalf("TextWidth(%q) = %d, want 5", styled, got)
	}
}

func TestVisibleWidthCountsWideRunes(t *testing.T) {
	if got := TextWidth("日本"); got != 4 {
		t.Fatalf("TextWidth(日本) = %d, want 4", got)
	}
}

func TestRenderWithoutColorIsPlain(t *testing.T) {
	withColor(t, false)
	if got := Red.Render("hello"); got != "hello" {
		t.Fatalf("got %q, want plain text", got)
	}
}

func TestCellFold(t *testing.T) {
	withColor(t, false)

	cases := []struct {
		name  string
		cell  Cell
		width int
		want  []string
	}{
		{"fits on one line", Text("abc", Plain), 10, []string{"abc"}},
		{"exact fit", Text("abcde", Plain), 5, []string{"abcde"}},
		{"folds at width", Text("abcdefg", Plain), 3, []string{"abc", "def", "g"}},
		{"empty cell", Text("", Plain), 5, []string{""}},
		{"folds across segments",
			Cell{{Text: "abc", Style: Plain}, {Text: "def", Style: Plain}}, 4,
			[]string{"abcd", "ef"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cell.fold(tc.width)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// Folding must not slice a multi-byte rune in half.
func TestCellFoldUnicode(t *testing.T) {
	withColor(t, false)

	lines := Cell{{Text: "héllo wörld", Style: Plain}}.fold(5)
	for _, line := range lines {
		if strings.ContainsRune(line, '\uFFFD') {
			t.Fatalf("fold produced a broken rune: %q", lines)
		}
	}
	if joined := strings.Join(lines, ""); joined != "héllo wörld" {
		t.Fatalf("fold lost content: %q", joined)
	}
}

func TestCellWidth(t *testing.T) {
	cell := Cell{{Text: "ab", Style: Red}, {Text: "cde", Style: Green}}
	if got := cell.width(); got != 5 {
		t.Fatalf("width() = %d, want 5", got)
	}
}

func TestTableRendersAllContent(t *testing.T) {
	withColor(t, false)

	table := &Table{
		Headers:  []Cell{Text("Path", Plain), Text("Value", Plain)},
		Rows:     [][]Cell{{Text("spec.replicas", Plain), Text("3", Plain)}},
		MaxWidth: 60,
	}
	var buf bytes.Buffer
	table.Render(&buf)

	out := buf.String()
	for _, want := range []string{"Path", "Value", "spec.replicas", "3", "╭", "╰", "│"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output is missing %q:\n%s", want, out)
		}
	}
}

// Every rendered line must be the same width, or the borders will not line up.
func TestTableLinesAreAligned(t *testing.T) {
	withColor(t, false)

	table := &Table{
		Headers: []Cell{Text("A", Plain), Text("B", Plain)},
		Rows: [][]Cell{
			{Text("short", Plain), Text("a much longer value that must fold", Plain)},
			{Text("x", Plain), Text("y", Plain)},
		},
		MaxWidth: 40,
	}
	var buf bytes.Buffer
	table.Render(&buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	width := TextWidth(lines[0])
	if width > 40 {
		t.Errorf("table is %d columns wide, want at most 40", width)
	}
	for i, line := range lines {
		if got := TextWidth(line); got != width {
			t.Errorf("line %d is %d wide, want %d:\n%s", i, got, width, line)
		}
	}
}

// Protected columns keep their width; the others fold instead.
func TestTableNoShrinkProtectsColumns(t *testing.T) {
	withColor(t, false)

	path := "resource.aws_instance.web[0].instance_type"
	table := &Table{
		Headers:  []Cell{Text("Path", Plain), Text("Value", Plain)},
		Rows:     [][]Cell{{Text(path, Plain), Text(strings.Repeat("v", 60), Plain)}},
		NoShrink: []bool{true, false},
		MaxWidth: 70,
	}
	var buf bytes.Buffer
	table.Render(&buf)

	if !strings.Contains(buf.String(), path) {
		t.Fatalf("protected column was folded:\n%s", buf.String())
	}
}

func TestTableAlignment(t *testing.T) {
	withColor(t, false)

	table := &Table{
		Headers:  []Cell{Text("k", Plain)},
		Rows:     [][]Cell{{Text("x", Plain)}},
		Aligns:   []Align{AlignCenter},
		MaxWidth: 40,
	}
	// The column is one wide, so centering is a no-op — it just must not panic
	// or lose the value.
	var buf bytes.Buffer
	table.Render(&buf)
	if !strings.Contains(buf.String(), "x") {
		t.Fatalf("value lost:\n%s", buf.String())
	}

	if got := table.pad("x", 5, 0); got != "  x  " {
		t.Errorf("center pad = %q, want %q", got, "  x  ")
	}
	table.Aligns = []Align{AlignRight}
	if got := table.pad("x", 3, 0); got != "  x" {
		t.Errorf("right pad = %q, want %q", got, "  x")
	}
	table.Aligns = []Align{AlignLeft}
	if got := table.pad("x", 3, 0); got != "x  " {
		t.Errorf("left pad = %q, want %q", got, "x  ")
	}
}

func TestRuleFitsTerminalWidth(t *testing.T) {
	withColor(t, false)

	var buf bytes.Buffer
	prev := Out
	Out = &buf
	t.Cleanup(func() { Out = prev })

	Rule("PAIR 1/3", Cyan)
	line := strings.TrimRight(buf.String(), "\n")

	if !strings.Contains(line, "PAIR 1/3") {
		t.Fatalf("rule is missing its title: %q", line)
	}
	if got := TextWidth(line); got != width() {
		t.Errorf("rule is %d wide, want %d", got, width())
	}
}
