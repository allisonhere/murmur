package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// newFlowApp opens a real application over a copy of the sample vault so the
// whole capture flow can be driven without a terminal.
func newFlowApp(t *testing.T) (*app.App, config.Config, string) {
	t.Helper()

	src := filepath.Join("..", "..", "testdata", "vault")
	root := t.TempDir()
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy vault: %v", err)
	}

	cfgDir, dataDir := t.TempDir(), t.TempDir()
	t.Setenv("MURMUR_CONFIG_DIR", cfgDir)
	t.Setenv("MURMUR_DATA_DIR", dataDir)

	cfg := config.Default()
	cfg.VaultPath = root
	cfg.VaultName = "TestVault"
	cfg.SetPath(filepath.Join(cfgDir, "config.yaml"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	a, err := app.Open(cfg, app.Options{DBPath: filepath.Join(dataDir, "murmur.db")})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, cfg, root
}

// send delivers a message and discards the resulting command. Most commands
// here are cursor blinks and spinner ticks, which sleep; running them would
// only slow the test down.
func send(t *testing.T, r *Root, msg tea.Msg) {
	t.Helper()
	r.Update(msg)
}

// sendAndRun delivers a message, runs the single command it produces and feeds
// the result back in. This is how the runtime delivers the outcome of the
// background work that Enter kicks off (routing and saving).
func sendAndRun(t *testing.T, r *Root, msg tea.Msg) {
	t.Helper()
	_, cmd := r.Update(msg)
	if cmd == nil {
		return
	}
	if out := cmd(); out != nil {
		r.Update(out)
	}
}

// newFlowRoot builds a Root that is initialised and sized, matching what the
// Bubble Tea runtime does before the first keystroke arrives.
func newFlowRoot(t *testing.T, a *app.App, cfg config.Config) *Root {
	t.Helper()
	r := New(a, cfg, nil, Options{})
	r.Init() // focuses the capture box; the returned commands only animate
	send(t, r, tea.WindowSizeMsg{Width: 100, Height: 40})
	return r
}

func typeText(t *testing.T, r *Root, text string) {
	t.Helper()
	for _, ch := range text {
		send(t, r, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
}

func TestCaptureFlowWritesToTheVault(t *testing.T) {
	a, cfg, root := newFlowApp(t)
	// A resize arrives before any screen beyond capture has been built. This
	// must not touch uninitialised components.
	r := newFlowRoot(t, a, cfg)

	typeText(t, r, "investigate why the z13 trackpad is a fallback mouse")
	if got := r.capture.value(); !strings.Contains(got, "trackpad") {
		t.Fatalf("the capture box holds %q", got)
	}

	// Enter routes the thought.
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenRouting {
		t.Fatalf("screen = %v, want routing", r.screen)
	}
	if r.draft == nil {
		t.Fatal("no draft was prepared")
	}
	if r.draft.Routing.NotePath != "Projects/Linux/ROG Flow Z13.md" {
		t.Errorf("routed to %s", r.draft.Routing.NotePath)
	}

	// The routing screen must render at this size.
	view := r.View()
	if !strings.Contains(view, "Suggested routing") || !strings.Contains(view, "Preview") {
		t.Error("the routing screen did not render its sections")
	}

	// Enter again saves.
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenSaved {
		t.Fatalf("screen = %v, want saved (status: %s)", r.screen, r.status)
	}

	data, err := os.ReadFile(filepath.Join(root, "Projects", "Linux", "ROG Flow Z13.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fallback mouse") {
		t.Errorf("the thought was not written:\n%s", data)
	}

	summary, ok := r.Result()
	if !ok || !strings.HasPrefix(summary, "Saved to Projects/Linux/ROG Flow Z13.md") {
		t.Errorf("summary = %q (ok=%v)", summary, ok)
	}
}

func TestResizeBeforeEveryScreenExists(t *testing.T) {
	a, cfg, _ := newFlowApp(t)
	r := New(a, cfg, nil, Options{})

	// Resizing repeatedly, including to sizes below the minimum, must never
	// panic on components that have not been constructed yet.
	for _, size := range []tea.WindowSizeMsg{
		{Width: 100, Height: 40}, {Width: 20, Height: 5},
		{Width: 52, Height: 14}, {Width: 200, Height: 60},
	} {
		send(t, r, size)
		_ = r.View()
	}
}

func TestRoutingScreenFieldNavigationAndPickers(t *testing.T) {
	a, cfg, _ := newFlowApp(t)
	r := newFlowRoot(t, a, cfg)

	typeText(t, r, "the z13 trackpad again")
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenRouting {
		t.Fatalf("screen = %v", r.screen)
	}

	// Space on the note field opens the fuzzy picker.
	send(t, r, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if r.screen != ScreenNotePicker {
		t.Fatalf("space did not open the note picker (screen %v)", r.screen)
	}
	typeText(t, r, "tidemail")
	send(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenRouting {
		t.Fatalf("picker did not return to routing (screen %v)", r.screen)
	}
	if r.draft.Routing.NotePath != "Projects/Tidemail.md" {
		t.Errorf("destination = %s, want Projects/Tidemail.md", r.draft.Routing.NotePath)
	}
	if !r.draft.Corrected() {
		t.Error("changing the destination should register as a correction")
	}

	// Tab moves to the section field; space opens the heading picker.
	send(t, r, tea.KeyMsg{Type: tea.KeyTab})
	if r.routing.field() != fieldSection {
		t.Fatalf("focus = %v, want section", r.routing.field())
	}
	send(t, r, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if r.screen != ScreenHeadingPicker {
		t.Fatalf("space did not open the heading picker (screen %v)", r.screen)
	}
	typeText(t, r, "Roadmap")
	send(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.draft.Routing.Section != "Roadmap" {
		t.Errorf("section = %q, want Roadmap", r.draft.Routing.Section)
	}
	if r.draft.Routing.Mode != model.InsertUnderHeading {
		t.Errorf("mode = %s, want under_heading", r.draft.Routing.Mode)
	}

	// Tab to the format field; the arrow keys cycle the content type.
	send(t, r, tea.KeyMsg{Type: tea.KeyTab})
	before := r.draft.Routing.Type
	send(t, r, tea.KeyMsg{Type: tea.KeyRight})
	if r.draft.Routing.Type == before {
		t.Error("the content type did not change")
	}
}

func TestPreviewEditorRoundTrip(t *testing.T) {
	a, cfg, _ := newFlowApp(t)
	r := newFlowRoot(t, a, cfg)

	typeText(t, r, "buy a replacement ups battery")
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})

	send(t, r, tea.KeyMsg{Type: tea.KeyCtrlE})
	if r.screen != ScreenEditor {
		t.Fatalf("ctrl+e did not open the editor (screen %v)", r.screen)
	}
	r.editor.setValue("- [ ] Hand edited task")
	send(t, r, tea.KeyMsg{Type: tea.KeyCtrlS})
	if r.screen != ScreenRouting {
		t.Fatalf("ctrl+s did not return to routing (screen %v)", r.screen)
	}
	if r.draft.Markdown != "- [ ] Hand edited task" {
		t.Errorf("markdown = %q", r.draft.Markdown)
	}
	if !r.draft.ManualMarkdown {
		t.Error("the draft should be marked as manually edited")
	}
}

func TestEscapeFromRoutingReturnsToCapture(t *testing.T) {
	a, cfg, _ := newFlowApp(t)
	r := newFlowRoot(t, a, cfg)

	typeText(t, r, "a passing thought")
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenRouting {
		t.Fatalf("screen = %v", r.screen)
	}
	send(t, r, tea.KeyMsg{Type: tea.KeyEsc})
	if r.screen != ScreenCapture {
		t.Errorf("esc did not go back to capture (screen %v)", r.screen)
	}
	if r.capture.value() != "a passing thought" {
		t.Errorf("the thought was lost on the way back: %q", r.capture.value())
	}
}

func TestEmptyCaptureIsRejected(t *testing.T) {
	a, cfg, _ := newFlowApp(t)
	r := newFlowRoot(t, a, cfg)

	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenCapture {
		t.Errorf("an empty capture changed screen to %v", r.screen)
	}
	if !r.isError || !strings.Contains(r.status, "nothing to capture") {
		t.Errorf("no helpful message: %q", r.status)
	}
}

func TestUndoFromSavedScreen(t *testing.T) {
	a, cfg, root := newFlowApp(t)
	r := newFlowRoot(t, a, cfg)

	before, err := os.ReadFile(filepath.Join(root, "Projects", "Linux", "ROG Flow Z13.md"))
	if err != nil {
		t.Fatal(err)
	}

	typeText(t, r, "the z13 trackpad needs another look")
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	sendAndRun(t, r, tea.KeyMsg{Type: tea.KeyEnter})
	if r.screen != ScreenSaved {
		t.Fatalf("screen = %v (%s)", r.screen, r.status)
	}

	send(t, r, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if r.screen != ScreenCapture {
		t.Errorf("undo did not return to capture (screen %v)", r.screen)
	}
	after, err := os.ReadFile(filepath.Join(root, "Projects", "Linux", "ROG Flow Z13.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("undo did not restore the file:\n%s", after)
	}
}
