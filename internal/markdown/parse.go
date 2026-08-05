package markdown

import (
	"regexp"
	"strings"

	"github.com/alliebayless/murmur/internal/model"
)

// LineEnding is the newline sequence used by a file.
type LineEnding string

// Supported line endings.
const (
	LF   LineEnding = "\n"
	CRLF LineEnding = "\r\n"
)

// DetectLineEnding reports the dominant line ending of a document. Files that
// contain no newline at all default to LF.
func DetectLineEnding(content string) LineEnding {
	crlf := strings.Count(content, "\r\n")
	if crlf == 0 {
		return LF
	}
	lf := strings.Count(content, "\n") - crlf
	if crlf >= lf {
		return CRLF
	}
	return LF
}

func normaliseNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// Restore converts LF-normalised text back to the given line ending.
func (le LineEnding) Restore(s string) string {
	if le == CRLF {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	wikilinkRe = regexp.MustCompile(`\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]`)
	inlineTag  = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}][\p{L}\p{N}_\-/]*)`)
	fenceRe    = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
)

// ExtractHeadings returns every ATX heading in the document, skipping any that
// appear inside fenced code blocks. Line numbers are relative to the whole
// document, including frontmatter, so callers can address lines directly.
func ExtractHeadings(content string) []model.Heading {
	lines := splitLines(normaliseNewlines(content))
	fm := ParseFrontmatter(content)

	var out []model.Heading
	var fence string
	for i := fm.EndLine; i < len(lines); i++ {
		line := lines[i]
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1][:1] // remember whether it was ` or ~
			case strings.HasPrefix(strings.TrimSpace(line), fence):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			text := strings.TrimSpace(m[2])
			if text == "" {
				continue
			}
			out = append(out, model.Heading{Level: len(m[1]), Text: text, Line: i})
		}
	}
	return out
}

// ExtractWikilinks returns the note targets referenced with [[...]] syntax.
func ExtractWikilinks(content string) []string {
	matches := wikilinkRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out
}

// ExtractInlineTags returns #tags written in the note body, ignoring headings
// and fenced code.
func ExtractInlineTags(content string) []string {
	body := Body(content)
	lines := splitLines(body)
	var fence string
	var collected []string
	for _, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if fence == "" {
				fence = m[1][:1]
			} else if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
			}
			continue
		}
		if fence != "" || headingRe.MatchString(line) {
			continue
		}
		for _, m := range inlineTag.FindAllStringSubmatch(line, -1) {
			collected = append(collected, m[1])
		}
	}
	return normaliseTags(collected)
}

// Title resolves a note's display title: frontmatter title, else the first H1,
// else the supplied fallback (normally the file name).
func Title(content, fallback string) string {
	fm := ParseFrontmatter(content)
	if fm.Title != "" {
		return fm.Title
	}
	for _, h := range ExtractHeadings(content) {
		if h.Level == 1 {
			return h.Text
		}
	}
	return fallback
}

var stripMarkup = regexp.MustCompile(`[#*_>` + "`" + `\[\]()!]+`)

// Excerpt returns up to limit bytes of plain-ish text from the note body, used
// for keyword matching. The full note text is deliberately never stored.
func Excerpt(content string, limit int) string {
	body := Body(content)
	var b strings.Builder
	var fence string
	for _, line := range splitLines(body) {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if fence == "" {
				fence = m[1][:1]
			} else if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		clean := strings.TrimSpace(stripMarkup.ReplaceAllString(line, " "))
		clean = strings.Join(strings.Fields(clean), " ")
		if clean == "" {
			continue
		}
		if b.Len()+len(clean)+1 > limit {
			b.WriteString(clean[:max(0, limit-b.Len())])
			break
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(clean)
	}
	return strings.TrimSpace(b.String())
}
