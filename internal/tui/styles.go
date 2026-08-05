// Package tui implements Murmur's Bubble Tea interface. Each screen lives in
// its own file and exposes Update/View methods; root.go owns the transitions.
package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds every colour Murmur uses. The palette is deliberately small: two
// accents, three text weights and one warning colour.
type Theme struct {
	Accent   lipgloss.AdaptiveColor
	Accent2  lipgloss.AdaptiveColor
	Text     lipgloss.AdaptiveColor
	Muted    lipgloss.AdaptiveColor
	Faint    lipgloss.AdaptiveColor
	Border   lipgloss.AdaptiveColor
	Warn     lipgloss.AdaptiveColor
	Good     lipgloss.AdaptiveColor
	Selected lipgloss.AdaptiveColor
}

// DefaultTheme is a calm palette that reads well on light and dark terminals.
var DefaultTheme = Theme{
	Accent:   lipgloss.AdaptiveColor{Light: "#3b6ea5", Dark: "#7aa2f7"},
	Accent2:  lipgloss.AdaptiveColor{Light: "#2c7a6b", Dark: "#7dcfb6"},
	Text:     lipgloss.AdaptiveColor{Light: "#1f2430", Dark: "#c8d3f5"},
	Muted:    lipgloss.AdaptiveColor{Light: "#5c6370", Dark: "#8a91a8"},
	Faint:    lipgloss.AdaptiveColor{Light: "#9099a8", Dark: "#5b637d"},
	Border:   lipgloss.AdaptiveColor{Light: "#c3c8d1", Dark: "#3b4261"},
	Warn:     lipgloss.AdaptiveColor{Light: "#a8590a", Dark: "#e0af68"},
	Good:     lipgloss.AdaptiveColor{Light: "#2c7a3f", Dark: "#9ece6a"},
	Selected: lipgloss.AdaptiveColor{Light: "#e8edf7", Dark: "#2a3050"},
}

// Styles are the reusable text styles derived from a Theme.
type Styles struct {
	Title     lipgloss.Style
	Border    lipgloss.Style
	Label     lipgloss.Style
	Value     lipgloss.Style
	Muted     lipgloss.Style
	Faint     lipgloss.Style
	Accent    lipgloss.Style
	Prompt    lipgloss.Style
	Preview   lipgloss.Style
	Selected  lipgloss.Style
	Focused   lipgloss.Style
	Help      lipgloss.Style
	HelpKey   lipgloss.Style
	Error     lipgloss.Style
	Warn      lipgloss.Style
	Good      lipgloss.Style
	Bar       lipgloss.Style
	BarFilled lipgloss.Style
}

// NewStyles builds the style set for a theme.
func NewStyles(t Theme) Styles {
	return Styles{
		Title:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Border:    lipgloss.NewStyle().Foreground(t.Border),
		Label:     lipgloss.NewStyle().Foreground(t.Muted),
		Value:     lipgloss.NewStyle().Foreground(t.Text),
		Muted:     lipgloss.NewStyle().Foreground(t.Muted),
		Faint:     lipgloss.NewStyle().Foreground(t.Faint),
		Accent:    lipgloss.NewStyle().Foreground(t.Accent),
		Prompt:    lipgloss.NewStyle().Foreground(t.Accent2).Bold(true),
		Preview:   lipgloss.NewStyle().Foreground(t.Text),
		Selected:  lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Focused:   lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Help:      lipgloss.NewStyle().Foreground(t.Faint),
		HelpKey:   lipgloss.NewStyle().Foreground(t.Muted).Bold(true),
		Error:     lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b3261e", Dark: "#f7768e"}),
		Warn:      lipgloss.NewStyle().Foreground(t.Warn),
		Good:      lipgloss.NewStyle().Foreground(t.Good),
		Bar:       lipgloss.NewStyle().Foreground(t.Faint),
		BarFilled: lipgloss.NewStyle().Foreground(t.Accent2),
	}
}

