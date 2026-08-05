// Package formatter turns a rough thought into clean Markdown. It is
// deliberately deterministic: the same input always produces the same output,
// with no network access and no AI required.
package formatter

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// URLRe matches bare http(s) URLs inside a thought.
var URLRe = regexp.MustCompile(`https?://[^\s<>()\[\]"']+`)

// fillerPrefixes are conversational openers that add nothing once the thought
// has been classified. They are stripped in order, longest first.
var fillerPrefixes = []string{
	"note to self that",
	"note to self:",
	"note to self",
	"remember to",
	"remember that",
	"remember",
	"don't forget to",
	"dont forget to",
	"don't forget",
	"i need to",
	"i should probably",
	"i should",
	"we should probably",
	"we should",
	"need to",
	"todo:",
	"todo",
	"task:",
	"idea:",
	"maybe we could",
	"maybe i could",
	"maybe i should",
	"maybe",
	"what if we",
	"what if",
	"today i finally",
	"today i",
	"today,",
	"today",
	"just",
	"quick note:",
	"quick note",
	"thinking about",
	"i want to",
	"want to",
	"question:",
	"bookmark:",
	"link:",
}

// StripFiller removes a leading conversational opener, if present.
func StripFiller(s string) string {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)
	best := ""
	for _, p := range fillerPrefixes {
		if strings.HasPrefix(lower, p) {
			rest := trimmed[len(p):]
			// Only strip when a word boundary follows, so "todos" survives.
			if rest == "" || isBoundary(rune(rest[0])) {
				if len(p) > len(best) {
					best = p
				}
			}
		}
	}
	if best == "" {
		return trimmed
	}
	out := strings.TrimSpace(trimmed[len(best):])
	out = strings.TrimLeft(out, ":,-— ")
	if out == "" {
		return trimmed
	}
	return strings.TrimSpace(out)
}

func isBoundary(r rune) bool {
	return unicode.IsSpace(r) || r == ':' || r == ',' || r == '-'
}

// Capitalize upper-cases the first letter of a sentence without touching the
// rest, so acronyms typed correctly stay correct.
func Capitalize(s string) string {
	for i, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return s[:i] + strings.ToUpper(string(r)) + s[i+len(string(r)):]
		}
	}
	return s
}

// EnsureTerminator appends a full stop when the text has no closing
// punctuation. Bullets read better as complete sentences.
func EnsureTerminator(s string) string {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return s
	}
	last := rune(s[len(s)-1])
	switch last {
	case '.', '!', '?', ':', ';', ')', ']', '`', '"', '\'':
		return s
	}
	// Do not punctuate a line that ends in a URL or a tag.
	if URLRe.MatchString(s) && strings.HasSuffix(s, URLRe.FindString(s)) {
		return s
	}
	if strings.HasSuffix(s, "#") {
		return s
	}
	return s + "."
}

// CollapseWhitespace joins a multi-line thought into a single paragraph, which
// is what list items and callout lines need.
func CollapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TermCaser restores the vault's own capitalisation for words the user typed in
// lower case. The vocabulary comes from note titles, aliases, tags and file
// names, so "forgejo" becomes "Forgejo" when the vault knows that word.
type TermCaser struct {
	terms map[string]string // lower-case form -> canonical form
}

// NewTermCaser builds a caser from vault vocabulary. Entries that are already
// all lower case are ignored: they teach nothing about casing.
func NewTermCaser(vocab []string) *TermCaser {
	tc := &TermCaser{terms: map[string]string{}}
	for _, v := range vocab {
		for _, word := range splitVocab(v) {
			lower := strings.ToLower(word)
			if lower == word || len([]rune(word)) < 2 {
				continue
			}
			if existing, ok := tc.terms[lower]; ok {
				// Prefer the more distinctive casing (e.g. NVIDIA over Nvidia).
				if countUpper(existing) >= countUpper(word) {
					continue
				}
			}
			tc.terms[lower] = word
		}
	}
	return tc
}

func splitVocab(v string) []string {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func countUpper(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsUpper(r) {
			n++
		}
	}
	return n
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// Apply rewrites known vocabulary words to their vault casing. Words the user
// already capitalised are left alone.
func (tc *TermCaser) Apply(s string) string {
	if tc == nil || len(tc.terms) == 0 {
		return s
	}
	return wordRe.ReplaceAllStringFunc(s, func(w string) string {
		if w != strings.ToLower(w) {
			return w // the user typed their own casing; respect it
		}
		if canonical, ok := tc.terms[w]; ok {
			return canonical
		}
		return w
	})
}

// Size reports how many terms the caser knows; used in tests and diagnostics.
func (tc *TermCaser) Size() int {
	if tc == nil {
		return 0
	}
	return len(tc.terms)
}
