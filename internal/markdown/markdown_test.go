package markdown_test

import (
	"strings"
	"testing"

	"github.com/alliebayless/murmur/internal/markdown"
	"github.com/alliebayless/murmur/internal/model"
)

func TestParseFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		wantOK     bool
		wantTitle  string
		wantTags   []string
		wantAlias  []string
		wantErr    bool
		wantEndsAt int
	}{
		{
			name: "full block",
			content: `---
title: ROG Flow Z13
aliases:
  - Z13
  - Flow Z13
tags:
  - linux
  - "#asus"
---

# Body
`,
			wantOK:     true,
			wantTitle:  "ROG Flow Z13",
			wantTags:   []string{"linux", "asus"},
			wantAlias:  []string{"Z13", "Flow Z13"},
			wantEndsAt: 9, // the closing --- is line 8, so the body starts at 9
		},
		{
			name:      "inline lists and comma strings",
			content:   "---\ntags: linux, asus\nalias: Z13\n---\nbody\n",
			wantOK:    true,
			wantTags:  []string{"linux", "asus"},
			wantAlias: []string{"Z13"},
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading\n",
			wantOK:  false,
		},
		{
			name:    "unterminated block is not frontmatter",
			content: "---\ntitle: nope\n\n# Heading\n",
			wantOK:  false,
		},
		{
			name:    "malformed yaml is reported, not fatal",
			content: "---\naliases: [one, two\n---\n\n# Heading\n",
			wantOK:  true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fm := markdown.ParseFrontmatter(tc.content)

			if fm.Present != tc.wantOK {
				t.Fatalf("Present = %v, want %v", fm.Present, tc.wantOK)
			}
			if (fm.Err != nil) != tc.wantErr {
				t.Fatalf("Err = %v, want error: %v", fm.Err, tc.wantErr)
			}
			if tc.wantTitle != "" && fm.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", fm.Title, tc.wantTitle)
			}
			if tc.wantTags != nil && !equalStrings(fm.Tags, tc.wantTags) {
				t.Errorf("Tags = %v, want %v", fm.Tags, tc.wantTags)
			}
			if tc.wantAlias != nil && !equalStrings(fm.Aliases, tc.wantAlias) {
				t.Errorf("Aliases = %v, want %v", fm.Aliases, tc.wantAlias)
			}
			if tc.wantEndsAt != 0 && fm.EndLine != tc.wantEndsAt {
				t.Errorf("EndLine = %d, want %d", fm.EndLine, tc.wantEndsAt)
			}
		})
	}
}

func TestBodyStripsFrontmatter(t *testing.T) {
	t.Parallel()
	body := markdown.Body("---\ntitle: X\n---\n\n# Heading\n\ntext\n")
	if strings.Contains(body, "title:") {
		t.Fatalf("frontmatter leaked into body: %q", body)
	}
	if !strings.Contains(body, "# Heading") {
		t.Fatalf("body lost its content: %q", body)
	}
}

