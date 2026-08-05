package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/alliebayless/murmur/internal/model"
)

// ErrHeadingNotFound is returned when an insertion targets a heading that is no
// longer present in the document.
var ErrHeadingNotFound = errors.New("heading not found")

// InsertRequest describes a single insertion into a note.
type InsertRequest struct {
	// Content is the current file content. Empty means the file does not exist
	// yet and will be created.
	Content string
	// Block is the Markdown to insert. Trailing newlines are ignored.
	Block string
	// Section is the target heading text. Ignored for InsertAppendEnd.
	Section string
	Mode    model.InsertMode
	// HeadingLevel is used when creating a new heading. Defaults to 2.
	HeadingLevel int
}

// FindHeading locates a heading by its text, comparing case-insensitively and
// ignoring surrounding whitespace. The first match wins.
func FindHeading(content, text string) (model.Heading, bool) {
	want := normaliseHeading(text)
	if want == "" {
		return model.Heading{}, false
	}
	for _, h := range ExtractHeadings(content) {
		if normaliseHeading(h.Text) == want {
			return h, true
		}
	}
	return model.Heading{}, false
}

func normaliseHeading(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "#")))
}

// sectionEnd returns the exclusive end line of the section owned by the heading
// at index h. A section ends at the next heading of the same or higher level
// (i.e. a smaller or equal '#' count), or at the end of the document.
func sectionEnd(lines []string, headings []model.Heading, h model.Heading) int {
	for _, other := range headings {
		if other.Line > h.Line && other.Level <= h.Level {
			return other.Line
		}
	}
	return len(lines)
}

var listItemRe = regexp.MustCompile(`^\s*(?:[-*+]\s|\d+[.)]\s|>\s*\[!)`)

func isListLike(s string) bool {
	return listItemRe.MatchString(s)
}

// Insert applies an insertion and returns the complete new file content. The
// original line ending style is preserved and the result always ends with
// exactly one newline.
func Insert(req InsertRequest) (string, error) {
	le := DetectLineEnding(req.Content)
	content := normaliseNewlines(req.Content)
	block := strings.TrimRight(normaliseNewlines(req.Block), "\n")
	if strings.TrimSpace(block) == "" {
		return "", errors.New("nothing to insert: the formatted content is empty")
	}
	level := req.HeadingLevel
	if level < 1 || level > 6 {
		level = 2
	}

	var result string
	var err error
	switch req.Mode {
	case model.InsertAppendEnd:
		result = appendToEnd(content, block)
	case model.InsertUnderHeading:
		result, err = insertUnderHeading(content, block, req.Section)
	case model.InsertCreateHeading:
		// If the heading turned up after all (for example the user created it
		// by hand between preview and save) reuse it rather than duplicating.
		if _, ok := FindHeading(content, req.Section); ok {
			result, err = insertUnderHeading(content, block, req.Section)
			break
		}
		result = createHeadingAndInsert(content, block, req.Section, level)
	default:
		return "", fmt.Errorf("unknown insert mode %q", req.Mode)
	}
	if err != nil {
		return "", err
	}

	result = strings.TrimRight(result, "\n") + "\n"
	return string(le.Restore(result)), nil
}

func appendToEnd(content, block string) string {
	trimmed := strings.TrimRight(content, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return block
	}
	lines := splitLines(trimmed)
	sep := "\n\n"
	if isListLike(lines[len(lines)-1]) && isListLike(block) {
		sep = "\n"
	}
	return trimmed + sep + block
}

func insertUnderHeading(content, block, section string) (string, error) {
	h, ok := FindHeading(content, section)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrHeadingNotFound, section)
	}
	lines := splitLines(content)
	headings := ExtractHeadings(content)
	end := sectionEnd(lines, headings, h)

	// Walk back over trailing blank lines so the new content sits immediately
	// after the section's last real line rather than after its padding.
	ins := end
	for ins > h.Line+1 && strings.TrimSpace(lines[ins-1]) == "" {
		ins--
	}

	var chunk []string
	empty := ins == h.Line+1
	if !empty && !(isListLike(lines[ins-1]) && isListLike(block)) {
		chunk = append(chunk, "")
	}
	if empty {
		chunk = append(chunk, "")
	}
	chunk = append(chunk, splitLines(block)...)
	if ins < len(lines) && strings.TrimSpace(lines[ins]) != "" {
		chunk = append(chunk, "")
	}

	out := make([]string, 0, len(lines)+len(chunk))
	out = append(out, lines[:ins]...)
	out = append(out, chunk...)
	out = append(out, lines[ins:]...)
	return strings.Join(out, "\n"), nil
}

func createHeadingAndInsert(content, block, section string, level int) string {
	heading := strings.Repeat("#", level) + " " + strings.TrimSpace(section)
	trimmed := strings.TrimRight(content, "\n")
	if strings.TrimSpace(trimmed) == "" {
		// A brand new (or frontmatter-only) file.
		if trimmed == "" {
			return heading + "\n\n" + block
		}
		return trimmed + "\n\n" + heading + "\n\n" + block
	}
	return trimmed + "\n\n" + heading + "\n\n" + block
}

// RemoveBlock deletes the first exact occurrence of block from content. It is
// used by undo when no full backup is available, and reports whether the block
// was found.
func RemoveBlock(content, block string) (string, bool) {
	le := DetectLineEnding(content)
	norm := normaliseNewlines(content)
	target := strings.TrimRight(normaliseNewlines(block), "\n")
	if target == "" {
		return content, false
	}
	lines := splitLines(norm)
	blockLines := splitLines(target)

	for i := 0; i+len(blockLines) <= len(lines); i++ {
		match := true
		for j := range blockLines {
			if lines[i+j] != blockLines[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		start, end := i, i+len(blockLines)
		// Absorb one blank line of the padding we are likely to have added.
		if end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++
		} else if start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		out := append(append([]string{}, lines[:start]...), lines[end:]...)
		joined := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
		return string(le.Restore(joined)), true
	}
	return content, false
}
