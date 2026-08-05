package router

import (
	"path"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/formatter"
	"github.com/alliebayless/murmur/internal/model"
)

// LearnedRoute is a destination previously chosen for similar words.
type LearnedRoute struct {
	NotePath string
	Section  string
	Type     model.ContentType
	Weight   float64
}

// Learner supplies the lightweight local learning signal. It is an interface so
// the router does not depend on the database package.
type Learner interface {
	LearnedRoutes(tokens []string) ([]LearnedRoute, error)
}

// Engine routes thoughts to destinations.
type Engine struct {
	cfg     config.Config
	rules   RuleSet
	notes   []model.Note
	byPath  map[string]model.Note
	vocab   map[string]string // lower-case term -> canonical casing
	learner Learner
}

// NewEngine builds a routing engine over a snapshot of the vault index.
func NewEngine(cfg config.Config, rules RuleSet, notes []model.Note, learner Learner) *Engine {
	e := &Engine{
		cfg:     cfg,
		rules:   rules,
		notes:   notes,
		byPath:  make(map[string]model.Note, len(notes)),
		vocab:   map[string]string{},
		learner: learner,
	}
	for _, n := range notes {
		e.byPath[n.RelPath] = n
		for _, t := range n.Tags {
			e.vocab[strings.ToLower(t)] = t
		}
	}
	return e
}

// Notes returns the indexed notes.
func (e *Engine) Notes() []model.Note { return e.notes }

// Note looks up an indexed note by vault-relative path.
func (e *Engine) Note(rel string) (model.Note, bool) {
	n, ok := e.byPath[rel]
	return n, ok
}

// Vocabulary returns the vault's canonical spellings, used both for tag
// suggestions and for restoring capitalisation in formatted output.
func (e *Engine) Vocabulary() []string {
	out := make([]string, 0, len(e.notes)*2)
	for _, n := range e.notes {
		out = append(out, n.FileName, n.Title)
		out = append(out, n.Aliases...)
		out = append(out, n.Tags...)
	}
	return out
}

// Caser builds a TermCaser from the vault vocabulary.
func (e *Engine) Caser() *formatter.TermCaser {
	return formatter.NewTermCaser(e.Vocabulary())
}

// Request is a routing request.
type Request struct {
	Text string
	Now  time.Time
	// ForceType overrides classification (used by --daily and --quick flags).
	ForceType model.ContentType
	// Daily forces the destination to the configured daily note.
	Daily bool
}

// Result is the routing decision plus the intermediate reasoning.
type Result struct {
	Cleaned        string
	Hints          model.Hints
	Routing        model.Routing
	Classification formatter.Classification
}

// Route runs every routing stage in precedence order.
func (e *Engine) Route(req Request) Result {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}

	cleaned, hints := ParseHints(req.Text, e.pathKnown)
	if cleaned == "" {
		cleaned = strings.TrimSpace(req.Text)
	}

	class := formatter.Classify(cleaned)
	ctype := class.Type
	switch {
	case req.ForceType != "":
		ctype = req.ForceType
		class = formatter.Classification{Type: ctype, Confidence: 1, Reason: "requested on the command line"}
	case hints.Type != "":
		ctype = hints.Type
		class = formatter.Classification{Type: ctype, Confidence: 1, Reason: "explicit #type hint"}
	}

	tokens := Tokenize(cleaned)
	learned := e.learnedWeights(tokens)

	routing := model.Routing{Type: ctype}

	// Stage 3 always runs so the UI can offer alternatives, even when an
	// earlier stage already decided the destination.
	candidates := Rank(RankInput{
		Text:    cleaned,
		Tokens:  tokens,
		Notes:   e.notes,
		Learned: learned,
		Now:     now,
		Project: hints.Project,
	}, 4)

	switch {
	case req.Daily:
		routing.NotePath = e.DailyPath(now)
		routing.Section = e.dailySection(ctype)
		routing.Source = model.SourceExplicit
		routing.Confidence = 1
		routing.Explanation = "daily note requested"

	case hints.Path != "":
		routing.NotePath = NormaliseNotePath(hints.Path)
		routing.Source = model.SourceExplicit
		routing.Confidence = 1
		routing.Explanation = "explicit destination in the thought"

	case hints.Project != "":
		if n, ok := e.resolveProject(hints.Project); ok {
			routing.NotePath = n.RelPath
			routing.Confidence = 0.95
			routing.Explanation = "matched @" + hints.Project
		} else {
			routing.NotePath = NormaliseNotePath(hints.Project)
			routing.Confidence = 0.8
			routing.Explanation = "new note for @" + hints.Project
		}
		routing.Source = model.SourceExplicit

	default:
		if rule, ok := e.rules.Match(cleaned, ctype); ok {
			routing.NotePath = e.expand(rule.Note, now)
			routing.Section = e.expand(rule.Section, now)
			routing.Source = model.SourceRule
			routing.Confidence = 0.9
			if kw := rule.MatchedKeyword(cleaned); kw != "" {
				routing.Explanation = "rule matched keyword \"" + kw + "\""
			} else {
				routing.Explanation = "rule matched content type " + string(ctype)
			}
			routing.Tags = append(routing.Tags, rule.Tags...)
		} else if len(candidates) > 0 {
			routing.NotePath = candidates[0].Note.RelPath
			routing.Source = model.SourceRanking
			routing.Confidence = Confidence(candidates)
			routing.Explanation = candidates[0].Reason()
		} else {
			routing.NotePath = e.fallbackPath(cleaned, ctype, now)
			routing.Source = model.SourceFallback
			routing.Confidence = 0.3
			routing.Explanation = "no vault match; using the configured fallback"
		}
	}

	if routing.NotePath == "" {
		routing.NotePath = e.fallbackPath(cleaned, ctype, now)
		routing.Source = model.SourceFallback
		routing.Confidence = 0.3
	}
	routing.NotePath = NormaliseNotePath(routing.NotePath)

	// Alternatives exclude whatever was selected.
	for _, c := range candidates {
		if c.Note.RelPath == routing.NotePath {
			continue
		}
		routing.Candidates = append(routing.Candidates, c)
		if len(routing.Candidates) == 3 {
			break
		}
	}

	dest, indexed := e.byPath[routing.NotePath]
	if routing.Section == "" {
		routing.Section = e.pickSection(dest, indexed, ctype, tokens, routing.NotePath, now)
	}
	routing.Mode = e.mode(dest, indexed, routing.Section)

	// A learned section for this destination is a good default when nothing
	// else suggested one.
	if routing.Section == "" {
		for _, lr := range e.learnedList(tokens) {
			if lr.NotePath == routing.NotePath && lr.Section != "" {
				routing.Section = lr.Section
				routing.Mode = e.mode(dest, indexed, routing.Section)
				break
			}
		}
	}

	routing.Tags = SuggestTags(cleaned, append(hints.Tags, routing.Tags...), dest, e.vocab, 5)

	return Result{Cleaned: cleaned, Hints: hints, Routing: routing, Classification: class}
}

