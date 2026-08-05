package formatter

import (
	"regexp"
	"strings"

	"github.com/alliebayless/murmur/internal/model"
)

var (
	questionStart = regexp.MustCompile(`^(why|how|what|when|where|who|which|is|are|does|do|did|should|could|can|would|will)\b`)
	journalStart  = regexp.MustCompile(`^(today|yesterday|this morning|this afternoon|this evening|tonight|finally|just)\b`)
	// Past-tense narrative verbs that signal a journal entry rather than a task.
	journalVerb = regexp.MustCompile(`\b(fixed|finished|shipped|wrote|read|met|went|spent|learned|discovered|solved|built|released|deployed|migrated|cleaned|refactored)\b`)
	ideaCue     = regexp.MustCompile(`^(maybe|what if|idea|it might|it could|we could|i could|perhaps|possibly)\b`)
	ideaInner   = regexp.MustCompile(`\b(should support|could support|would be (nice|cool|good|great)|might be worth|worth considering)\b`)
	taskCue     = regexp.MustCompile(`^(remember|todo|need|buy|fix|update|call|email|add|write|send|book|renew|order|schedule|review|check|install|upgrade|replace|clean|file|pay|ask|investigate|patch|test|deploy|merge|refactor|document|reply)\b`)
	referenceRe = regexp.MustCompile(`^(reference|ref|according to|quote|from the docs|per the)\b`)
)

// Classification is a content-type guess with a confidence in 0..1.
type Classification struct {
	Type       model.ContentType
	Confidence float64
	Reason     string
}

// Classify guesses the content type of a thought using ordered heuristics. The
// order matters: a sentence like "today I should maybe fix X" matches several
// patterns and the earliest rule wins, which keeps behaviour predictable.
func Classify(text string) Classification {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if trimmed == "" {
		return Classification{Type: model.TypeNote, Confidence: 0}
	}

	// A URL dominates: the user is saving a link.
	if URLRe.MatchString(trimmed) {
		return Classification{Type: model.TypeBookmark, Confidence: 0.95, Reason: "contains a URL"}
	}
	// An explicit question mark is unambiguous.
	if strings.HasSuffix(trimmed, "?") {
		return Classification{Type: model.TypeQuestion, Confidence: 0.9, Reason: "ends with a question mark"}
	}
	// "Today I fixed ..." is a journal entry, not a task, even though "fix" is
	// also a task cue.
	if journalStart.MatchString(lower) && journalVerb.MatchString(lower) {
		return Classification{Type: model.TypeJournal, Confidence: 0.88, Reason: "past-tense entry about today"}
	}
	if journalStart.MatchString(lower) {
		return Classification{Type: model.TypeJournal, Confidence: 0.7, Reason: "time-anchored entry"}
	}
	// Speculation is checked before question phrasing: "what if we cached the
	// index" starts with a wh-word but is an idea, not a question.
	if ideaCue.MatchString(lower) || ideaInner.MatchString(lower) {
		return Classification{Type: model.TypeIdea, Confidence: 0.8, Reason: "speculative phrasing"}
	}
	if questionStart.MatchString(lower) {
		return Classification{Type: model.TypeQuestion, Confidence: 0.75, Reason: "phrased as a question"}
	}
	if taskCue.MatchString(lower) {
		return Classification{Type: model.TypeTask, Confidence: 0.82, Reason: "starts with an action verb"}
	}
	if journalVerb.MatchString(lower) {
		return Classification{Type: model.TypeJournal, Confidence: 0.6, Reason: "past-tense narrative"}
	}
	if referenceRe.MatchString(lower) {
		return Classification{Type: model.TypeReference, Confidence: 0.7, Reason: "reference phrasing"}
	}
	// Multi-paragraph text is more likely a written-out note than a one-liner.
	if strings.Count(trimmed, "\n\n") > 0 || len(trimmed) > 240 {
		return Classification{Type: model.TypeProject, Confidence: 0.55, Reason: "long-form text"}
	}
	return Classification{Type: model.TypeNote, Confidence: 0.4, Reason: "no strong signal"}
}
