// Package router decides where a captured thought belongs. It runs in stages:
// explicit hints, then deterministic rules, then weighted vault ranking, with
// an optional AI pass layered on top by the caller.
package router

import (
	"regexp"
	"strings"

	"github.com/alliebayless/murmur/internal/model"
)

// PathKnown reports whether a candidate string names a real note. The router
// uses it to work out where a ">path with spaces" hint ends.
type PathKnown func(candidate string) bool

var (
	atHintRe  = regexp.MustCompile(`(^|\s)@([\p{L}\p{N}][\p{L}\p{N}_\-/]*)`)
	tagHintRe = regexp.MustCompile(`(^|\s)#([\p{L}\p{N}][\p{L}\p{N}_\-/]*)`)
)

// ParseHints extracts explicit routing instructions and returns the thought
// with those instructions removed.
//
// Recognised syntax:
//
//	>Projects/Linux/ROG Flow Z13   explicit destination (may contain spaces)
//	>"Some Note.md"                explicit destination, quoted
//	@tidemail                      project or note hint
//	#journal / #task / #idea       content type hint
//	#anything-else                 suggested tag
func ParseHints(text string, known PathKnown) (string, model.Hints) {
	var h model.Hints
	work := strings.TrimSpace(text)

	work = extractPathHint(work, known, &h)

	if m := atHintRe.FindStringSubmatch(work); m != nil {
		h.Project = m[2]
		work = strings.Replace(work, m[0], m[1], 1)
	}

	for {
		m := tagHintRe.FindStringSubmatch(work)
		if m == nil {
			break
		}
		token := m[2]
		if t, ok := model.ParseContentType(token); ok && h.Type == "" {
			h.Type = t
		} else {
			h.Tags = appendTag(h.Tags, token)
		}
		work = strings.Replace(work, m[0], m[1], 1)
	}

	work = strings.TrimSpace(strings.Join(strings.Fields(work), " "))
	return work, h
}

func appendTag(tags []string, t string) []string {
	for _, existing := range tags {
		if strings.EqualFold(existing, t) {
			return tags
		}
	}
	return append(tags, t)
}

// extractPathHint handles a leading ">destination". Because destinations may
// contain spaces, the longest prefix that names a real note wins; failing that
// the first whitespace-delimited token is used.
func extractPathHint(text string, known PathKnown, h *model.Hints) string {
	if !strings.HasPrefix(text, ">") {
		return text
	}
	rest := strings.TrimSpace(text[1:])
	if rest == "" {
		return text
	}
	// A Markdown callout or quote ("> some text") is not a path hint.
	if strings.HasPrefix(text, "> ") && !strings.Contains(firstLine(rest), "/") && known == nil {
		return text
	}

	if strings.HasPrefix(rest, `"`) {
		if end := strings.Index(rest[1:], `"`); end >= 0 {
			h.Path = rest[1 : 1+end]
			return strings.TrimSpace(rest[end+2:])
		}
	}

	line := firstLine(rest)
	words := strings.Fields(line)
	if len(words) == 0 {
		return text
	}
	if known != nil {
		for n := len(words); n >= 1; n-- {
			candidate := strings.Join(words[:n], " ")
			if known(candidate) {
				h.Path = candidate
				return strings.TrimSpace(strings.TrimPrefix(rest, candidate))
			}
		}
	}
	// Nothing matched the index. A ".md" suffix marks where the path ends,
	// which is what makes ">Projects/New/Deep Note.md text" work for a note
	// that does not exist yet.
	for i, w := range words {
		if strings.HasSuffix(strings.ToLower(w), ".md") {
			candidate := strings.Join(words[:i+1], " ")
			h.Path = candidate
			return strings.TrimSpace(strings.TrimPrefix(rest, candidate))
		}
	}
	// Otherwise fall back to the first token, the right answer for paths
	// without spaces.
	h.Path = words[0]
	return strings.TrimSpace(strings.TrimPrefix(rest, words[0]))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
