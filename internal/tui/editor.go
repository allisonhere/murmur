package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// editorModel lets the user edit the exact Markdown that will be written.
//
// ready guards against operating on the zero value: a textarea.Model is only
// usable after textarea.New, and the root model may receive a resize before the
// editor has ever been opened.
type editorModel struct {
	st    Styles
	ta    textarea.Model
	ready bool
}

func newEditorModel(st Styles, content string) editorModel {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = st.Faint.Render("│ ")
	ta.CharLimit = 20000
	ta.SetWidth(70)
	ta.SetHeight(10)
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()
	ta.FocusedStyle.Base = ta.FocusedStyle.Base.UnsetBorderStyle()
	ta.BlurredStyle.Base = ta.BlurredStyle.Base.UnsetBorderStyle()
	ta.SetValue(content)
	return editorModel{st: st, ta: ta, ready: true}
}

func (m *editorModel) focus() tea.Cmd {
	if !m.ready {
		return nil
	}
	return m.ta.Focus()
}

func (m *editorModel) resize(w, h int) {
	if !m.ready {
		return
	}
	m.ta.SetWidth(w)
	m.ta.SetHeight(h)
}

func (m *editorModel) value() string { return m.ta.Value() }

func (m *editorModel) setValue(s string) { m.ta.SetValue(s) }

func (m *editorModel) update(msg tea.Msg) tea.Cmd {
	if !m.ready {
		return nil
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}

func (m *editorModel) view(width int) string {
	if !m.ready {
		return m.st.Card(width, "Edit preview", []Section{{Lines: []string{""}}})
	}
	lines := []string{""}
	lines = append(lines, strings.Split(m.ta.View(), "\n")...)
	lines = append(lines, "")
	return m.st.Card(width, "Edit preview", []Section{{Lines: lines}})
}
