package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/model"
)

// historyModel lists recent captures.
type historyModel struct {
	st      Styles
	records []model.CaptureRecord
	cursor  int
	offset  int
	loaded  bool
}

func newHistoryModel(st Styles) historyModel {
	return historyModel{st: st}
}

func (m *historyModel) setRecords(recs []model.CaptureRecord) {
	m.records = recs
	m.loaded = true
	m.cursor, m.offset = 0, 0
}

func (m *historyModel) update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "up", "k", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "ctrl+n":
		if m.cursor < len(m.records)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = maxInt(0, len(m.records)-1)
	}
	return nil
}

func (m *historyModel) view(width, height int) string {
	inner := width - 4
	if !m.loaded {
		return m.st.Card(width, "History", []Section{{Lines: []string{"", "  " + m.st.Muted.Render("Loading…"), ""}}})
	}
	if len(m.records) == 0 {
		return m.st.Card(width, "History", []Section{{Lines: []string{
			"",
			"  " + m.st.Muted.Render("No captures yet."),
			"",
			"  " + m.st.Faint.Render("Everything you save with Murmur shows up here,"),
			"  " + m.st.Faint.Render("along with where it went and whether you corrected it."),
			"",
		}}})
	}

	rows := height / 3
	if rows < 2 {
		rows = 2
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	end := minInt(m.offset+rows, len(m.records))

	lines := []string{""}
	for i := m.offset; i < end; i++ {
		rec := m.records[i]
		marker := "  "
		style := m.st.Value
		if i == m.cursor {
			marker = m.st.Accent.Render("▸ ")
			style = m.st.Selected
		}

		meta := []string{relativeTime(rec.CreatedAt), rec.Type.Label(), fmt.Sprintf("%.0f%%", rec.Confidence*100)}
		if rec.Corrected {
			meta = append(meta, "corrected")
		}
		if rec.Undone {
			meta = append(meta, "undone")
		}

		lines = append(lines,
			marker+style.Render(truncPlain(oneLine(rec.Raw), inner-4)),
			"    "+m.st.Label.Render(truncPlain(destination(rec), inner-6)),
			"    "+m.st.Faint.Render(strings.Join(meta, " · ")),
		)
	}
	lines = append(lines, "")
	if len(m.records) > rows {
		lines = append(lines, "  "+m.st.Faint.Render(fmt.Sprintf("%d of %d", m.cursor+1, len(m.records))), "")
	}
	return m.st.Card(width, "History", []Section{{Lines: lines}})
}

func destination(rec model.CaptureRecord) string {
	if rec.Section == "" {
		return rec.NotePath
	}
	return rec.NotePath + " › " + rec.Section
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2 Jan")
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
