package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// captureModel is the "What's on your mind?" screen.
type captureModel struct {
	st Styles
	ta textarea.Model
}

func newCaptureModel(st Styles, initial string) captureModel {
	ta := textarea.New()
	ta.Placeholder = "Type a thought. Routing hints work here too: @project, #task, >Path/To/Note"
	ta.ShowLineNumbers = false
	ta.Prompt = "  "
	ta.CharLimit = 8000
	ta.SetWidth(70)
	ta.SetHeight(6)

	// Keep the textarea visually quiet: the card provides the structure.
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()
	ta.FocusedStyle.Base = ta.FocusedStyle.Base.UnsetBorderStyle()
	ta.BlurredStyle.Base = ta.BlurredStyle.Base.UnsetBorderStyle()
	ta.FocusedStyle.Placeholder = st.Faint
	ta.BlurredStyle.Placeholder = st.Faint

	if initial != "" {
		ta.SetValue(initial)
	}
	return captureModel{st: st, ta: ta}
}

func (m *captureModel) focus() tea.Cmd { return m.ta.Focus() }

func (m *captureModel) resize(w, h int) {
	m.ta.SetWidth(w)
	m.ta.SetHeight(h)
}

func (m *captureModel) value() string { return m.ta.Value() }

func (m *captureModel) clear() { m.ta.Reset() }

func (m *captureModel) update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		// Enter submits, so an explicit newline needs its own binding. Pasted
		// text still arrives with its newlines intact.
		switch key.String() {
		case "ctrl+j", "alt+enter":
			m.ta.InsertString("\n")
			return nil
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}

func (m *captureModel) view(width int, busy bool, spin, label string) string {
	lines := []string{
		"",
		m.st.Prompt.Render("What's on your mind?"),
		"",
	}
	lines = append(lines, strings.Split(m.ta.View(), "\n")...)
	lines = append(lines, "")
	if busy {
		lines = append(lines, m.st.Muted.Render(spin+label), "")
	}
	return m.st.Card(width, "Murmur", []Section{{Lines: lines}})
}
