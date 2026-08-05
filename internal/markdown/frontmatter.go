// Package markdown parses and edits Obsidian-flavoured Markdown files. It has
// no dependency on the rest of Murmur so that it can be tested purely against
// strings and temporary directories.
package markdown

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the YAML block at the top of an Obsidian note.
type Frontmatter struct {
	Present bool
	// Raw holds every key so that unknown fields survive a round trip when a
	// caller re-serialises. Murmur itself never rewrites frontmatter.
	Raw     map[string]any
	Title   string
	Aliases []string
	Tags    []string
	// EndLine is the index of the line *after* the closing delimiter, i.e. the
	// first line of the note body. Zero when no frontmatter is present.
	EndLine int
	// Err records a frontmatter block that exists but does not parse. The rest
	// of the note is still usable, so this is surfaced as a warning rather than
	// failing the whole index.
	Err error
}

// ParseFrontmatter extracts the leading YAML block from a note. Malformed YAML
// is reported through Frontmatter.Err instead of a returned error: a single bad
// note must never abort indexing the vault.
func ParseFrontmatter(content string) Frontmatter {
	lines := splitLines(normaliseNewlines(content))
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Frontmatter{}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end == -1 {
		// An unterminated block is not frontmatter; treat the file as body only.
		return Frontmatter{}
	}

	fm := Frontmatter{Present: true, EndLine: end + 1, Raw: map[string]any{}}
	block := strings.Join(lines[1:end], "\n")
	if err := yaml.Unmarshal([]byte(block), &fm.Raw); err != nil {
		fm.Err = fmt.Errorf("invalid YAML frontmatter: %w", err)
		return fm
	}
	if fm.Raw == nil {
		fm.Raw = map[string]any{}
	}
	fm.Title = stringValue(fm.Raw["title"])
	fm.Aliases = stringList(fm.Raw["aliases"])
	if len(fm.Aliases) == 0 {
		fm.Aliases = stringList(fm.Raw["alias"])
	}
	fm.Tags = normaliseTags(stringList(fm.Raw["tags"]))
	if len(fm.Tags) == 0 {
		fm.Tags = normaliseTags(stringList(fm.Raw["tag"]))
	}
	return fm
}

// Body returns the note content with any frontmatter block removed.
func Body(content string) string {
	fm := ParseFrontmatter(content)
	if !fm.Present {
		return content
	}
	lines := splitLines(normaliseNewlines(content))
	if fm.EndLine >= len(lines) {
		return ""
	}
	return strings.Join(lines[fm.EndLine:], "\n")
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// stringList coerces the several shapes Obsidian users write lists in: a plain
// string, a comma separated string, or a YAML sequence.
func stringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	}
	return nil
}

func normaliseTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}
