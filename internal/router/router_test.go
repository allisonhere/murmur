package router_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/router"
)

var now = time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC)

// ------------------------------------------------------------------ fixtures

func testNotes() []model.Note {
	return []model.Note{
		{
			RelPath:  "Projects/Linux/ROG Flow Z13.md",
			FileName: "ROG Flow Z13",
			Title:    "ROG Flow Z13",
			Aliases:  []string{"Z13"},
			Tags:     []string{"linux", "asus", "z13"},
			Headings: []model.Heading{
				{Level: 1, Text: "ROG Flow Z13", Line: 1},
				{Level: 2, Text: "Hardware notes", Line: 5},
				{Level: 2, Text: "Trackpad troubleshooting", Line: 10},
			},
			Excerpt: "notes on running linux on the asus rog flow z13 hid_asus trackpad",
			ModTime: now.Add(-48 * time.Hour),
		},
		{
			RelPath:  "Projects/Tidemail.md",
			FileName: "Tidemail",
			Title:    "Tidemail",
			Tags:     []string{"tui", "email"},
			Headings: []model.Heading{
				{Level: 2, Text: "Roadmap", Line: 4},
				{Level: 2, Text: "Ideas", Line: 8},
			},
			Excerpt: "a terminal email client written in go attachments",
			ModTime: now.Add(-24 * time.Hour),
		},
		{
			RelPath:  "Reference/Fedora Suspend.md",
			FileName: "Fedora Suspend",
			Title:    "Fedora Suspend",
			Tags:     []string{"linux", "fedora", "nvidia"},
			Headings: []model.Heading{{Level: 2, Text: "Findings", Line: 3}},
			Excerpt:  "resume fails when the nvidia driver keeps vram allocated",
			ModTime:  now.Add(-72 * time.Hour),
		},
		{
			RelPath:  "Inbox/Tasks.md",
			FileName: "Tasks",
			Title:    "Tasks",
			Headings: []model.Heading{{Level: 2, Text: "Hardware", Line: 2}},
			ModTime:  now.Add(-time.Hour),
		},
	}
}

func testConfig() config.Config {
	cfg := config.Default()
	cfg.VaultPath = "/tmp/vault"
	return cfg
}

// --------------------------------------------------------------------- hints

func TestParseHints(t *testing.T) {
	t.Parallel()

	known := func(c string) bool {
		return c == "Projects/Linux/ROG Flow Z13" || c == "Projects/Tidemail"
	}

	tests := []struct {
		name        string
		input       string
		wantClean   string
		wantPath    string
		wantProject string
		wantType    model.ContentType
		wantTags    []string
	}{
		{
			name:        "project hint",
			input:       "@tidemail Add an attachment preview",
			wantClean:   "Add an attachment preview",
			wantProject: "tidemail",
		},
		{
			name:      "type hint",
			input:     "#journal Finally fixed the trackpad issue",
			wantClean: "Finally fixed the trackpad issue",
			wantType:  model.TypeJournal,
		},
		{
			name:      "path hint with spaces",
			input:     ">Projects/Linux/ROG Flow Z13 investigate hid_asus",
			wantClean: "investigate hid_asus",
			wantPath:  "Projects/Linux/ROG Flow Z13",
		},
		{
			name:      "quoted path hint",
			input:     `>"Inbox/Some Note.md" buy milk`,
			wantClean: "buy milk",
			wantPath:  "Inbox/Some Note.md",
		},
		{
			name:      "unknown path falls back to one token",
			input:     ">Inbox/New.md write it down",
			wantClean: "write it down",
			wantPath:  "Inbox/New.md",
		},
		{
			name:      "tags are collected and removed",
			input:     "Fix the fan curve #linux #hardware",
			wantClean: "Fix the fan curve",
			wantTags:  []string{"linux", "hardware"},
		},
		{
			name:        "combined hints",
			input:       "@tidemail #task add attachment preview #ui",
			wantClean:   "add attachment preview",
			wantProject: "tidemail",
			wantType:    model.TypeTask,
			wantTags:    []string{"ui"},
		},
		{
			name:      "no hints",
			input:     "just a plain thought",
			wantClean: "just a plain thought",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clean, h := router.ParseHints(tc.input, known)

			if clean != tc.wantClean {
				t.Errorf("clean = %q, want %q", clean, tc.wantClean)
			}
			if h.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", h.Path, tc.wantPath)
			}
			if h.Project != tc.wantProject {
				t.Errorf("project = %q, want %q", h.Project, tc.wantProject)
			}
			if h.Type != tc.wantType {
				t.Errorf("type = %q, want %q", h.Type, tc.wantType)
			}
			if tc.wantTags != nil && strings.Join(h.Tags, ",") != strings.Join(tc.wantTags, ",") {
				t.Errorf("tags = %v, want %v", h.Tags, tc.wantTags)
			}
		})
	}
}

