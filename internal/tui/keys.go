package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/obsidian"
	"github.com/alliebayless/murmur/internal/router"
)

func (r *Root) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if r.busy {
		// While a background task runs, only quitting is allowed.
		return r, nil
	}
	r.status, r.isError = "", false

	switch r.screen {
	case ScreenSetup:
		return r.keySetup(msg)
	case ScreenCapture:
		return r.keyCapture(msg)
	case ScreenRouting:
		return r.keyRouting(msg)
	case ScreenNotePicker:
		return r.keyNotePicker(msg)
	case ScreenHeadingPicker:
		return r.keyHeadingPicker(msg)
	case ScreenEditor:
		return r.keyEditor(msg)
	case ScreenHistory:
		return r.keyHistory(msg)
	case ScreenSaved:
		return r.keySaved(msg)
	}
	return r, nil
}

func (r *Root) keySetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		r.quit = true
		return r, tea.Quit
	case "enter":
		cfg, err := r.setup.commit()
		if err != nil {
			return r, failure(err)
		}
		return r, r.openApp(cfg)
	}
	return r, r.setup.update(msg)
}

func (r *Root) keyCapture(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if strings.TrimSpace(r.capture.value()) != "" {
			r.capture.clear()
			return r, status("Cleared.")
		}
		r.quit = true
		return r, tea.Quit
	case "enter":
		text := r.capture.value()
		if strings.TrimSpace(text) == "" {
			return r, failure(errors.New("nothing to capture yet — type a thought first"))
		}
		return r, r.prepare(text)
	case "ctrl+h":
		r.back = ScreenCapture
		r.screen = ScreenHistory
		r.history = newHistoryModel(r.st)
		return r, r.loadHistory()
	}
	return r, r.capture.update(msg)
}

func (r *Root) keyRouting(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if r.routing.editingTags {
		switch msg.String() {
		case "enter":
			r.routing.commitTags()
			return r, status("Tags updated.")
		case "esc":
			r.routing.cancelTags()
			return r, nil
		}
		return r, r.routing.update(msg)
	}

	switch msg.String() {
	case "enter":
		return r, r.save()
	case "esc":
		r.screen = ScreenCapture
		return r, r.capture.focus()
	case "q":
		r.quit = true
		return r, tea.Quit
	case "tab", "down":
		r.routing.next()
		return r, nil
	case "shift+tab", "up":
		r.routing.prev()
		return r, nil
	case "ctrl+e":
		r.editor = newEditorModel(r.st, r.draft.Markdown)
		r.editor.resize(r.contentWidth(), r.editorHeight())
		r.screen = ScreenEditor
		return r, r.editor.focus()
	case "ctrl+h":
		r.back = ScreenRouting
		r.screen = ScreenHistory
		r.history = newHistoryModel(r.st)
		return r, r.loadHistory()
	case " ", "e", "right", "l":
		return r.activateField(msg.String())
	case "left", "h":
		return r.cycleField(-1)
	}
	return r, nil
}

// activateField opens the picker or cycles the value for the focused field.
func (r *Root) activateField(key string) (tea.Model, tea.Cmd) {
	switch r.routing.field() {
	case fieldNote:
		if key == "right" || key == "l" {
			return r.cycleField(1)
		}
		r.notePick = newNotePicker(r.st, r.app.Engine.Notes(), r.draft.Routing.NotePath)
		r.screen = ScreenNotePicker
		return r, r.notePick.focus()
	case fieldSection:
		if key == "right" || key == "l" {
			return r.cycleField(1)
		}
		r.headPick = newHeadingPicker(r.st, r.draft)
		r.screen = ScreenHeadingPicker
		return r, r.headPick.focus()
	case fieldType:
		return r.cycleField(1)
	case fieldTags:
		r.routing.beginTags()
		return r, r.routing.tagInput.Focus()
	}
	return r, nil
}

