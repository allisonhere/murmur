package provider_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/provider"
)

func TestNoneProviderDeclines(t *testing.T) {
	t.Parallel()
	_, err := provider.None{}.Classify(context.Background(), provider.ClassificationRequest{})
	if err != provider.ErrNoProvider {
		t.Fatalf("err = %v, want ErrNoProvider", err)
	}
}

func TestNewProviderSelection(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	if c, err := provider.New(cfg); err != nil || c.Name() != "none" {
		t.Errorf("default should be none, got %v (%v)", c, err)
	}

	cfg.AI.Provider = "ollama"
	cfg.AI.Model = "llama3.1"
	if c, err := provider.New(cfg); err != nil || !strings.HasPrefix(c.Name(), "ollama") {
		t.Errorf("ollama not selected: %v (%v)", c, err)
	}

	cfg.AI.Provider = "openai"
	cfg.AI.BaseURL = "http://localhost:1234/v1" // local gateway: no key needed
	if c, err := provider.New(cfg); err != nil || !strings.HasPrefix(c.Name(), "openai") {
		t.Errorf("openai not selected: %v (%v)", c, err)
	}

	cfg.AI.BaseURL = "" // defaults to api.openai.com, which needs a key
	cfg.AI.APIKeyEnv = "MURMUR_TEST_KEY_THAT_IS_UNSET"
	if _, err := provider.New(cfg); err == nil {
		t.Error("expected an error when no API key is available")
	}

	cfg.AI.Provider = "definitely-not-real"
	if _, err := provider.New(cfg); err == nil {
		t.Error("expected an error for an unknown provider")
	}
}

func TestParseResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		want  string
		isErr bool
	}{
		{
			name: "plain json",
			raw:  `{"note_path":"Inbox.md","content_type":"task","markdown":"- [ ] x"}`,
			want: "Inbox.md",
		},
		{
			name: "fenced json",
			raw:  "```json\n{\"note_path\":\"Inbox.md\",\"content_type\":\"task\",\"markdown\":\"- [ ] x\"}\n```",
			want: "Inbox.md",
		},
		{
			name: "json with surrounding prose",
			raw:  "Sure! Here you go:\n{\"note_path\":\"Inbox.md\",\"content_type\":\"task\",\"markdown\":\"- x\"}\nHope that helps.",
			want: "Inbox.md",
		},
		{name: "empty", raw: "   ", isErr: true},
		{name: "not json", raw: "I cannot help with that.", isErr: true},
		{name: "broken json", raw: `{"note_path": }`, isErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := provider.ParseResult(tc.raw)
			if tc.isErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResult: %v", err)
			}
			if got.NotePath != tc.want {
				t.Errorf("NotePath = %q, want %q", got.NotePath, tc.want)
			}
		})
	}
}

func TestValidateAcceptsGoodResult(t *testing.T) {
	t.Parallel()

	res := provider.ClassificationResult{
		NotePath:   "Projects/Tidemail",
		Section:    "## Roadmap",
		Type:       "task",
		Tags:       []string{"#tui", "tui", "", "with space"},
		Markdown:   "- [ ] Add attachment previews",
		Confidence: 1.4,
	}
	if err := provider.Validate(&res, []string{"Projects/Tidemail.md"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.NotePath != "Projects/Tidemail.md" {
		t.Errorf("path was not normalised: %q", res.NotePath)
	}
	if res.Section != "Roadmap" {
		t.Errorf("section markers were not stripped: %q", res.Section)
	}
	if len(res.Tags) != 1 || res.Tags[0] != "tui" {
		t.Errorf("tags were not cleaned: %v", res.Tags)
	}
	if res.Confidence != 1 {
		t.Errorf("confidence was not clamped: %v", res.Confidence)
	}
}

func TestValidateRejectsUnsafeResults(t *testing.T) {
	t.Parallel()

	allowed := []string{"Inbox.md"}
	tests := []struct {
		name string
		res  provider.ClassificationResult
	}{
		{"no path", provider.ClassificationResult{Type: "task", Markdown: "- x"}},
		{"absolute path", provider.ClassificationResult{NotePath: "/etc/passwd.md", Type: "task", Markdown: "- x"}},
		{"home path", provider.ClassificationResult{NotePath: "~/notes.md", Type: "task", Markdown: "- x"}},
		{"traversal", provider.ClassificationResult{NotePath: "../../escape.md", Type: "task", Markdown: "- x"}},
		{"deep invented path", provider.ClassificationResult{NotePath: "a/b/c/d/e/f.md", Type: "task", Markdown: "- x"}},
		{"unknown type", provider.ClassificationResult{NotePath: "Inbox.md", Type: "haiku", Markdown: "- x"}},
		{"empty markdown", provider.ClassificationResult{NotePath: "Inbox.md", Type: "task", Markdown: "   "}},
		{"frontmatter", provider.ClassificationResult{NotePath: "Inbox.md", Type: "task", Markdown: "---\ntags: x\n---"}},
		{"top level heading", provider.ClassificationResult{NotePath: "Inbox.md", Type: "task", Markdown: "# Overwrite the title"}},
		{"multiline section", provider.ClassificationResult{NotePath: "Inbox.md", Type: "task", Section: "a\nb", Markdown: "- x"}},
		{"oversized markdown", provider.ClassificationResult{NotePath: "Inbox.md", Type: "task", Markdown: strings.Repeat("x", provider.MaxMarkdownBytes+1)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := tc.res
			if err := provider.Validate(&res, allowed); err == nil {
				t.Errorf("Validate accepted an unsafe result: %+v", res)
			}
		})
	}
}

func TestValidateAcceptsSynonymType(t *testing.T) {
	t.Parallel()
	res := provider.ClassificationResult{NotePath: "Inbox.md", Type: "todo", Markdown: "- [ ] x"}
	if err := provider.Validate(&res, nil); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Type != model.TypeTask {
		t.Errorf("type = %q, want task", res.Type)
	}
}