// --------------------------------------------------------------------- rules

func TestRuleMatching(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yaml")
	body := `routes:
  - keywords:
      - z13
      - rog flow
      - hid_asus
    note: Projects/Linux/ROG Flow Z13.md
    section: Trackpad troubleshooting

  - keywords:
      - tidemail
    note: Projects/Tidemail.md
    tags:
      - tui

  - type: journal
    note: Daily/{{date}}.md

  - type: task
    fallback_note: Inbox/Tasks.md
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rs, err := router.LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(rs.Routes) != 4 {
		t.Fatalf("parsed %d rules, want 4", len(rs.Routes))
	}

	r, ok := rs.Match("the z13 trackpad is flaky", model.TypeTask)
	if !ok {
		t.Fatal("keyword rule did not match")
	}
	if r.Note != "Projects/Linux/ROG Flow Z13.md" || r.Section != "Trackpad troubleshooting" {
		t.Errorf("matched the wrong rule: %+v", r)
	}
	if kw := r.MatchedKeyword("the z13 trackpad is flaky"); kw != "z13" {
		t.Errorf("MatchedKeyword = %q, want z13", kw)
	}

	if r, ok := rs.Match("wrote about nothing in particular", model.TypeJournal); !ok || r.Note != "Daily/{{date}}.md" {
		t.Errorf("type-only rule did not match: %+v", r)
	}
	if _, ok := rs.Match("something unrelated", model.TypeNote); ok {
		t.Error("a rule matched when none should have")
	}

	fb, ok := rs.Fallback("buy milk", model.TypeTask)
	if !ok || fb.FallbackNote != "Inbox/Tasks.md" {
		t.Errorf("fallback rule did not match: %+v", fb)
	}
	if _, ok := rs.Fallback("buy milk", model.TypeIdea); ok {
		t.Error("fallback matched the wrong type")
	}
}

func TestLoadRulesMissingFileIsFine(t *testing.T) {
	t.Parallel()
	rs, err := router.LoadRules(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("a missing rules file should not be an error: %v", err)
	}
	if len(rs.Routes) != 0 {
		t.Error("expected no rules")
	}
}

func TestLoadRulesReportsBadYAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "routes.yaml")
	if err := os.WriteFile(path, []byte("routes: [ unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := router.LoadRules(path); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestExpandTemplate(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Daily/{{date}}.md":            "Daily/2026-08-05.md",
		"Journal/{{year}}/{{month}}":   "Journal/2026/08",
		"{{year}}-{{month}}-{{day}}":   "2026-08-05",
		"Log {{ date }} at {{time}}":   "Log 2026-08-05 at 09:30",
		"{{weekday}}":                  "Wednesday",
		"nothing to expand":            "nothing to expand",
		"Daily/{{date}}/{{date}}.md":   "Daily/2026-08-05/2026-08-05.md",
		"{{unknown}} stays as written": "{{unknown}} stays as written",
	}
	for in, want := range tests {
		if got := router.ExpandTemplate(in, now, "2006-01-02", "15:04"); got != want {
			t.Errorf("ExpandTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseNotePath(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Projects/Tidemail":     "Projects/Tidemail.md",
		"Projects/Tidemail.md":  "Projects/Tidemail.md",
		"/Inbox.md":             "Inbox.md",
		"./Inbox":               "Inbox.md",
		`Projects\Windows\a.md`: "Projects/Windows/a.md",
		"":                      "",
	}
	for in, want := range tests {
		if got := router.NormaliseNotePath(in); got != want {
			t.Errorf("NormaliseNotePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// ------------------------------------------------------------------- ranking

func TestRankPrefersTheObviousNote(t *testing.T) {
	t.Parallel()

	text := "investigate why the z13 trackpad is detected as a fallback mouse"
	cands := router.Rank(router.RankInput{
		Text:   text,
		Tokens: router.Tokenize(text),
		Notes:  testNotes(),
		Now:    now,
	}, 3)

	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if cands[0].Note.RelPath != "Projects/Linux/ROG Flow Z13.md" {
		t.Errorf("top candidate = %s, want the Z13 note (all: %v)", cands[0].Note.RelPath, paths(cands))
	}
	if cands[0].Reason() == "" {
		t.Error("the top candidate has no explanation")
	}
	if len(cands) > 3 {
		t.Errorf("returned %d candidates, want at most 3", len(cands))
	}
}

func TestRankUsesTagsAndLearning(t *testing.T) {
	t.Parallel()

	text := "the nvidia driver blocks resume"
	notes := testNotes()

	cands := router.Rank(router.RankInput{
		Text: text, Tokens: router.Tokenize(text), Notes: notes, Now: now,
	}, 3)
	if len(cands) == 0 || cands[0].Note.RelPath != "Reference/Fedora Suspend.md" {
		t.Fatalf("tag matching failed, got %v", paths(cands))
	}

	// A strong learned signal should be able to move a weaker note to the top.
	learned := map[string]router.LearnedWeight{"Inbox/Tasks.md": {Weight: 4}}
	withLearning := router.Rank(router.RankInput{
		Text: "nvidia", Tokens: router.Tokenize("nvidia"), Notes: notes, Now: now, Learned: learned,
	}, 3)
	found := false
	for _, c := range withLearning {
		if c.Note.RelPath == "Inbox/Tasks.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("the learned destination never appeared: %v", paths(withLearning))
	}
}

func TestRankProjectHintDominates(t *testing.T) {
	t.Parallel()
	text := "add an attachment preview"
	cands := router.Rank(router.RankInput{
		Text: text, Tokens: router.Tokenize(text), Notes: testNotes(), Now: now, Project: "tidemail",
	}, 3)
	if len(cands) == 0 || cands[0].Note.RelPath != "Projects/Tidemail.md" {
		t.Fatalf("project hint ignored, got %v", paths(cands))
	}
}

func TestConfidence(t *testing.T) {
	t.Parallel()

	if got := router.Confidence(nil); got != 0 {
		t.Errorf("no candidates should give zero confidence, got %v", got)
	}

	clear := []model.Candidate{{Score: 20}, {Score: 2}}
	ambiguous := []model.Candidate{{Score: 20}, {Score: 19}}
	if router.Confidence(clear) <= router.Confidence(ambiguous) {
		t.Errorf("a clear winner (%v) should beat an ambiguous one (%v)",
			router.Confidence(clear), router.Confidence(ambiguous))
	}
	for _, cs := range [][]model.Candidate{clear, ambiguous, {{Score: 1000}}} {
		if c := router.Confidence(cs); c < 0 || c > 1 {
			t.Errorf("confidence %v is out of range", c)
		}
	}
}

func TestTokenizeDropsStopwords(t *testing.T) {
	t.Parallel()
	got := router.Tokenize("Why does the NVIDIA driver fail on Fedora?")
	for _, tok := range got {
		if tok == "the" || tok == "does" || tok == "why" || tok == "on" {
			t.Errorf("stopword %q survived tokenisation: %v", tok, got)
		}
	}
	if !contains(got, "nvidia") || !contains(got, "fedora") {
		t.Errorf("meaningful tokens missing: %v", got)
	}
}

func TestSuggestHeading(t *testing.T) {
	t.Parallel()
	note := testNotes()[0]
	if got := router.SuggestHeading(note, router.Tokenize("trackpad is flaky")); got != "Trackpad troubleshooting" {
		t.Errorf("SuggestHeading = %q", got)
	}
	if got := router.SuggestHeading(note, router.Tokenize("completely unrelated words")); got != "" {
		t.Errorf("expected no heading, got %q", got)
	}
}

func TestFuzzySearch(t *testing.T) {
	t.Parallel()
	notes := testNotes()

	results := router.FuzzySearch(notes, "tidemail", 10)
	if len(results) == 0 || results[0].Note.RelPath != "Projects/Tidemail.md" {
		t.Fatalf("exact search failed: %v", results)
	}
	if !strings.Contains(results[0].Reason, "Matched") {
		t.Errorf("no reason given: %q", results[0].Reason)
	}

	if r := router.FuzzySearch(notes, "z13", 10); len(r) == 0 || r[0].Note.RelPath != "Projects/Linux/ROG Flow Z13.md" {
		t.Errorf("alias/name search failed: %v", r)
	}
	if r := router.FuzzySearch(notes, "", 2); len(r) != 2 {
		t.Errorf("empty query should list recent notes, got %d", len(r))
	}
	if r := router.FuzzySearch(notes, "zzzzqqqq", 10); len(r) != 0 {
		t.Errorf("expected no matches, got %v", r)
	}
}

// -------------------------------------------------------------------- engine

func TestEngineExplicitHintsWin(t *testing.T) {
	t.Parallel()
	e := router.NewEngine(testConfig(), router.RuleSet{}, testNotes(), nil)

	res := e.Route(router.Request{Text: ">Inbox/Someday.md remember the milk", Now: now})
	if res.Routing.NotePath != "Inbox/Someday.md" {
		t.Errorf("explicit path ignored: %s", res.Routing.NotePath)
	}
	if res.Routing.Source != model.SourceExplicit || res.Routing.Confidence != 1 {
		t.Errorf("explicit routing should be certain: %+v", res.Routing)
	}
	if strings.Contains(res.Cleaned, ">Inbox") {
		t.Errorf("the hint was not stripped: %q", res.Cleaned)
	}

	res = e.Route(router.Request{Text: "#idea we could cache the index", Now: now})
	if res.Routing.Type != model.TypeIdea {
		t.Errorf("type hint ignored: %s", res.Routing.Type)
	}
}

func TestEngineRulesBeatRanking(t *testing.T) {
	t.Parallel()
	rules := router.RuleSet{Routes: []router.Rule{{
		Keywords: []string{"trackpad"},
		Note:     "Inbox/Tasks.md",
		Section:  "Hardware",
	}}}
	e := router.NewEngine(testConfig(), rules, testNotes(), nil)

	res := e.Route(router.Request{Text: "the z13 trackpad is flaky", Now: now})
	if res.Routing.NotePath != "Inbox/Tasks.md" {
		t.Errorf("rule was not applied: %s", res.Routing.NotePath)
	}
	if res.Routing.Source != model.SourceRule {
		t.Errorf("source = %s, want rule", res.Routing.Source)
	}
	// The ranking stage still runs so alternatives remain available.
	if len(res.Routing.Candidates) == 0 {
		t.Error("no alternatives were offered")
	}
}

func TestEngineDailyRouting(t *testing.T) {
	t.Parallel()
	e := router.NewEngine(testConfig(), router.RuleSet{}, testNotes(), nil)

	res := e.Route(router.Request{Text: "finished the prototype", Now: now, Daily: true, ForceType: model.TypeJournal})
	if res.Routing.NotePath != "Daily/2026-08-05.md" {
		t.Errorf("daily path = %s", res.Routing.NotePath)
	}
	if res.Routing.Section != "Journal" {
		t.Errorf("journal section = %q, want Journal", res.Routing.Section)
	}

	res = e.Route(router.Request{Text: "buy a ups battery", Now: now, Daily: true, ForceType: model.TypeTask})
	if res.Routing.Section != "Tasks" {
		t.Errorf("task section = %q, want Tasks", res.Routing.Section)
	}

	res = e.Route(router.Request{Text: "the drive is in the drawer", Now: now, Daily: true, ForceType: model.TypeNote})
	if res.Routing.Section != "Notes" {
		t.Errorf("note section = %q, want Notes", res.Routing.Section)
	}
}

func TestEngineFallsBackByType(t *testing.T) {
	t.Parallel()
	e := router.NewEngine(testConfig(), router.RuleSet{}, nil, nil) // an empty vault

	res := e.Route(router.Request{Text: "buy a replacement ups battery", Now: now})
	if res.Routing.NotePath != "Inbox/Tasks.md" {
		t.Errorf("task fallback = %s", res.Routing.NotePath)
	}
	if res.Routing.Source != model.SourceFallback {
		t.Errorf("source = %s, want fallback", res.Routing.Source)
	}

	res = e.Route(router.Request{Text: "the spare drive is in the drawer", Now: now})
	if res.Routing.NotePath != "Inbox.md" {
		t.Errorf("note fallback = %s", res.Routing.NotePath)
	}
}

func TestEngineSuggestsHeadingAndMode(t *testing.T) {
	t.Parallel()
	e := router.NewEngine(testConfig(), router.RuleSet{}, testNotes(), nil)

	res := e.Route(router.Request{Text: "the z13 trackpad needs hid_asus patched", Now: now})
	if res.Routing.NotePath != "Projects/Linux/ROG Flow Z13.md" {
		t.Fatalf("routed to %s", res.Routing.NotePath)
	}
	if res.Routing.Section != "Trackpad troubleshooting" {
		t.Errorf("section = %q", res.Routing.Section)
	}
	if res.Routing.Mode != model.InsertUnderHeading {
		t.Errorf("mode = %s, want under_heading", res.Routing.Mode)
	}
	if len(res.Routing.Tags) == 0 {
		t.Error("no tags were suggested")
	}
}

func TestEngineNeverReturnsTheSelectionAsAnAlternative(t *testing.T) {
	t.Parallel()
	e := router.NewEngine(testConfig(), router.RuleSet{}, testNotes(), nil)
	res := e.Route(router.Request{Text: "z13 trackpad", Now: now})
	for _, c := range res.Routing.Candidates {
		if c.Note.RelPath == res.Routing.NotePath {
			t.Errorf("the selected note %s was also listed as an alternative", c.Note.RelPath)
		}
	}
}

// ------------------------------------------------------------------- helpers

func paths(cs []model.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Note.RelPath)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