func TestExtractHeadingsSkipsFencedCode(t *testing.T) {
	t.Parallel()
	content := `---
title: X
---

# Title

## Real Section

` + "```sh" + `
## not a heading
` + "```" + `

### Nested

Text with a # hash that is not a heading.

#### Deep ####
`
	got := markdown.ExtractHeadings(content)
	want := []struct {
		level int
		text  string
	}{
		{1, "Title"},
		{2, "Real Section"},
		{3, "Nested"},
		{4, "Deep"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d headings %v, want %d", len(got), headingTexts(got), len(want))
	}
	for i, w := range want {
		if got[i].Level != w.level || got[i].Text != w.text {
			t.Errorf("heading %d = H%d %q, want H%d %q", i, got[i].Level, got[i].Text, w.level, w.text)
		}
	}
}

func TestExtractWikilinksAndTags(t *testing.T) {
	t.Parallel()
	content := "# Note\n\nSee [[Fedora Suspend]] and [[Projects/Tidemail|Tidemail]] and [[Note#Section]].\n\nTagged #linux and #asus/laptop here.\n\n```\n#notatag\n```\n"

	links := markdown.ExtractWikilinks(content)
	wantLinks := []string{"Fedora Suspend", "Projects/Tidemail", "Note"}
	if !equalStrings(links, wantLinks) {
		t.Errorf("links = %v, want %v", links, wantLinks)
	}

	tags := markdown.ExtractInlineTags(content)
	wantTags := []string{"linux", "asus/laptop"}
	if !equalStrings(tags, wantTags) {
		t.Errorf("tags = %v, want %v", tags, wantTags)
	}
}

func TestTitleResolution(t *testing.T) {
	t.Parallel()
	if got := markdown.Title("---\ntitle: From YAML\n---\n\n# From H1\n", "file"); got != "From YAML" {
		t.Errorf("frontmatter title should win, got %q", got)
	}
	if got := markdown.Title("# From H1\n", "file"); got != "From H1" {
		t.Errorf("H1 should be used, got %q", got)
	}
	if got := markdown.Title("just text\n", "file"); got != "file" {
		t.Errorf("fallback should be used, got %q", got)
	}
}

func TestInsertUnderHeadingStopsAtSameLevel(t *testing.T) {
	t.Parallel()
	content := `# Note

## First

- existing item

## Second

text
`
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   "- [ ] new item",
		Section: "First",
		Mode:    model.InsertUnderHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	want := `# Note

## First

- existing item
- [ ] new item

## Second

text
`
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInsertUnderHeadingIgnoresDeeperSubheadings(t *testing.T) {
	t.Parallel()
	content := "# Note\n\n## Parent\n\ntext\n\n### Child\n\nmore\n\n## Sibling\n\nend\n"
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   "- added",
		Section: "Parent",
		Mode:    model.InsertUnderHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// The section owned by "## Parent" runs until "## Sibling", so the new
	// content lands after "more", not before "### Child".
	want := "# Note\n\n## Parent\n\ntext\n\n### Child\n\nmore\n\n- added\n\n## Sibling\n\nend\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInsertIntoEmptySection(t *testing.T) {
	t.Parallel()
	content := "# Tasks\n\n## Hardware\n\n## Errands\n"
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   "- [ ] Buy a UPS battery",
		Section: "Hardware",
		Mode:    model.InsertUnderHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	want := "# Tasks\n\n## Hardware\n\n- [ ] Buy a UPS battery\n\n## Errands\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInsertMissingHeadingIsAnError(t *testing.T) {
	t.Parallel()
	_, err := markdown.Insert(markdown.InsertRequest{
		Content: "# Note\n\n## Present\n",
		Block:   "- x",
		Section: "Absent",
		Mode:    model.InsertUnderHeading,
	})
	if err == nil {
		t.Fatal("expected an error for a missing heading")
	}
	if !strings.Contains(err.Error(), "heading not found") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestInsertCreateHeading(t *testing.T) {
	t.Parallel()
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: "# Note\n\n## Existing\n\ntext\n",
		Block:   "- [ ] task",
		Section: "Trackpad troubleshooting",
		Mode:    model.InsertCreateHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	want := "# Note\n\n## Existing\n\ntext\n\n## Trackpad troubleshooting\n\n- [ ] task\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestInsertCreateHeadingReusesExistingOne(t *testing.T) {
	t.Parallel()
	// The heading appeared between preview and save: do not duplicate it.
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: "# Note\n\n## Later Added\n\nold\n",
		Block:   "- new",
		Section: "later added",
		Mode:    model.InsertCreateHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if strings.Count(got, "Later Added") != 1 {
		t.Errorf("heading was duplicated:\n%s", got)
	}
}

func TestInsertIntoNewFile(t *testing.T) {
	t.Parallel()
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: "",
		Block:   "- [ ] first task",
		Section: "Tasks",
		Mode:    model.InsertCreateHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got != "## Tasks\n\n- [ ] first task\n" {
		t.Errorf("got %q", got)
	}
}

func TestInsertAppendEndPreservesFrontmatter(t *testing.T) {
	t.Parallel()
	content := "---\ntags:\n  - daily\n---\n\n# Note\n\ntext\n"
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   "- appended",
		Mode:    model.InsertAppendEnd,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !strings.HasPrefix(got, "---\ntags:\n  - daily\n---\n") {
		t.Errorf("frontmatter was damaged:\n%s", got)
	}
	if !strings.HasSuffix(got, "text\n\n- appended\n") {
		t.Errorf("append landed wrong:\n%q", got)
	}
}

func TestInsertPreservesCRLF(t *testing.T) {
	t.Parallel()
	content := "# Note\r\n\r\n## Log\r\n\r\n- existing\r\n"
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: content,
		Block:   "- added",
		Section: "Log",
		Mode:    model.InsertUnderHeading,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("mixed line endings in output: %q", got)
	}
	if !strings.Contains(got, "- existing\r\n- added\r\n") {
		t.Errorf("CRLF output wrong: %q", got)
	}
}

func TestInsertRejectsEmptyBlock(t *testing.T) {
	t.Parallel()
	if _, err := markdown.Insert(markdown.InsertRequest{
		Content: "# Note\n",
		Block:   "   \n\n",
		Mode:    model.InsertAppendEnd,
	}); err == nil {
		t.Fatal("expected an error for empty content")
	}
}

func TestInsertAlwaysEndsWithSingleNewline(t *testing.T) {
	t.Parallel()
	got, err := markdown.Insert(markdown.InsertRequest{
		Content: "# Note\n\n\n\n",
		Block:   "- x\n\n\n",
		Mode:    model.InsertAppendEnd,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !strings.HasSuffix(got, "- x\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("trailing newlines wrong: %q", got)
	}
}

func TestRemoveBlock(t *testing.T) {
	t.Parallel()
	content := "# Note\n\n## Log\n\n- keep me\n- [ ] remove me\n  - Added: 2026-08-05\n\n## Next\n"
	block := "- [ ] remove me\n  - Added: 2026-08-05"

	got, ok := markdown.RemoveBlock(content, block)
	if !ok {
		t.Fatal("block was not found")
	}
	if strings.Contains(got, "remove me") {
		t.Errorf("block survived:\n%s", got)
	}
	if !strings.Contains(got, "- keep me") || !strings.Contains(got, "## Next") {
		t.Errorf("removal damaged the note:\n%s", got)
	}

	if _, ok := markdown.RemoveBlock(content, "- not present"); ok {
		t.Error("reported success for an absent block")
	}
}

func TestDetectLineEnding(t *testing.T) {
	t.Parallel()
	if markdown.DetectLineEnding("a\r\nb\r\n") != markdown.CRLF {
		t.Error("CRLF not detected")
	}
	if markdown.DetectLineEnding("a\nb\n") != markdown.LF {
		t.Error("LF not detected")
	}
	if markdown.DetectLineEnding("no newline") != markdown.LF {
		t.Error("default should be LF")
	}
}

func TestExcerptIsBounded(t *testing.T) {
	t.Parallel()
	long := "# Title\n\n" + strings.Repeat("word ", 500)
	got := markdown.Excerpt(long, 100)
	if len(got) > 100 {
		t.Errorf("excerpt is %d bytes, want <= 100", len(got))
	}
	if strings.Contains(got, "#") {
		t.Errorf("markup leaked into the excerpt: %q", got)
	}
}

func headingTexts(hs []model.Heading) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Text)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
