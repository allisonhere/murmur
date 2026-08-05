package formatter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/alliebayless/murmur/internal/formatter"
	"github.com/alliebayless/murmur/internal/model"
)

var testDate = time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC)

func opts() formatter.Options {
	o := formatter.Defaults()
	o.Now = testDate
	return o
}

func TestFormatByContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		ctype model.ContentType
		want  string
	}{
		{
			name:  "task strips filler and dates the item",
			input: "remember to update forgejo",
			ctype: model.TypeTask,
			want:  "- [ ] Update forgejo\n  - Added: 2026-08-05",
		},
		{
			name:  "idea becomes a callout",
			input: "maybe tidemail should support newsletters",
			ctype: model.TypeIdea,
			want:  "> [!idea]\n> Tidemail should support newsletters.",
		},
		{
			name:  "journal is a plain past-tense bullet",
			input: "today i finally fixed the z13 trackpad",
			ctype: model.TypeJournal,
			want:  "- Fixed the z13 trackpad.",
		},
		{
			name:  "question becomes a research task",
			input: "why does nvidia resume fail on fedora",
			ctype: model.TypeQuestion,
			want:  "- [ ] Research: why does nvidia resume fail on fedora?\n  - Added: 2026-08-05",
		},
		{
			name:  "bookmark becomes a link",
			input: "https://example.com useful article about bubble tea architecture",
			ctype: model.TypeBookmark,
			want:  "- [Useful article about bubble tea architecture](https://example.com)",
		},
		{
			name:  "plain note is a bullet",
			input: "the spare drive is in the left drawer",
			ctype: model.TypeNote,
			want:  "- The spare drive is in the left drawer.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatter.Format(tc.input, tc.ctype, opts())
			if got != tc.want {
				t.Errorf("Format() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestFormatRespectsConfiguration(t *testing.T) {
	t.Parallel()

	o := opts()
	o.IncludeDate = false
	if got := formatter.Format("buy a ups battery", model.TypeTask, o); strings.Contains(got, "Added") {
		t.Errorf("date was added despite include_capture_date=false: %q", got)
	}

	o = opts()
	o.UseCallouts = false
	got := formatter.Format("maybe add newsletters", model.TypeIdea, o)
	if strings.Contains(got, "[!idea]") {
		t.Errorf("callout used despite use_callouts_for_ideas=false: %q", got)
	}
	if !strings.HasPrefix(got, "- ") {
		t.Errorf("expected a bullet fallback, got %q", got)
	}

	o = opts()
	o.TaskDateProperty = "Created"
	if got := formatter.Format("fix the thing", model.TypeTask, o); !strings.Contains(got, "- Created: 2026-08-05") {
		t.Errorf("task_date_property ignored: %q", got)
	}

	o = opts()
	o.AppendTags = true
	o.Tags = []string{"linux", "z13"}
	if got := formatter.Format("fix the trackpad", model.TypeTask, o); !strings.Contains(got, "#linux #z13") {
		t.Errorf("tags were not appended: %q", got)
	}
}

func TestFormatUsesVaultCasing(t *testing.T) {
	t.Parallel()

	o := opts()
	o.Caser = formatter.NewTermCaser([]string{"Forgejo", "NVIDIA", "Fedora", "ROG Flow Z13", "tidemail"})

	got := formatter.Format("remember to update forgejo", model.TypeTask, o)
	if !strings.Contains(got, "Update Forgejo") {
		t.Errorf("vault casing not applied: %q", got)
	}

	got = formatter.Format("why does nvidia resume fail on fedora", model.TypeQuestion, o)
	if !strings.Contains(got, "NVIDIA") || !strings.Contains(got, "Fedora") {
		t.Errorf("vault casing not applied to a question: %q", got)
	}

	// An all-lower-case vocabulary entry teaches nothing and must not match.
	if got := formatter.Format("open tidemail", model.TypeNote, o); !strings.Contains(got, "tidemail") {
		t.Errorf("lower-case vocabulary should be ignored: %q", got)
	}
}

func TestTermCaserRespectsUserCasing(t *testing.T) {
	t.Parallel()
	tc := formatter.NewTermCaser([]string{"NVIDIA"})
	if got := tc.Apply("Nvidia is fine"); got != "Nvidia is fine" {
		t.Errorf("user casing was overwritten: %q", got)
	}
	if got := tc.Apply("nvidia driver"); got != "NVIDIA driver" {
		t.Errorf("lower-case term not corrected: %q", got)
	}
	if formatter.NewTermCaser(nil).Size() != 0 {
		t.Error("empty vocabulary should produce an empty caser")
	}
}

func TestFormatEmptyInput(t *testing.T) {
	t.Parallel()
	if got := formatter.Format("   ", model.TypeTask, opts()); got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestFormatMultilineProjectNote(t *testing.T) {
	t.Parallel()
	got := formatter.Format("first paragraph here\n\nsecond paragraph here", model.TypeProject, opts())
	if !strings.Contains(got, "First paragraph here.\n\nSecond paragraph here.") {
		t.Errorf("paragraphs were not preserved: %q", got)
	}
	if !strings.Contains(got, "*Captured 2026-08-05*") {
		t.Errorf("capture date missing: %q", got)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  model.ContentType
	}{
		{"remember to update forgejo", model.TypeTask},
		{"buy a replacement ups battery", model.TypeTask},
		{"maybe tidemail should support newsletters", model.TypeIdea},
		{"what if we cached the index", model.TypeIdea},
		{"today i finally fixed the z13 trackpad", model.TypeJournal},
		{"yesterday i shipped the prototype", model.TypeJournal},
		{"why does nvidia resume fail on fedora", model.TypeQuestion},
		{"is the battery threshold persistent?", model.TypeQuestion},
		{"https://example.com nice article", model.TypeBookmark},
		{"the drive is in the drawer", model.TypeNote},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := formatter.Classify(tc.input)
			if got.Type != tc.want {
				t.Errorf("Classify(%q) = %s (%s), want %s", tc.input, got.Type, got.Reason, tc.want)
			}
			if got.Confidence <= 0 || got.Confidence > 1 {
				t.Errorf("confidence out of range: %v", got.Confidence)
			}
		})
	}
}

func TestClassifyPrecedence(t *testing.T) {
	t.Parallel()
	// "fix" is a task cue but "today ... fixed" is a journal entry: the
	// earlier rule must win so behaviour stays predictable.
	if got := formatter.Classify("today i fixed the trackpad"); got.Type != model.TypeJournal {
		t.Errorf("got %s, want journal", got.Type)
	}
	// A URL beats everything else.
	if got := formatter.Classify("remember to read https://example.com"); got.Type != model.TypeBookmark {
		t.Errorf("got %s, want bookmark", got.Type)
	}
}

func TestStripFiller(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"remember to update forgejo": "update forgejo",
		"note to self: call the vet": "call the vet",
		"todo: renew the domain":     "renew the domain",
		"today i fixed it":           "fixed it",
		"todos are not filler":       "todos are not filler",
		"just do it":                 "do it",
	}
	for in, want := range tests {
		if got := formatter.StripFiller(in); got != want {
			t.Errorf("StripFiller(%q) = %q, want %q", in, got, want)
		}
	}
}
