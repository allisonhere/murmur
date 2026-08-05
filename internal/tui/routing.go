package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/model"
)

type routingField int

const (
	fieldNote routingField = iota
	fieldSection
	fieldType
	fieldTags
	fieldCount
)

// routingModel is the confirmation and editing screen.
type routingModel struct {
	st    Styles
	draft *app.Draft

	focus routingField
	// altIndex tracks which alternative is selected: 0 is the original
	// suggestion, -1 means "chosen from the picker".
	altIndex int

	editingTags bool
	tagInput    textinput.Model
}

func newRoutingModel(st Styles, d *app.Draft) routingModel {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "linux, asus, z13"
	ti.CharLimit = 200

	return routingModel{st: st, draft: d, tagInput: ti}
}

func (m *routingModel) field() routingField { return m.focus }

func (m *routingModel) next() { m.focus = routingField(wrapIndex(int(m.focus)+1, int(fieldCount))) }
func (m *routingModel) prev() { m.focus = routingField(wrapIndex(int(m.focus)-1, int(fieldCount))) }

func (m *routingModel) beginTags() {
	m.editingTags = true
	m.tagInput.SetValue(strings.Join(m.draft.Routing.Tags, ", "))
	m.tagInput.CursorEnd()
}

func (m *routingModel) commitTags() {
	var tags []string
	for _, t := range strings.Split(m.tagInput.Value(), ",") {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t != "" {
			tags = append(tags, t)
		}
	}
	m.draft.SetTags(tags)
	m.editingTags = false
	m.tagInput.Blur()
}

func (m *routingModel) cancelTags() {
	m.editingTags = false
	m.tagInput.Blur()
}

func (m *routingModel) update(msg tea.Msg) tea.Cmd {
	if m.editingTags {
		var cmd tea.Cmd
		m.tagInput, cmd = m.tagInput.Update(msg)
		return cmd
	}
	return nil
}

func (m *routingModel) view(width, height int) string {
	if m.draft == nil {
		return m.st.Card(width, "Murmur", []Section{{Lines: []string{"", m.st.Muted.Render("Nothing to route."), ""}}})
	}
	inner := width - 4
	d := m.draft

	thought := []string{""}
	for _, l := range wrapPlain(d.Cleaned, inner-2) {
		thought = append(thought, "  "+m.st.Value.Render(l))
	}
	thought = append(thought, "")
	if d.Hints.Any() {
		thought = append(thought, "  "+m.st.Faint.Render(hintSummary(d.Hints)), "")
	}

	routing := m.routingLines(inner)
	preview := m.previewLines(inner, height)

	return m.st.Card(width, "Murmur", []Section{
		{Lines: thought},
		{Title: "Suggested routing", Lines: routing},
		{Title: "Preview", Lines: preview},
	})
}

func (m *routingModel) routingLines(inner int) []string {
	d := m.draft
	labelW := 12
	valueW := inner - labelW - 2

	row := func(f routingField, label, value string, extra string) string {
		marker := "  "
		style := m.st.Value
		if m.focus == f {
			marker = m.st.Accent.Render("▸ ")
			style = m.st.Focused
		}
		line := marker + m.st.Label.Render(padTo(label, labelW)) + style.Render(truncPlain(value, valueW))
		if extra != "" {
			line += " " + m.st.Faint.Render(extra)
		}
		return line
	}

	notePath := d.Routing.NotePath
	noteExtra := ""
	if !d.DestExists {
		noteExtra = "(will be created)"
	} else if n := len(d.Routing.Candidates); n > 0 && m.focus == fieldNote {
		noteExtra = fmt.Sprintf("← → %d alternative%s", n, plural(n))
	}

	section := d.Routing.Section
	sectionExtra := ""
	switch {
	case section == "":
		section = "End of note"
	case d.Routing.Mode == model.InsertCreateHeading:
		sectionExtra = "(new heading)"
	}

	tags := "none"
	if len(d.Routing.Tags) > 0 {
		tags = "#" + strings.Join(d.Routing.Tags, " #")
	}

	lines := []string{
		"",
		row(fieldNote, "Note", notePath, noteExtra),
		row(fieldSection, "Section", section, sectionExtra),
		row(fieldType, "Format", d.Routing.Type.Label(), typeHint(m.focus)),
	}

	conf := fmt.Sprintf("%3.0f%%  %s", d.Routing.Confidence*100, m.st.confidenceBar(d.Routing.Confidence, 10))
	lines = append(lines, "  "+m.st.Label.Render(padTo("Confidence", labelW))+conf)

	if m.editingTags {
		m.tagInput.Width = valueW
		lines = append(lines, m.st.Accent.Render("▸ ")+m.st.Label.Render(padTo("Tags", labelW))+m.tagInput.View())
	} else {
		lines = append(lines, row(fieldTags, "Tags", tags, tagHint(m.focus)))
	}

	if d.Routing.Explanation != "" {
		lines = append(lines, "")
		for i, l := range wrapPlain(sourceLabel(d.Routing.Source)+d.Routing.Explanation, inner-4) {
			prefix := "  "
			if i > 0 {
				prefix = "    "
			}
			lines = append(lines, prefix+m.st.Faint.Render(l))
		}
	}

	// Show the alternatives inline while the note field has focus.
	if m.focus == fieldNote && len(d.Routing.Candidates) > 0 {
		lines = append(lines, "")
		for _, c := range d.Routing.Candidates {
			lines = append(lines, "    "+m.st.Faint.Render("· "+truncPlain(c.Note.RelPath, inner-8)))
		}
	}

	return append(lines, "")
}

func (m *routingModel) previewLines(inner, height int) []string {
	lines := []string{""}
	budget := height - 22
	if budget < 4 {
		budget = 4
	}
	if budget > 14 {
		budget = 14
	}

	rendered := wrapPlain(m.draft.Markdown, inner-2)
	for i, l := range rendered {
		if i >= budget {
			lines = append(lines, "  "+m.st.Faint.Render(fmt.Sprintf("… %d more lines (Ctrl+E to edit)", len(rendered)-budget)))
			break
		}
		lines = append(lines, "  "+m.st.Preview.Render(l))
	}
	return append(lines, "")
}

func typeHint(f routingField) string {
	if f == fieldType {
		return "← → change"
	}
	return ""
}

func tagHint(f routingField) string {
	if f == fieldTags {
		return "e edit"
	}
	return ""
}

func sourceLabel(s model.RoutingSource) string {
	switch s {
	case model.SourceExplicit:
		return "You said: "
	case model.SourceRule:
		return "Rule: "
	case model.SourceAI:
		return "AI: "
	case model.SourceFallback:
		return "Fallback: "
	default:
		return "Why: "
	}
}

func hintSummary(h model.Hints) string {
	var parts []string
	if h.Path != "" {
		parts = append(parts, "destination "+h.Path)
	}
	if h.Project != "" {
		parts = append(parts, "@"+h.Project)
	}
	if h.Type != "" {
		parts = append(parts, "#"+string(h.Type))
	}
	for _, t := range h.Tags {
		parts = append(parts, "#"+t)
	}
	if len(parts) == 0 {
		return ""
	}
	return "hints: " + strings.Join(parts, ", ")
}

// plural returns the "s" suffix for a count.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
