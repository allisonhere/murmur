package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// checkRectangular asserts that a rendered screen is a clean box of the
// expected width, which is what keeps the frame from tearing on resize.
func checkRectangular(t *testing.T, name, view string, width int) {
	t.Helper()
	if view == "" {
		t.Fatalf("%s rendered nothing", name)
	}
	for i, line := range strings.Split(strings.TrimRight(view, "\n"), "\n") {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("%s line %d is %d cells wide, want %d:\n%q", name, i, got, width, line)
		}
	}
	t.Logf("%s:\n%s", name, view)
}

// draftForPicker builds the minimum Draft the heading picker reads.
func draftForPicker(headings []model.Heading, section string) *app.Draft {
	d := &app.Draft{DestHeadings: headings}
	d.Routing.Section = section
	return d
}

func TestCaptureScreenRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	m := newCaptureModel(st, "Investigate why the Z13 trackpad is detected as a fallback mouse.")
	m.resize(60, 4)
	checkRectangular(t, "capture", m.view(64, false, "", ""), 64)

	if !strings.Contains(m.view(64, false, "", ""), "What's on your mind?") {
		t.Error("the prompt is missing")
	}
	if m.value() == "" {
		t.Error("the initial text was not applied")
	}
	m.clear()
	if m.value() != "" {
		t.Error("clear did not empty the textarea")
	}

	busy := m.view(64, true, "· ", "Finding a home for that…")
	if !strings.Contains(busy, "Finding a home") {
		t.Error("the busy label is missing")
	}
}

func TestNotePickerRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	notes := []model.Note{
		{RelPath: "Projects/Tidemail.md", FileName: "Tidemail", Title: "Tidemail",
			Tags: []string{"tui", "email"}, ModTime: time.Now()},
		{RelPath: "Projects/Linux/ROG Flow Z13.md", FileName: "ROG Flow Z13", Title: "ROG Flow Z13",
			Tags: []string{"linux"}, ModTime: time.Now()},
	}

	p := newNotePicker(st, notes, "Projects/Tidemail.md")
	view := p.view(70, 10)
	checkRectangular(t, "note picker", view, 70)
	if !strings.Contains(view, "Tidemail") {
		t.Error("the note list is empty")
	}
	if !strings.Contains(view, "(current)") {
		t.Error("the current destination is not marked")
	}

	// Typing narrows the list.
	p.input.SetValue("z13")
	p.refresh()
	if got, ok := p.selected(); !ok || got.value != "Projects/Linux/ROG Flow Z13.md" {
		t.Errorf("search selected %+v", got)
	}

	p.input.SetValue("zzzznothing")
	p.refresh()
	if _, ok := p.selected(); ok {
		t.Error("expected no selection for a non-matching query")
	}
	empty := p.view(70, 10)
	if !strings.Contains(empty, "No matches") {
		t.Error("the empty state is missing")
	}
	checkRectangular(t, "note picker (empty)", empty, 70)
}

func TestHeadingPickerOffersStructuralChoices(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	d := draftForPicker([]model.Heading{
		{Level: 1, Text: "Tasks"},
		{Level: 2, Text: "Hardware"},
		{Level: 2, Text: "Errands"},
	}, "Hardware")

	p := newHeadingPicker(st, d)
	view := p.view(70, 12)
	checkRectangular(t, "heading picker", view, 70)

	for _, want := range []string{"End of note", "Hardware", "Errands", "Inbox"} {
		if !strings.Contains(view, want) {
			t.Errorf("option %q is missing", want)
		}
	}
	// The H1 is the note title, not a section, so it must not be offered.
	if strings.Count(view, "Tasks") > 0 {
		t.Error("an H1 was offered as a section")
	}
}

func TestHistoryScreenRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	m := newHistoryModel(st)
	checkRectangular(t, "history (loading)", m.view(70, 12), 70)

	m.setRecords(nil)
	empty := m.view(70, 12)
	if !strings.Contains(empty, "No captures yet") {
		t.Error("the empty state is missing")
	}
	checkRectangular(t, "history (empty)", empty, 70)

	m.setRecords([]model.CaptureRecord{
		{
			CreatedAt:  time.Now().Add(-30 * time.Minute),
			Raw:        "investigate why the z13 trackpad is a fallback mouse",
			NotePath:   "Projects/Linux/ROG Flow Z13.md",
			Section:    "Trackpad troubleshooting",
			Type:       model.TypeTask,
			Confidence: 0.87,
		},
		{
			CreatedAt: time.Now().Add(-26 * time.Hour),
			Raw:       "buy a replacement ups battery",
			NotePath:  "Inbox/Tasks.md",
			Type:      model.TypeTask,
			Corrected: true,
			Undone:    true,
		},
	})
	view := m.view(70, 12)
	checkRectangular(t, "history", view, 70)
	for _, want := range []string{"trackpad", "30m ago", "corrected", "undone", "87%"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is missing from the history view", want)
		}
	}
}

func TestSetupScreenRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	cfg := config.Default()
	m := newSetupModel(st, cfg)
	view := m.view(70)
	checkRectangular(t, "setup", view, 70)
	if !strings.Contains(view, "Vault path") {
		t.Error("the vault path field is missing")
	}

	// An invalid path must be reported rather than saved.
	m.input.SetValue("/definitely/not/a/real/vault")
	if _, err := m.commit(); err == nil {
		t.Error("commit accepted a missing vault")
	}
}

func TestEditorScreenRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	m := newEditorModel(st, "- [ ] Investigate the trackpad\n  - Added: 2026-08-05")
	m.resize(60, 6)
	checkRectangular(t, "editor", m.view(64), 64)
	if !strings.Contains(m.value(), "Investigate the trackpad") {
		t.Error("the editor did not load the content")
	}
	m.setValue("- edited")
	if m.value() != "- edited" {
		t.Error("setValue did not take effect")
	}
}

func TestFooterChangesWithScreen(t *testing.T) {
	t.Parallel()
	r := New(nil, config.Default(), nil, Options{})
	r.width, r.height = 90, 30

	seen := map[string]bool{}
	for _, s := range []Screen{ScreenSetup, ScreenCapture, ScreenRouting, ScreenNotePicker, ScreenEditor, ScreenHistory, ScreenSaved} {
		r.screen = s
		f := r.footer()
		if f == "" {
			t.Errorf("screen %d has no footer", s)
		}
		seen[f] = true
	}
	if len(seen) < 6 {
		t.Errorf("footers are not screen-specific: %d distinct of 7", len(seen))
	}
}

func TestTerminalTooSmall(t *testing.T) {
	t.Parallel()
	r := New(nil, config.Default(), nil, Options{})
	r.width, r.height = 20, 5

	view := r.View()
	if !strings.Contains(view, "bigger window") {
		t.Errorf("no friendly message for a tiny terminal: %q", view)
	}
}

func TestRoutingScreenRenders(t *testing.T) {
	t.Parallel()
	st := NewStyles(DefaultTheme)

	d := &app.Draft{DestExists: true}
	d.Cleaned = "Investigate why the Z13 trackpad is detected as a fallback mouse and whether hid_asus needs to be patched."
	d.Markdown = "- [ ] Investigate why the Z13 trackpad is detected as a fallback mouse and whether `hid_asus` needs to be patched.\n  - Added: 2026-08-05"
	d.Routing = model.Routing{
		NotePath:    "Projects/Linux/ROG Flow Z13.md",
		Section:     "Trackpad troubleshooting",
		Mode:        model.InsertUnderHeading,
		Type:        model.TypeTask,
		Tags:        []string{"linux", "asus", "z13", "trackpad"},
		Confidence:  0.87,
		Source:      model.SourceRanking,
		Explanation: "matched note name \"ROG Flow Z13\", tags: linux",
		Candidates: []model.Candidate{
			{Note: model.Note{RelPath: "Reference/Fedora Suspend.md"}},
			{Note: model.Note{RelPath: "Inbox/Tasks.md"}},
		},
	}
	d.Suggested = d.Routing

	m := newRoutingModel(st, d)
	view := m.view(72, 34)
	checkRectangular(t, "routing", view, 72)

	for _, want := range []string{"Suggested routing", "Preview", "Note", "Section", "Format", "Confidence", "Tags", "87%"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q is missing from the routing screen", want)
		}
	}

	// Moving focus changes which field is marked.
	m.next()
	if m.field() != fieldSection {
		t.Errorf("next() moved to %v", m.field())
	}
	m.prev()
	m.prev()
	if m.field() != fieldTags {
		t.Errorf("prev() should wrap to the last field, got %v", m.field())
	}

	// Editing tags is modal and round-trips the value.
	m.beginTags()
	if !m.editingTags {
		t.Fatal("tag editing did not start")
	}
	m.tagInput.SetValue("linux, #asus , , new-tag")
	m.commitTags()
	if got := strings.Join(d.Routing.Tags, ","); got != "linux,asus,new-tag" {
		t.Errorf("tags = %q", got)
	}
	if m.editingTags {
		t.Error("tag editing did not finish")
	}
}
