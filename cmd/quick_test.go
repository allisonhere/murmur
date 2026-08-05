package cmd

import (
	"testing"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// draftWith builds the minimum Draft that canAutoSave inspects.
func draftWith(conf float64, hints model.Hints) *app.Draft {
	d := &app.Draft{}
	d.Routing = model.Routing{Confidence: conf}
	d.Hints = hints
	return d
}

func TestCanAutoSave(t *testing.T) {
	cfg := config.Default() // quick_mode_confidence: 0.90

	tests := []struct {
		name  string
		quick bool
		daily bool
		draft *app.Draft
		want  bool
	}{
		{
			name:  "without --quick nothing auto-saves",
			quick: false,
			draft: draftWith(1.0, model.Hints{Path: "Inbox.md"}),
			want:  false,
		},
		{
			name:  "confident enough",
			quick: true,
			draft: draftWith(0.95, model.Hints{}),
			want:  true,
		},
		{
			name:  "exactly at the threshold",
			quick: true,
			draft: draftWith(0.90, model.Hints{}),
			want:  true,
		},
		{
			name:  "just below the threshold",
			quick: true,
			draft: draftWith(0.89, model.Hints{}),
			want:  false,
		},
		{
			name:  "explicit destination overrides low confidence",
			quick: true,
			draft: draftWith(0.10, model.Hints{Path: "Inbox/Tasks.md"}),
			want:  true,
		},
		{
			name:  "project hint overrides low confidence",
			quick: true,
			draft: draftWith(0.10, model.Hints{Project: "tidemail"}),
			want:  true,
		},
		{
			name:  "--daily is an explicit destination",
			quick: true,
			daily: true,
			draft: draftWith(0.10, model.Hints{}),
			want:  true,
		},
		{
			name:  "no signal at all",
			quick: true,
			draft: draftWith(0, model.Hints{}),
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := flags
			t.Cleanup(func() { flags = saved })

			flags.quick = tc.quick
			flags.daily = tc.daily

			if got := canAutoSave(tc.draft, cfg); got != tc.want {
				t.Errorf("canAutoSave = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanAutoSaveHonoursConfiguredThreshold(t *testing.T) {
	saved := flags
	t.Cleanup(func() { flags = saved })
	flags.quick = true
	flags.daily = false

	d := draftWith(0.5, model.Hints{})

	strict := config.Default()
	strict.QuickModeConfidence = 0.9
	if canAutoSave(d, strict) {
		t.Error("0.5 should not pass a 0.9 threshold")
	}

	relaxed := config.Default()
	relaxed.QuickModeConfidence = 0.4
	if !canAutoSave(d, relaxed) {
		t.Error("0.5 should pass a 0.4 threshold")
	}
}

func TestForcedTypeParsing(t *testing.T) {
	saved := flags
	t.Cleanup(func() { flags = saved })

	flags.typeName = ""
	if got, err := forcedType(); err != nil || got != "" {
		t.Errorf("empty flag = %q / %v", got, err)
	}

	flags.typeName = "todo"
	got, err := forcedType()
	if err != nil {
		t.Fatalf("forcedType: %v", err)
	}
	if got != model.TypeTask {
		t.Errorf("got %q, want task", got)
	}

	flags.typeName = "haiku"
	if _, err := forcedType(); err == nil {
		t.Error("expected an error for an unknown type")
	}
}

func TestUseAI(t *testing.T) {
	saved := flags
	t.Cleanup(func() { flags = saved })

	cfg := config.Default() // provider: none
	flags.noAI = false
	if useAI(cfg) {
		t.Error("the default configuration must not call out to an AI provider")
	}

	cfg.AI.Provider = "ollama"
	if !useAI(cfg) {
		t.Error("a configured provider should be used")
	}

	flags.noAI = true
	if useAI(cfg) {
		t.Error("--no-ai must win")
	}
}

func TestReadInputJoinsArguments(t *testing.T) {
	got, err := readInput([]string{"add", "barcode", "scanning"})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	// Standard input is a terminal (or empty) under `go test`, so only the
	// arguments contribute.
	if got != "add barcode scanning" {
		t.Errorf("readInput = %q", got)
	}
}