// ---------------------------------------------------------------- card layout

// Section is one labelled region inside a card.
type Section struct {
	Title string
	Lines []string
}

// Card renders the bordered frame Murmur uses for every screen. Separators are
// drawn to touch the frame, which a plain Lip Gloss border cannot do.
func (s Styles) Card(width int, title string, sections []Section) string {
	if width < 24 {
		width = 24
	}
	inner := width - 4 // two border cells plus one space of padding each side
	var b strings.Builder

	b.WriteString(s.rule("╭", "╮", title, width, true))
	b.WriteByte('\n')

	for i, sec := range sections {
		if i > 0 {
			b.WriteString(s.rule("├", "┤", sec.Title, width, false))
			b.WriteByte('\n')
		}
		for _, line := range sec.Lines {
			b.WriteString(s.row(line, inner))
			b.WriteByte('\n')
		}
	}
	b.WriteString(s.rule("╰", "╯", "", width, false))
	return b.String()
}

func (s Styles) row(content string, inner int) string {
	edge := s.Border.Render("│")
	return edge + " " + padTo(content, inner) + " " + edge
}

func (s Styles) rule(left, right, title string, width int, strong bool) string {
	if title == "" {
		return s.Border.Render(left + strings.Repeat("─", width-2) + right)
	}
	// A title longer than the frame would push the corner off the end, so it
	// is truncated to leave room for at least one fill character.
	if max := width - 6; lipgloss.Width(title) > max {
		title = truncPlain(title, max)
	}
	label := " " + title + " "
	style := s.Muted
	if strong {
		style = s.Title
	}
	fill := width - 2 - 1 - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return s.Border.Render(left+"─") + style.Render(label) + s.Border.Render(strings.Repeat("─", fill)+right)
}

// padTo pads or truncates a possibly-styled string to an exact display width.
func padTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	d := w - lipgloss.Width(s)
	if d >= 0 {
		return s + strings.Repeat(" ", d)
	}
	return truncateStyled(s, w)
}

// truncateStyled cuts a string to a display width, skipping over ANSI escape
// sequences so styling is never split in half.
func truncateStyled(s string, w int) string {
	if w <= 1 {
		return ""
	}
	var b strings.Builder
	width := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if unicode.IsLetter(r) {
				inEscape = false
			}
			continue
		}
		if width >= w-1 {
			b.WriteString("…\x1b[0m")
			return b.String()
		}
		b.WriteRune(r)
		width++
	}
	return b.String()
}

// truncPlain shortens unstyled text with an ellipsis.
func truncPlain(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	if len(runes) <= w-1 {
		return s
	}
	return string(runes[:w-1]) + "…"
}

// wrapPlain word-wraps unstyled text, preserving existing line breaks.
func wrapPlain(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		// Preserve leading indentation so nested Markdown keeps its shape.
		indent := para[:len(para)-len(strings.TrimLeft(para, " \t"))]
		line := indent
		for _, word := range strings.Fields(para) {
			candidate := word
			if strings.TrimSpace(line) != "" {
				candidate = line + " " + word
			} else {
				candidate = line + word
			}
			if lipgloss.Width(candidate) > w && strings.TrimSpace(line) != "" {
				out = append(out, line)
				line = indent + "  " + word
				continue
			}
			line = candidate
		}
		out = append(out, line)
	}
	return out
}

// keyHelp renders the footer keybinding hints.
func (s Styles) keyHelp(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, s.HelpKey.Render(p[0])+" "+s.Help.Render(p[1]))
	}
	return " " + strings.Join(parts, s.Help.Render("   "))
}

// confidenceBar renders a small, quiet confidence indicator.
func (s Styles) confidenceBar(conf float64, width int) string {
	if width < 4 {
		width = 4
	}
	filled := int(conf*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return s.BarFilled.Render(strings.Repeat("▰", filled)) +
		s.Bar.Render(strings.Repeat("▱", width-filled))
}
