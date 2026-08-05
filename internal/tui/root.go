package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// Screen identifies which view is on top.
type Screen int

// The screens Murmur can show. ScreenCapture is deliberately the zero value so
// that an Options struct with no StartScreen set opens the capture box.
const (
	ScreenCapture Screen = iota
	ScreenSetup
	ScreenRouting
	ScreenNotePicker
	ScreenHeadingPicker
	ScreenEditor
	ScreenHistory
	ScreenSaved
)

// MinWidth and MinHeight are the smallest terminal Murmur will draw into.
const (
	MinWidth  = 52
	MinHeight = 14
)

// Options configure a TUI session.
type Options struct {
	// InitialText pre-fills the capture box (from an argument or stdin).
	InitialText string
	// SkipCapture jumps straight to routing, used when text came from the CLI.
	SkipCapture bool
	Daily       bool
	ForceType   model.ContentType
	UseAI       bool
	StartScreen Screen
	Verbose     bool
}

// Builder creates the application once a vault has been configured. It is used
// by the first-run setup screen.
type Builder func(config.Config) (*app.App, error)

// Root is the top-level Bubble Tea model.
type Root struct {
	app     *app.App
	cfg     config.Config
	builder Builder
	st      Styles
	opts    Options

	screen Screen
	back   Screen
	width  int
	height int

	capture  captureModel
	routing  routingModel
	notePick pickerModel
	headPick pickerModel
	editor   editorModel
	history  historyModel
	setup    setupModel

	draft  *app.Draft
	result *app.SaveResult

	spin      spinner.Model
	busy      bool
	busyLabel string

	status  string
	isError bool
	quit    bool
}

// New builds the root model. a may be nil, in which case the setup screen runs
// first and builder is used to open the application afterwards.
func New(a *app.App, cfg config.Config, builder Builder, opts Options) *Root {
	st := NewStyles(DefaultTheme)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = st.Accent

	r := &Root{
		app:     a,
		cfg:     cfg,
		builder: builder,
		st:      st,
		opts:    opts,
		spin:    sp,
		width:   80,
		height:  24,
	}
	r.capture = newCaptureModel(st, opts.InitialText)
	r.setup = newSetupModel(st, cfg)

	r.screen = opts.StartScreen
	if a == nil {
		// No application means no vault has been configured yet.
		r.screen = ScreenSetup
	}
	return r
}

// Init implements tea.Model.
func (r *Root) Init() tea.Cmd {
	cmds := []tea.Cmd{r.spin.Tick}
	switch r.screen {
	case ScreenSetup:
		cmds = append(cmds, r.setup.focus())
	case ScreenHistory:
		r.history = newHistoryModel(r.st)
		cmds = append(cmds, r.loadHistory())
	default:
		if r.opts.SkipCapture && strings.TrimSpace(r.opts.InitialText) != "" {
			return tea.Batch(append(cmds, r.prepare(r.opts.InitialText))...)
		}
		cmds = append(cmds, r.capture.focus())
	}
	return tea.Batch(cmds...)
}

// ------------------------------------------------------------------ messages

type draftMsg struct {
	draft *app.Draft
	err   error
}

type savedMsg struct {
	result app.SaveResult
	err    error
}

type appReadyMsg struct {
	app *app.App
	cfg config.Config
	err error
}

type historyMsg struct {
	records []model.CaptureRecord
	err     error
}

type statusMsg struct {
	text    string
	isError bool
}

func status(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text} }
}

func failure(err error) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: err.Error(), isError: true} }
}

// prepare runs routing in the background so the UI never blocks on AI calls.
func (r *Root) prepare(text string) tea.Cmd {
	a := r.app
	opts := app.PrepareOptions{
		Daily:     r.opts.Daily,
		ForceType: r.opts.ForceType,
		UseAI:     r.opts.UseAI,
	}
	r.busy = true
	r.busyLabel = "Finding a home for that…"
	return func() tea.Msg {
		d, err := a.Prepare(context.Background(), text, opts)
		return draftMsg{draft: d, err: err}
	}
}

func (r *Root) save() tea.Cmd {
	d := r.draft
	r.busy = true
	r.busyLabel = "Saving…"
	return func() tea.Msg {
		res, err := d.Save()
		return savedMsg{result: res, err: err}
	}
}

func (r *Root) loadHistory() tea.Cmd {
	a := r.app
	return func() tea.Msg {
		recs, err := a.History(100)
		return historyMsg{records: recs, err: err}
	}
}

func (r *Root) openApp(cfg config.Config) tea.Cmd {
	builder := r.builder
	r.busy = true
	r.busyLabel = "Indexing your vault…"
	return func() tea.Msg {
		if builder == nil {
			return appReadyMsg{err: errNoBuilder}
		}
		a, err := builder(cfg)
		return appReadyMsg{app: a, cfg: cfg, err: err}
	}
}

// ------------------------------------------------------------------- update

