package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestCardIsRectangular(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	for _, width := range []int{40, 60, 80, 120} {
		card := st.Card(width, "Murmur", []Section{
			{Lines: []string{"", "  a short line", ""}},
			{Title: "Suggested routing", Lines: []string{"  Note    Projects/Tidemail.md"}},
			{Title: "Preview", Lines: []string{"  - [ ] something"}},
		})
		for i, line := range strings.Split(strings.TrimRight(card, "\n"), "\n") {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("width %d: line %d is %d cells wide:\n%q", width, i, got, line)
			}
		}
	}
}

func TestCardDrawsSeparators(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)
	card := st.Card(60, "Murmur", []Section{
		{Lines: []string{"body"}},
		{Title: "Preview", Lines: []string{"content"}},
	})

	if !strings.Contains(card, "╭") || !strings.Contains(card, "╯") {
		t.Error("the card has no rounded corners")
	}
	if !strings.Contains(card, "├") || !strings.Contains(card, "┤") {
		t.Error("the section separator does not touch the frame")
	}
	if !strings.Contains(card, "Preview") {
		t.Error("the section title is missing")
	}
}

func TestCardHandlesTinyWidths(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)
	// Must not panic or produce negative-width padding.
	card := st.Card(10, "A very long title that cannot possibly fit", []Section{
		{Lines: []string{"some content that is far too wide for this card"}},
	})
	if card == "" {
		t.Fatal("no output")
	}
	for _, line := range strings.Split(strings.TrimRight(card, "\n"), "\n") {
		if lipgloss.Width(line) != 24 { // Card clamps to a 24-cell minimum
			t.Errorf("line is %d cells wide: %q", lipgloss.Width(line), line)
		}
	}
}

func TestPadToHandlesStyledText(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	styled := st.Accent.Render("hello")
	padded := padTo(styled, 20)
	if got := lipgloss.Width(padded); got != 20 {
		t.Errorf("width = %d, want 20", got)
	}
	if !strings.Contains(padded, "hello") {
		t.Errorf("content was lost: %q", padded)
	}

	long := st.Accent.Render(strings.Repeat("x", 50))
	if got := lipgloss.Width(padTo(long, 10)); got > 10 {
		t.Errorf("truncation left %d cells, want at most 10", got)
	}
	if padTo("anything", 0) != "" {
		t.Error("zero width should produce nothing")
	}
}

func TestTruncPlain(t *testing.T) {
	t.Parallel()
	if got := truncPlain("short", 20); got != "short" {
		t.Errorf("got %q", got)
	}
	got := truncPlain("Projects/Linux/ROG Flow Z13.md", 12)
	if lipgloss.Width(got) > 12 {
		t.Errorf("%q is wider than 12", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("no ellipsis: %q", got)
	}
}

func TestWrapPlain(t *testing.T) {
	t.Parallel()

	lines := wrapPlain("the quick brown fox jumps over the lazy dog", 15)
	if len(lines) < 2 {
		t.Fatalf("text was not wrapped: %v", lines)
	}
	for _, l := range lines {
		if lipgloss.Width(l) > 15 {
			t.Errorf("line too wide (%d): %q", lipgloss.Width(l), l)
		}
	}
	joined := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if joined != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("wrapping lost words: %q", joined)
	}

	// Existing line breaks and indentation are preserved.
	lines = wrapPlain("- [ ] task\n  - Added: 2026-08-05", 40)
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "  - Added") {
		t.Errorf("structure was not preserved: %v", lines)
	}
	if got := wrapPlain("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("empty input = %v", got)
	}
}

func TestConfidenceBar(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	for _, conf := range []float64{0, 0.5, 1, 1.5, -0.2} {
		bar := st.confidenceBar(conf, 10)
		if got := lipgloss.Width(bar); got != 10 {
			t.Errorf("confidence %v produced a %d-cell bar", conf, got)
		}
	}
}

func TestWrapIndex(t *testing.T) {
	t.Parallel()
	cases := map[[2]int]int{
		{0, 3}:  0,
		{3, 3}:  0,
		{-1, 3}: 2,
		{4, 3}:  1,
		{0, 0}:  0,
	}
	for in, want := range cases {
		if got := wrapIndex(in[0], in[1]); got != want {
			t.Errorf("wrapIndex(%d, %d) = %d, want %d", in[0], in[1], got, want)
		}
	}
}
