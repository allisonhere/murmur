package tui

import (
	"strings"
)

func (r *Root) savedView() string {
	width := r.cardWidth()
	inner := width - 4
	if r.result == nil {
		return r.st.Card(width, "Saved", []Section{{Lines: []string{"", "  " + r.st.Muted.Render("Nothing saved."), ""}}})
	}
	res := r.result

	lines := []string{
		"",
		"  " + r.st.Good.Render("✓ ") + r.st.Value.Render(truncPlain(res.Path, inner-6)),
	}
	if res.Section != "" {
		lines = append(lines, "    "+r.st.Muted.Render("under ## "+truncPlain(res.Section, inner-14)))
	} else {
		lines = append(lines, "    "+r.st.Muted.Render("at the end of the note"))
	}
	if res.Created {
		lines = append(lines, "    "+r.st.Faint.Render("note created"))
	}
	lines = append(lines, "")

	if r.draft != nil {
		for _, l := range wrapPlain(r.draft.Markdown, inner-4) {
			lines = append(lines, "  "+r.st.Preview.Render(l))
		}
		lines = append(lines, "")
	}
	return r.st.Card(width, "Saved", []Section{{Lines: lines}})
}

// footer renders the contextual keybinding help.
func (r *Root) footer() string {
	switch r.screen {
	case ScreenSetup:
		return r.st.keyHelp([2]string{"enter", "continue"}, [2]string{"esc", "quit"})
	case ScreenCapture:
		return r.st.keyHelp(
			[2]string{"enter", "route"},
			[2]string{"ctrl+j", "newline"},
			[2]string{"ctrl+h", "history"},
			[2]string{"esc", "clear"},
			[2]string{"ctrl+c", "quit"},
		)
	case ScreenRouting:
		if r.routing.editingTags {
			return r.st.keyHelp([2]string{"enter", "apply tags"}, [2]string{"esc", "cancel"})
		}
		return r.st.keyHelp(
			[2]string{"enter", "save"},
			[2]string{"tab", "fields"},
			[2]string{fieldAction(r.routing.field()), fieldActionLabel(r.routing.field())},
			[2]string{"ctrl+e", "edit preview"},
			[2]string{"esc", "back"},
			[2]string{"q", "quit"},
		)
	case ScreenNotePicker, ScreenHeadingPicker:
		return r.st.keyHelp(
			[2]string{"↑↓", "move"},
			[2]string{"enter", "choose"},
			[2]string{"type", "search"},
			[2]string{"esc", "back"},
		)
	case ScreenEditor:
		return r.st.keyHelp(
			[2]string{"ctrl+s", "apply"},
			[2]string{"ctrl+r", "regenerate"},
			[2]string{"esc", "discard"},
		)
	case ScreenHistory:
		return r.st.keyHelp([2]string{"↑↓", "move"}, [2]string{"esc", "back"})
	case ScreenSaved:
		return r.st.keyHelp(
			[2]string{"o", "open in Obsidian"},
			[2]string{"u", "undo"},
			[2]string{"n", "new capture"},
			[2]string{"q", "quit"},
		)
	}
	return ""
}

func fieldAction(f routingField) string {
	switch f {
	case fieldNote:
		return "space"
	case fieldSection:
		return "space"
	case fieldType:
		return "← →"
	default:
		return "e"
	}
}

func fieldActionLabel(f routingField) string {
	switch f {
	case fieldNote:
		return "search notes"
	case fieldSection:
		return "choose section"
	case fieldType:
		return "change format"
	default:
		return "edit tags"
	}
}

// Result exposes what was saved, so the CLI can print a summary after the TUI
// exits.
func (r *Root) Result() (string, bool) {
	if r.result == nil {
		return "", false
	}
	return strings.TrimSpace(r.result.Summary()), true
}