// Update implements tea.Model.
func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		r.capture.resize(r.contentWidth(), r.captureHeight())
		r.editor.resize(r.contentWidth(), r.editorHeight())
		return r, nil

	case tea.KeyMsg:
		if k := msg.String(); k == "ctrl+c" {
			r.quit = true
			return r, tea.Quit
		}
		return r.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		r.spin, cmd = r.spin.Update(msg)
		return r, cmd

	case statusMsg:
		r.status, r.isError = msg.text, msg.isError
		return r, nil

	case draftMsg:
		r.busy = false
		if msg.err != nil {
			r.status, r.isError = msg.err.Error(), true
			r.screen = ScreenCapture
			return r, r.capture.focus()
		}
		r.draft = msg.draft
		r.routing = newRoutingModel(r.st, r.draft)
		r.screen = ScreenRouting
		r.status, r.isError = r.draft.AIStatus, false
		return r, nil

	case savedMsg:
		r.busy = false
		if msg.err != nil {
			r.status, r.isError = friendlySaveError(msg.err), true
			// A conflict means the preview is stale; rebuild it.
			if isConflict(msg.err) && r.draft != nil {
				if err := r.draft.RefreshDestination(); err == nil {
					r.routing = newRoutingModel(r.st, r.draft)
				}
			}
			return r, nil
		}
		res := msg.result
		r.result = &res
		r.screen = ScreenSaved
		r.status, r.isError = "", false
		return r, nil

	case historyMsg:
		r.busy = false
		if msg.err != nil {
			r.status, r.isError = msg.err.Error(), true
			return r, nil
		}
		r.history.setRecords(msg.records)
		return r, nil

	case appReadyMsg:
		r.busy = false
		if msg.err != nil {
			r.status, r.isError = msg.err.Error(), true
			return r, r.setup.focus()
		}
		r.app = msg.app
		r.cfg = msg.cfg
		r.screen = ScreenCapture
		r.status, r.isError = "Vault ready. What's on your mind?", false
		return r, r.capture.focus()
	}

	return r, r.forward(msg)
}

// forward passes non-key messages to the active screen's components.
func (r *Root) forward(msg tea.Msg) tea.Cmd {
	switch r.screen {
	case ScreenCapture:
		return r.capture.update(msg)
	case ScreenEditor:
		return r.editor.update(msg)
	case ScreenNotePicker:
		return r.notePick.update(msg)
	case ScreenHeadingPicker:
		return r.headPick.update(msg)
	case ScreenSetup:
		return r.setup.update(msg)
	}
	return nil
}

func (r *Root) contentWidth() int {
	w := r.width - 6
	if w < 20 {
		w = 20
	}
	if w > 110 {
		w = 110
	}
	return w
}

func (r *Root) cardWidth() int { return r.contentWidth() + 4 }

func (r *Root) captureHeight() int {
	h := r.height - 12
	if h < 3 {
		h = 3
	}
	if h > 12 {
		h = 12
	}
	return h
}

func (r *Root) editorHeight() int {
	h := r.height - 10
	if h < 4 {
		h = 4
	}
	if h > 20 {
		h = 20
	}
	return h
}

// View implements tea.Model.
func (r *Root) View() string {
	if r.quit {
		return ""
	}
	if r.width < MinWidth || r.height < MinHeight {
		return r.st.Warn.Render("\n Murmur needs a slightly bigger window.\n") +
			r.st.Muted.Render("  Resize to at least 52×14 and it will redraw.\n")
	}

	var body string
	switch r.screen {
	case ScreenSetup:
		body = r.setup.view(r.cardWidth())
	case ScreenCapture:
		body = r.capture.view(r.cardWidth(), r.busy, r.spin.View(), r.busyLabel)
	case ScreenRouting:
		body = r.routing.view(r.cardWidth(), r.height)
	case ScreenNotePicker:
		body = r.notePick.view(r.cardWidth(), r.listHeight())
	case ScreenHeadingPicker:
		body = r.headPick.view(r.cardWidth(), r.listHeight())
	case ScreenEditor:
		body = r.editor.view(r.cardWidth())
	case ScreenHistory:
		body = r.history.view(r.cardWidth(), r.listHeight())
	case ScreenSaved:
		body = r.savedView()
	}

	return "\n" + body + "\n" + r.statusLine() + "\n" + r.footer() + "\n"
}

func (r *Root) listHeight() int {
	h := r.height - 11
	if h < 3 {
		h = 3
	}
	if h > 18 {
		h = 18
	}
	return h
}

func (r *Root) statusLine() string {
	if r.busy {
		return " " + r.spin.View() + r.st.Muted.Render(r.busyLabel)
	}
	if r.status == "" {
		return ""
	}
	text := truncPlain(strings.ReplaceAll(r.status, "\n", " "), r.cardWidth()-2)
	if r.isError {
		return " " + r.st.Error.Render("✗ "+text)
	}
	return " " + r.st.Muted.Render(text)
}

var errNoBuilder = errNoBuilderType{}

type errNoBuilderType struct{}

func (errNoBuilderType) Error() string { return "internal error: no application builder configured" }