// cycleField steps a field's value without leaving the routing screen.
func (r *Root) cycleField(delta int) (tea.Model, tea.Cmd) {
	switch r.routing.field() {
	case fieldNote:
		alts := r.draft.Routing.Candidates
		if len(alts) == 0 {
			return r, status("No other candidates. Press space to search the vault.")
		}
		r.routing.altIndex = wrapIndex(r.routing.altIndex+delta, len(alts)+1)
		target := r.draft.Suggested.NotePath
		if r.routing.altIndex > 0 {
			target = alts[r.routing.altIndex-1].Note.RelPath
		}
		if err := r.draft.SetDestination(target); err != nil {
			return r, failure(err)
		}
		r.draft.Render()
		return r, nil
	case fieldType:
		types := model.AllContentTypes()
		idx := 0
		for i, t := range types {
			if t == r.draft.Routing.Type {
				idx = i
			}
		}
		next := types[wrapIndex(idx+delta, len(types))]
		r.draft.SetType(next)
		// A different type may belong under a different daily heading.
		if err := r.draft.RefreshDestination(); err != nil {
			return r, failure(err)
		}
		return r, nil
	}
	return r, nil
}

func wrapIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i % n) + n) % n
}

func (r *Root) keyNotePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		r.screen = ScreenRouting
		return r, nil
	case "enter":
		item, ok := r.notePick.selected()
		if !ok {
			// Nothing matched: treat the query as a new note path.
			query := strings.TrimSpace(r.notePick.query())
			if query == "" {
				r.screen = ScreenRouting
				return r, nil
			}
			if err := r.draft.SetDestination(router.NormaliseNotePath(query)); err != nil {
				return r, failure(err)
			}
			r.screen = ScreenRouting
			r.routing.altIndex = -1
			return r, status("Will create " + r.draft.Routing.NotePath)
		}
		if err := r.draft.SetDestination(item.value); err != nil {
			return r, failure(err)
		}
		r.draft.Render()
		r.routing.altIndex = -1
		r.screen = ScreenRouting
		return r, nil
	}
	return r, r.notePick.update(msg)
}

func (r *Root) keyHeadingPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		r.screen = ScreenRouting
		return r, nil
	case "enter":
		item, ok := r.headPick.selected()
		if !ok {
			query := strings.TrimSpace(r.headPick.query())
			if query == "" {
				r.screen = ScreenRouting
				return r, nil
			}
			if err := r.draft.SetSection(query); err != nil {
				return r, failure(err)
			}
			r.screen = ScreenRouting
			return r, status("Will create ## " + query)
		}
		if err := r.draft.SetSection(item.value); err != nil {
			return r, failure(err)
		}
		r.screen = ScreenRouting
		return r, nil
	}
	return r, r.headPick.update(msg)
}

func (r *Root) keyEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		r.screen = ScreenRouting
		return r, nil
	case "ctrl+s":
		r.draft.SetMarkdown(r.editor.value())
		r.screen = ScreenRouting
		return r, status("Preview updated.")
	case "ctrl+r":
		r.draft.ManualMarkdown = false
		r.draft.Render()
		r.editor.setValue(r.draft.Markdown)
		return r, status("Reset to the generated Markdown.")
	}
	return r, r.editor.update(msg)
}

func (r *Root) keyHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+h":
		r.screen = r.back
		if r.screen == ScreenCapture {
			return r, r.capture.focus()
		}
		return r, nil
	}
	return r, r.history.update(msg)
}

func (r *Root) keySaved(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter":
		r.quit = true
		return r, tea.Quit
	case "o":
		if r.result == nil {
			return r, nil
		}
		if err := obsidian.Open(r.result.URI); err != nil {
			return r, failure(err)
		}
		return r, status("Opening in Obsidian…")
	case "n":
		r.draft = nil
		r.result = nil
		r.capture.clear()
		r.screen = ScreenCapture
		return r, r.capture.focus()
	case "u":
		plan, err := r.app.PlanUndo()
		if err != nil {
			return r, failure(err)
		}
		if err := r.app.ApplyUndo(plan); err != nil {
			return r, failure(err)
		}
		r.result = nil
		r.draft = nil
		r.capture.clear()
		r.screen = ScreenCapture
		return r, status("Undone: " + plan.Tx.Path + " restored.")
	case "h", "ctrl+h":
		r.back = ScreenSaved
		r.screen = ScreenHistory
		r.history = newHistoryModel(r.st)
		return r, r.loadHistory()
	}
	return r, nil
}

func isConflict(err error) bool {
	var c *app.ConflictError
	return errors.As(err, &c)
}

func friendlySaveError(err error) string {
	var c *app.ConflictError
	if errors.As(err, &c) {
		return c.Path + " changed on disk. The preview has been rebuilt — check it and press Enter again."
	}
	return err.Error()
}
