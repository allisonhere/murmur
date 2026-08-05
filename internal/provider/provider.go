// Package provider defines Murmur's optional AI classification interface and
// its implementations. Murmur is fully usable with provider "none": everything
// here is an enhancement layered on top of deterministic routing.
//
// Privacy: a request carries the captured thought and metadata for a handful of
// candidate notes. The vault itself is never sent.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// ErrNoProvider is returned by the "none" provider and signals the caller to
// keep the deterministic result.
var ErrNoProvider = errors.New("no AI provider configured")

// CandidateInfo is the bounded metadata sent for one candidate note.
type CandidateInfo struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Headings []string `json:"headings,omitempty"`
}

// ClassificationRequest is everything the model is allowed to see.
type ClassificationRequest struct {
	Thought      string          `json:"thought"`
	Candidates   []CandidateInfo `json:"candidate_notes"`
	ContentTypes []string        `json:"content_types"`
	// Suggested is Murmur's deterministic answer, offered as a starting point.
	Suggested       ClassificationResult `json:"current_suggestion"`
	FormattingRules string               `json:"formatting_rules"`
	Today           string               `json:"today"`
}

// ClassificationResult is the structured answer required from a provider.
type ClassificationResult struct {
	NotePath   string            `json:"note_path"`
	Section    string            `json:"section"`
	Type       model.ContentType `json:"content_type"`
	Tags       []string          `json:"tags"`
	Markdown   string            `json:"markdown"`
	Confidence float64           `json:"confidence"`
	Reason     string            `json:"reason"`
}

// Classifier turns a thought into a routing decision.
type Classifier interface {
	Classify(ctx context.Context, req ClassificationRequest) (ClassificationResult, error)
	Name() string
}

// New builds the configured provider. An unknown provider name is an error, but
// callers may treat it as "fall back to deterministic routing".
func New(cfg config.Config) (Classifier, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.AI.Provider)) {
	case "", config.ProviderNone:
		return None{}, nil
	case config.ProviderOllama:
		return NewOllama(cfg), nil
	case config.ProviderOpenAI, "openai-compatible", "openai_compatible":
		return NewOpenAI(cfg)
	default:
		return None{}, fmt.Errorf("unknown AI provider %q; use none, ollama or openai", cfg.AI.Provider)
	}
}

// None is the default provider: it does nothing, on purpose.
type None struct{}

// Name implements Classifier.
func (None) Name() string { return "none" }

// Classify implements Classifier by declining.
func (None) Classify(context.Context, ClassificationRequest) (ClassificationResult, error) {
	return ClassificationResult{}, ErrNoProvider
}

// SystemPrompt is the instruction shared by every provider.
const SystemPrompt = `You route short captured thoughts into an Obsidian vault.

You will receive a thought, a small set of candidate notes with their metadata,
and Murmur's own suggestion. Reply with a single JSON object and nothing else:

{
  "note_path": "<one of the candidate paths, or a new path ending in .md>",
  "section":   "<a heading from that note, a new heading, or an empty string>",
  "content_type": "<task|idea|journal|project|reference|question|bookmark|note>",
  "tags": ["lowercase", "tags"],
  "markdown": "<the exact Markdown block to insert>",
  "confidence": 0.0,
  "reason": "<one short sentence>"
}

Rules:
- Never invent content the user did not imply. Tidy their words; do not expand them.
- Keep the Markdown to the smallest block that expresses the thought.
- Do not include YAML frontmatter or a top-level heading in "markdown".
- Prefer an existing candidate note over inventing a new one.
- confidence is your own 0..1 estimate.`

// FormattingRules describes Murmur's Markdown conventions for the model.
const FormattingRules = `task: "- [ ] Do the thing" optionally followed by "  - Added: YYYY-MM-DD"
question: "- [ ] Research: <the question>?"
idea: an Obsidian callout starting "> [!idea]"
journal: a single bullet, past tense
bookmark: "- [Title](https://url)"
reference/note: a single bullet
project: one or more plain paragraphs`

// ParseResult decodes a model reply, tolerating the common habit of wrapping
// JSON in prose or a fenced code block.
func ParseResult(raw string) (ClassificationResult, error) {
	var res ClassificationResult
	text := strings.TrimSpace(raw)
	if text == "" {
		return res, errors.New("provider returned an empty response")
	}
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		text = strings.TrimSpace(rest)
	}
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return res, fmt.Errorf("provider response was not JSON: %.120s", text)
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &res); err != nil {
		return res, fmt.Errorf("provider returned invalid JSON: %w", err)
	}
	return res, nil
}

// MaxMarkdownBytes bounds how much Markdown a provider may return. A model that
// wants to write an essay into someone's vault is a bug, not a feature.
const MaxMarkdownBytes = 4000

// Validate checks a provider result before Murmur is willing to act on it.
// allowedPaths is the set of candidate paths; a result may also propose a new
// path, but it must be a plausible vault-relative Markdown path.
func Validate(res *ClassificationResult, allowedPaths []string) error {
	res.NotePath = strings.TrimSpace(strings.ReplaceAll(res.NotePath, "\\", "/"))
	if res.NotePath == "" {
		return errors.New("provider omitted note_path")
	}
	if strings.HasPrefix(res.NotePath, "/") || strings.HasPrefix(res.NotePath, "~") {
		return fmt.Errorf("provider returned an absolute path %q", res.NotePath)
	}
	for _, seg := range strings.Split(res.NotePath, "/") {
		if seg == ".." {
			return fmt.Errorf("provider returned a traversing path %q", res.NotePath)
		}
	}
	if !strings.HasSuffix(strings.ToLower(res.NotePath), ".md") {
		res.NotePath += ".md"
	}
	if len(allowedPaths) > 0 {
		known := false
		for _, p := range allowedPaths {
			if strings.EqualFold(p, res.NotePath) {
				known = true
				break
			}
		}
		// A brand new note is allowed, but it must not look like a path outside
		// the user's vocabulary of folders.
		if !known && strings.Count(res.NotePath, "/") > 4 {
			return fmt.Errorf("provider proposed an implausible new note %q", res.NotePath)
		}
	}

	if !res.Type.Valid() {
		if t, ok := model.ParseContentType(string(res.Type)); ok {
			res.Type = t
		} else {
			return fmt.Errorf("provider returned unknown content type %q", res.Type)
		}
	}

	res.Markdown = strings.TrimSpace(res.Markdown)
	if res.Markdown == "" {
		return errors.New("provider returned empty markdown")
	}
	if len(res.Markdown) > MaxMarkdownBytes {
		return fmt.Errorf("provider returned %d bytes of markdown (limit %d)", len(res.Markdown), MaxMarkdownBytes)
	}
	if strings.HasPrefix(res.Markdown, "---") {
		return errors.New("provider tried to write YAML frontmatter")
	}
	if strings.HasPrefix(res.Markdown, "# ") {
		return errors.New("provider tried to write a top-level heading")
	}

	res.Section = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(res.Section), "#"))
	if strings.ContainsAny(res.Section, "\n\r") {
		return errors.New("provider returned a multi-line section")
	}

	clean := make([]string, 0, len(res.Tags))
	seen := map[string]bool{}
	for _, t := range res.Tags {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t == "" || strings.ContainsAny(t, " \t\n") || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		clean = append(clean, t)
		if len(clean) == 8 {
			break
		}
	}
	res.Tags = clean

	if res.Confidence < 0 {
		res.Confidence = 0
	}
	if res.Confidence > 1 {
		res.Confidence = 1
	}
	return nil
}
