package router

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/alliebayless/murmur/internal/model"
)

// Rule is one deterministic routing rule from routes.yaml.
type Rule struct {
	Keywords []string `yaml:"keywords,omitempty"`
	Type     string   `yaml:"type,omitempty"`
	Note     string   `yaml:"note,omitempty"`
	Section  string   `yaml:"section,omitempty"`
	// FallbackNote applies only when no other stage produced a destination.
	FallbackNote string   `yaml:"fallback_note,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
}

// RuleSet is the parsed routes.yaml file.
type RuleSet struct {
	Routes []Rule `yaml:"routes"`
}

// LoadRules reads routing rules from disk. A missing file is not an error:
// Murmur works fine with no rules at all.
func LoadRules(path string) (RuleSet, error) {
	var rs RuleSet
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rs, nil
	}
	if err != nil {
		return rs, fmt.Errorf("read routing rules %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return rs, fmt.Errorf("parse routing rules %s: %w\n\nCheck the YAML syntax; Murmur will route without rules until this is fixed.", path, err)
	}
	return rs, nil
}

// Match reports the first rule that matches the thought and yields a
// destination note. Keyword matching is case-insensitive and substring based so
// that "z13" matches "the z13 trackpad".
func (rs RuleSet) Match(text string, ctype model.ContentType) (Rule, bool) {
	lower := strings.ToLower(text)
	for _, r := range rs.Routes {
		if r.Note == "" {
			continue
		}
		if !r.matches(lower, ctype) {
			continue
		}
		return r, true
	}
	return Rule{}, false
}

// Fallback returns the first fallback_note rule matching the content type.
func (rs RuleSet) Fallback(text string, ctype model.ContentType) (Rule, bool) {
	lower := strings.ToLower(text)
	for _, r := range rs.Routes {
		if r.FallbackNote == "" {
			continue
		}
		if !r.matches(lower, ctype) {
			continue
		}
		return r, true
	}
	return Rule{}, false
}

func (r Rule) matches(lowerText string, ctype model.ContentType) bool {
	if r.Type != "" {
		want, ok := model.ParseContentType(r.Type)
		if !ok || want != ctype {
			return false
		}
		if len(r.Keywords) == 0 {
			return true
		}
	}
	if len(r.Keywords) == 0 {
		return r.Type != ""
	}
	for _, kw := range r.Keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lowerText, kw) {
			return true
		}
	}
	return false
}

// MatchedKeyword returns the keyword responsible for a match, for explanations.
func (r Rule) MatchedKeyword(text string) string {
	lower := strings.ToLower(text)
	for _, kw := range r.Keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return kw
		}
	}
	return ""
}

// ExpandTemplate substitutes Murmur's date variables in a path or heading.
//
//	{{date}}  formatted with dateFormat
//	{{year}}  2006
//	{{month}} 01
//	{{day}}   02
//	{{time}}  formatted with timeFormat
func ExpandTemplate(s string, now time.Time, dateFormat, timeFormat string) string {
	if dateFormat == "" {
		dateFormat = "2006-01-02"
	}
	if timeFormat == "" {
		timeFormat = "15:04"
	}
	replacements := []struct{ from, to string }{
		{"{{date}}", now.Format(dateFormat)},
		{"{{ date }}", now.Format(dateFormat)},
		{"{{year}}", now.Format("2006")},
		{"{{ year }}", now.Format("2006")},
		{"{{month}}", now.Format("01")},
		{"{{ month }}", now.Format("01")},
		{"{{day}}", now.Format("02")},
		{"{{ day }}", now.Format("02")},
		{"{{time}}", now.Format(timeFormat)},
		{"{{ time }}", now.Format(timeFormat)},
		{"{{weekday}}", now.Format("Monday")},
		{"{{ weekday }}", now.Format("Monday")},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}
