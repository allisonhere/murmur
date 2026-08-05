package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/router"
)

// pickerItem is one selectable row.
type pickerItem struct {
	title string
	desc  string
	value string
}

// pickerModel is a fuzzy-searchable list used for both notes and headings.
type pickerModel struct {
	st      Styles
	title   string
	prompt  string
	input   textinput.Model
	all     []pickerItem
	shown   []pickerItem
	cursor  int
	offset  int
	notes   []model.Note // set for the note picker, enabling richer search
	current string
	// allowNew shows a hint that the typed value will create something.
	allowNew string
}

func newPicker(st Styles, title, prompt, placeholder string) pickerModel {
	ti := textinput.New()
	ti.Prompt = st.Accent.Render("› ")
	ti.Placeholder = placeholder
	ti.CharLimit = 200
	return pickerModel{st: st, title: title, prompt: prompt, input: ti}
}

// newNotePicker builds a fuzzy search over the whole vault index.
func newNotePicker(st Styles, notes []model.Note, current string) pickerModel {
	p := newPicker(st, "Choose a note", "Search notes", "path, name, alias, tag or heading")
	p.notes = notes
	p.current = current
	p.allowNew = "note"
	p.refresh()
	return p
}

// newHeadingPicker lists the destination note's headings plus the structural
// choices Murmur supports.
func newHeadingPicker(st Styles, d *app.Draft) pickerModel {
	p := newPicker(st, "Choose a section", "Search headings", "heading name, or type a new one")
	p.current = d.Routing.Section
	p.allowNew = "heading"

	items := []pickerItem{
		{title: "End of note", desc: "append after everything else", value: ""},
	}
	for _, h := range d.DestHeadings {
		if h.Level == 1 {
			continue
		}
		items = append(items, pickerItem{
			title: strings.Repeat("  ", h.Level-2) + h.Text,
			desc:  fmt.Sprintf("existing H%d", h.Level),
			value: h.Text,
		})
	}
	if !hasHeading(d.DestHeadings, "Inbox") {
		items = append(items, pickerItem{title: "Inbox", desc: "create an Inbox section", value: "Inbox"})
	}
	p.all = items
	p.refresh()
	return p
}

func hasHeading(hs []model.Heading, name string) bool {
	for _, h := range hs {
		if strings.EqualFold(h.Text, name) {
			return true
		}
	}
	return false
}

func (p *pickerModel) focus() tea.Cmd { return p.input.Focus() }

func (p *pickerModel) query() string { return p.input.Value() }

func (p *pickerModel) update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "ctrl+p":
			p.move(-1)
			return nil
		case "down", "ctrl+n":
			p.move(1)
			return nil
		case "pgup":
			p.move(-5)
			return nil
		case "pgdown":
			p.move(5)
			return nil
		}
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor, p.offset = 0, 0
		p.refresh()
	}
	return cmd
}

func (p *pickerModel) move(delta int) {
	if len(p.shown) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.shown) {
		p.cursor = len(p.shown) - 1
	}
}

func (p *pickerModel) refresh() {
	q := strings.TrimSpace(p.input.Value())
	if p.notes != nil {
		results := router.FuzzySearch(p.notes, q, 200)
		p.shown = make([]pickerItem, 0, len(results))
		for _, r := range results {
			p.shown = append(p.shown, pickerItem{
				title: r.Note.RelPath,
				desc:  r.Reason,
				value: r.Note.RelPath,
			})
		}
		return
	}

	if q == "" {
		p.shown = p.all
		return
	}
	lower := strings.ToLower(q)
	p.shown = make([]pickerItem, 0, len(p.all))
	for _, item := range p.all {
		if strings.Contains(strings.ToLower(item.title), lower) {
			p.shown = append(p.shown, item)
		}
	}
}

func (p *pickerModel) selected() (pickerItem, bool) {
	if p.cursor < 0 || p.cursor >= len(p.shown) {
		return pickerItem{}, false
	}
	return p.shown[p.cursor], true
}

func (p *pickerModel) view(width, height int) string {
	inner := width - 4
	// Each row renders as a title plus a reason line, so the window holds half
	// as many items as it has lines.
	height /= 2
	if height < 3 {
		height = 3
	}

	// Keep the cursor inside the visible window.
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+height {
		p.offset = p.cursor - height + 1
	}

	lines := []string{"", "  " + p.input.View(), ""}

	if len(p.shown) == 0 {
		lines = append(lines,
			"  "+p.st.Muted.Render("No matches."),
			"",
			"  "+p.st.Faint.Render("Press Enter to use what you typed as a new "+p.allowNew+"."),
			"")
		return p.st.Card(width, p.title, []Section{{Lines: lines}})
	}

	end := p.offset + height
	if end > len(p.shown) {
		end = len(p.shown)
	}
	for i := p.offset; i < end; i++ {
		item := p.shown[i]
		marker := "  "
		titleStyle := p.st.Value
		if i == p.cursor {
			marker = p.st.Accent.Render("▸ ")
			titleStyle = p.st.Selected
		}
		suffix := ""
		if item.value == p.current {
			suffix = p.st.Faint.Render("  (current)")
		}
		lines = append(lines, marker+titleStyle.Render(truncPlain(item.title, inner-6))+suffix)
		if item.desc != "" {
			lines = append(lines, "    "+p.st.Faint.Render(truncPlain(item.desc, inner-6)))
		}
	}

	lines = append(lines, "")
	if len(p.shown) > height {
		lines = append(lines, "  "+p.st.Faint.Render(fmt.Sprintf("%d of %d", p.cursor+1, len(p.shown))), "")
	}
	return p.st.Card(width, p.title, []Section{{Lines: lines}})
}
