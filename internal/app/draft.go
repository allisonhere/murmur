package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/formatter"
	"github.com/alliebayless/murmur/internal/markdown"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/provider"
	"github.com/alliebayless/murmur/internal/router"
	"github.com/alliebayless/murmur/internal/storage"
)

// ErrEmptyThought is returned when there is nothing to capture.
var ErrEmptyThought = errors.New("nothing to capture: the thought is empty")

// Draft is a prepared capture plus the state the confirmation screen needs.
type Draft struct {
	model.Draft

	// Suggested is the routing Murmur proposed, kept so corrections can be
	// detected and learned from.
	Suggested model.Routing

	DestExists   bool
	DestHeadings []model.Heading
	// ManualMarkdown is set once the user edits the preview by hand, after
	// which Murmur stops regenerating it.
	ManualMarkdown bool
	// AIStatus describes what the AI provider did, if anything.
	AIStatus string

	app *App
	now time.Time
}

// PrepareOptions control a single capture.
type PrepareOptions struct {
	Daily     bool
	ForceType model.ContentType
	// UseAI enables the optional classification pass.
	UseAI bool
	Now   time.Time
}

// Prepare runs routing and formatting for a thought and returns a Draft.
func (a *App) Prepare(ctx context.Context, text string, opts PrepareOptions) (*Draft, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyThought
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	res := a.Engine.Route(router.Request{
		Text:      text,
		Now:       now,
		ForceType: opts.ForceType,
		Daily:     opts.Daily,
	})

	d := &Draft{
		Draft: model.Draft{
			Raw:       text,
			Cleaned:   res.Cleaned,
			Hints:     res.Hints,
			Routing:   res.Routing,
			CreatedAt: now,
		},
		Suggested: res.Routing,
		app:       a,
		now:       now,
	}
	a.Debugf("routing: %s -> %s (%s, %.0f%%) %s",
		res.Classification.Type, res.Routing.NotePath, res.Routing.Source,
		res.Routing.Confidence*100, res.Routing.Explanation)

	if opts.UseAI {
		d.applyAI(ctx)
	}

	if err := d.RefreshDestination(); err != nil {
		return nil, err
	}
	d.Render()
	return d, nil
}

// applyAI asks the configured provider for a better answer and adopts it only
// when it validates. Any failure leaves the deterministic result untouched.
func (d *Draft) applyAI(ctx context.Context) {
	if d.app.classifier == nil {
		return
	}
	if _, isNone := d.app.classifier.(provider.None); isNone {
		return
	}

	req := provider.ClassificationRequest{
		Thought: d.Cleaned,
		Today:   d.now.Format("2006-01-02"),
		Suggested: provider.ClassificationResult{
			NotePath:   d.Routing.NotePath,
			Section:    d.Routing.Section,
			Type:       d.Routing.Type,
			Tags:       d.Routing.Tags,
			Confidence: d.Routing.Confidence,
		},
	}
	allowed := []string{d.Routing.NotePath}
	add := func(path string) {
		n, ok := d.app.Engine.Note(path)
		if !ok {
			return
		}
		req.Candidates = append(req.Candidates, provider.CandidateInfo{
			Path:     n.RelPath,
			Title:    n.Title,
			Tags:     n.Tags,
			Headings: n.HeadingTexts(),
		})
	}
	add(d.Routing.NotePath)
	for _, c := range d.Routing.Candidates {
		add(c.Note.RelPath)
		allowed = append(allowed, c.Note.RelPath)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.app.Cfg.AI.TimeoutSeconds)*time.Second)
	defer cancel()

	out, err := d.app.classifier.Classify(ctx, req)
	if err != nil {
		if errors.Is(err, provider.ErrNoProvider) {
			return
		}
		d.AIStatus = "AI unavailable, using local routing: " + err.Error()
		d.app.Debugf("AI classify failed: %v", err)
		return
	}
	if err := provider.Validate(&out, allowed); err != nil {
		d.AIStatus = "AI reply rejected, using local routing: " + err.Error()
		d.app.Debugf("AI validation failed: %v", err)
		return
	}

	d.Routing.NotePath = router.NormaliseNotePath(out.NotePath)
	d.Routing.Section = out.Section
	d.Routing.Type = out.Type
	if len(out.Tags) > 0 {
		d.Routing.Tags = out.Tags
	}
	if out.Confidence > 0 {
		d.Routing.Confidence = out.Confidence
	}
	d.Routing.Source = model.SourceAI
	if out.Reason != "" {
		d.Routing.Explanation = out.Reason
	}
	d.Markdown = out.Markdown
	d.ManualMarkdown = true // the provider produced it; do not overwrite
	d.AIStatus = "routed by " + d.app.classifier.Name()
}