func (e *Engine) mode(dest model.Note, indexed bool, section string) model.InsertMode {
	if section == "" {
		return model.InsertAppendEnd
	}
	if indexed {
		for _, h := range dest.Headings {
			if strings.EqualFold(strings.TrimSpace(h.Text), strings.TrimSpace(section)) {
				return model.InsertUnderHeading
			}
		}
	}
	return model.InsertCreateHeading
}

// pickSection chooses a heading inside the destination note.
func (e *Engine) pickSection(dest model.Note, indexed bool, ctype model.ContentType, tokens []string, notePath string, now time.Time) string {
	if notePath == e.DailyPath(now) {
		return e.dailySection(ctype)
	}
	if !indexed {
		return ""
	}
	if h := SuggestHeading(dest, tokens); h != "" {
		return h
	}
	// Fall back to a conventional inbox-style section when the note has one.
	// H1s are skipped: they are note titles, not sections.
	for _, h := range dest.Headings {
		if h.Level == 1 {
			continue
		}
		switch strings.ToLower(h.Text) {
		case "inbox", "notes", "log", "unsorted":
			return h.Text
		}
	}
	return ""
}

func (e *Engine) dailySection(ctype model.ContentType) string {
	switch ctype {
	case model.TypeJournal:
		return e.cfg.Daily.Journal
	case model.TypeTask, model.TypeQuestion:
		return e.cfg.Daily.Tasks
	default:
		return e.cfg.Daily.Notes
	}
}

// DailyPath resolves the configured daily note path for a moment in time.
func (e *Engine) DailyPath(now time.Time) string {
	return NormaliseNotePath(e.expand(e.cfg.DailyNotePath, now))
}

func (e *Engine) expand(s string, now time.Time) string {
	return ExpandTemplate(s, now, e.cfg.DateFormat, e.cfg.TimeFormat)
}

func (e *Engine) fallbackPath(text string, ctype model.ContentType, now time.Time) string {
	if rule, ok := e.rules.Fallback(text, ctype); ok {
		return e.expand(rule.FallbackNote, now)
	}
	switch ctype {
	case model.TypeTask, model.TypeQuestion:
		return e.cfg.DefaultTaskNote
	case model.TypeJournal:
		return e.DailyPath(now)
	default:
		return e.cfg.DefaultInbox
	}
}

func (e *Engine) pathKnown(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	norm := NormaliseNotePath(candidate)
	if _, ok := e.byPath[norm]; ok {
		return true
	}
	// Also accept a bare note name that exists somewhere in the vault.
	for _, n := range e.notes {
		if strings.EqualFold(n.FileName, candidate) || strings.EqualFold(n.Title, candidate) {
			return true
		}
	}
	return false
}

func (e *Engine) resolveProject(name string) (model.Note, bool) {
	lower := strings.ToLower(name)
	var best model.Note
	found := false
	for _, n := range e.notes {
		if strings.EqualFold(n.FileName, name) || strings.EqualFold(n.Title, name) {
			return n, true
		}
		for _, a := range n.Aliases {
			if strings.EqualFold(a, name) {
				return n, true
			}
		}
		if !found && strings.Contains(strings.ToLower(n.FileName), lower) {
			best, found = n, true
		}
	}
	return best, found
}

func (e *Engine) learnedList(tokens []string) []LearnedRoute {
	if e.learner == nil || len(tokens) == 0 {
		return nil
	}
	routes, err := e.learner.LearnedRoutes(tokens)
	if err != nil {
		return nil // learning is best-effort; never block a capture
	}
	return routes
}

func (e *Engine) learnedWeights(tokens []string) map[string]LearnedWeight {
	out := map[string]LearnedWeight{}
	for _, lr := range e.learnedList(tokens) {
		out[lr.NotePath] = LearnedWeight{Weight: lr.Weight, Section: lr.Section, Type: lr.Type}
	}
	return out
}

// NormaliseNotePath cleans a user-supplied destination: slashes are normalised,
// a .md extension is added when missing, and leading separators are removed.
func NormaliseNotePath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ""
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if !strings.HasSuffix(strings.ToLower(p), ".md") {
		p += ".md"
	}
	return path.Clean(p)
}