// RefreshDestination re-reads the destination note so headings, existence and
// the conflict hash all reflect what is on disk right now.
func (d *Draft) RefreshDestination() error {
	if d.app == nil {
		return errors.New("draft is not attached to an application")
	}
	st, err := d.app.Vault.Read(d.Routing.NotePath)
	if err != nil {
		return err
	}
	d.DestExists = st.Exists
	d.FileHashBefore = st.Hash
	d.DestHeadings = markdown.ExtractHeadings(st.Content)

	// Re-derive the insertion mode from the file itself: the index may be
	// stale, and the file is the truth.
	switch {
	case strings.TrimSpace(d.Routing.Section) == "":
		d.Routing.Mode = model.InsertAppendEnd
	default:
		if _, ok := markdown.FindHeading(st.Content, d.Routing.Section); ok {
			d.Routing.Mode = model.InsertUnderHeading
		} else {
			d.Routing.Mode = model.InsertCreateHeading
		}
	}
	return nil
}

// Render regenerates the Markdown block from the thought and content type,
// unless the user has taken manual control of the preview.
func (d *Draft) Render() {
	if d.ManualMarkdown || d.app == nil {
		return
	}
	opts := formatter.Options{
		Now:              d.now,
		DateFormat:       d.app.Cfg.DateFormat,
		IncludeDate:      d.app.Cfg.Formatting.IncludeCaptureDate,
		UseCallouts:      d.app.Cfg.Formatting.UseCalloutsForIdea,
		TaskDateProperty: d.app.Cfg.Formatting.TaskDateProperty,
		Tags:             d.Routing.Tags,
		Caser:            d.app.Engine.Caser(),
	}
	d.Markdown = formatter.Format(d.Cleaned, d.Routing.Type, opts)
}

// SetDestination points the draft at another note and refreshes its state.
func (d *Draft) SetDestination(rel string) error {
	rel = router.NormaliseNotePath(rel)
	if rel == "" {
		return errors.New("destination path is empty")
	}
	if _, err := d.app.Vault.Resolve(rel); err != nil {
		return err
	}
	d.Routing.NotePath = rel
	// A heading from the previous note may not exist in the new one.
	if err := d.RefreshDestination(); err != nil {
		return err
	}
	if d.Routing.Mode == model.InsertCreateHeading && d.Routing.Section != "" {
		if n, ok := d.app.Engine.Note(rel); ok {
			if h := router.SuggestHeading(n, router.Tokenize(d.Cleaned)); h != "" {
				d.Routing.Section = h
				return d.RefreshDestination()
			}
		}
	}
	return nil
}

// SetSection changes the destination heading. An empty section means "append to
// the end of the note".
func (d *Draft) SetSection(section string) error {
	d.Routing.Section = strings.TrimSpace(section)
	return d.RefreshDestination()
}

// SetType changes the content type and re-renders the preview.
func (d *Draft) SetType(t model.ContentType) {
	if !t.Valid() {
		return
	}
	d.Routing.Type = t
	d.ManualMarkdown = false
	d.Render()
}

// SetTags replaces the suggested tags.
func (d *Draft) SetTags(tags []string) {
	d.Routing.Tags = tags
	d.Render()
}

// SetMarkdown takes manual control of the preview content.
func (d *Draft) SetMarkdown(md string) {
	d.Markdown = strings.TrimRight(md, "\n")
	d.ManualMarkdown = true
}

// Preview renders the destination file as it will look after the write, so the
// confirmation screen can show real context rather than an isolated block.
func (d *Draft) Preview() (string, error) {
	st, err := d.app.Vault.Read(d.Routing.NotePath)
	if err != nil {
		return "", err
	}
	content := st.Content
	if !st.Exists {
		content = d.newNoteContent()
	}
	return markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   d.Markdown,
		Section: d.Routing.Section,
		Mode:    d.Routing.Mode,
	})
}

// Corrected reports whether the user changed Murmur's suggestion.
func (d *Draft) Corrected() bool {
	return d.Routing.NotePath != d.Suggested.NotePath ||
		!strings.EqualFold(d.Routing.Section, d.Suggested.Section) ||
		d.Routing.Type != d.Suggested.Type
}

// Destination renders the destination as a single human-readable line.
func (d *Draft) Destination() string {
	if d.Routing.Section == "" {
		return d.Routing.NotePath
	}
	return d.Routing.NotePath + " under " + headingPrefix(d.Routing) + d.Routing.Section
}

func headingPrefix(r model.Routing) string {
	if r.Mode == model.InsertAppendEnd {
		return ""
	}
	return "## "
}

// ConflictError reports that the destination changed between preview and save.
type ConflictError struct {
	Path string
}

func (e *ConflictError) Error() string {
	return e.Path + " changed on disk since the preview was built"
}

// stateForWrite re-reads the destination and verifies it still matches the
// preview, which is Murmur's guard against clobbering a concurrent edit.
func (d *Draft) stateForWrite() (storage.FileState, error) {
	st, err := d.app.Vault.Read(d.Routing.NotePath)
	if err != nil {
		return st, err
	}
	if st.Hash != d.FileHashBefore {
		return st, &ConflictError{Path: d.Routing.NotePath}
	}
	return st, nil
}
