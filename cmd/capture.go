package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/tui"
)

// runCapture is the default command: capture a thought.
func runCapture(cmd *cobra.Command, args []string) error {
	text, err := readInput(args)
	if err != nil {
		return err
	}
	ctype, err := forcedType()
	if err != nil {
		return err
	}

	cfg, cfgErr := loadConfig()
	needsSetup := errors.Is(cfgErr, config.ErrNotConfigured) || strings.TrimSpace(cfg.VaultPath) == ""
	if cfgErr != nil && !errors.Is(cfgErr, config.ErrNotConfigured) {
		return cfgErr
	}

	// First run: the setup screen has to come first, and it needs a terminal.
	if needsSetup {
		if !stdoutIsTerminal() {
			return errors.New("Murmur is not set up yet. Run `murmur` in a terminal to choose your vault.")
		}
		return runTUI(nil, cfg, tui.Options{
			InitialText: text,
			SkipCapture: text != "",
			Daily:       flags.daily,
			ForceType:   ctype,
			Verbose:     flags.verbose,
		})
	}

	a, err := openApp(cfg, false)
	if err != nil {
		return err
	}
	defer func() { _ = a.Close() }()

	opts := app.PrepareOptions{Daily: flags.daily, ForceType: ctype, UseAI: useAI(cfg)}

	// Quick mode and non-interactive sessions both need a routing decision up
	// front, so prepare the draft before deciding whether to show the UI.
	if flags.quick || !stdoutIsTerminal() {
		if strings.TrimSpace(text) == "" {
			return errors.New("nothing to capture: pass a thought as an argument or pipe it in")
		}
		draft, err := a.Prepare(context.Background(), text, opts)
		if err != nil {
			return err
		}
		if canAutoSave(draft, cfg) {
			res, err := draft.Save()
			if err != nil {
				return err
			}
			fmt.Println(res.Summary())
			return nil
		}
		if !stdoutIsTerminal() {
			return notInteractiveError(draft, cfg)
		}
		// Quick mode was not confident enough: fall through to the full UI.
		fmt.Fprintln(os.Stderr, "murmur: confidence "+
			fmt.Sprintf("%.0f%%", draft.Routing.Confidence*100)+
			" is below the quick-mode threshold; opening the confirmation screen.")
	}

	return runTUI(a, cfg, tui.Options{
		InitialText: text,
		SkipCapture: text != "",
		Daily:       flags.daily,
		ForceType:   ctype,
		UseAI:       useAI(cfg),
		Verbose:     flags.verbose,
	})
}

// canAutoSave implements the quick-mode contract: write without confirmation
// only for an explicit destination or a confidence at or above the threshold.
func canAutoSave(d *app.Draft, cfg config.Config) bool {
	if !flags.quick {
		return false
	}
	if d.Hints.Path != "" || d.Hints.Project != "" || flags.daily {
		return true
	}
	return d.Routing.Confidence >= cfg.QuickModeConfidence
}

// notInteractiveError explains why a capture stopped short of writing when
// there is no terminal to confirm on. There are two distinct reasons, and
// saying the wrong one is worse than saying nothing.
func notInteractiveError(d *app.Draft, cfg config.Config) error {
	var b strings.Builder

	if !flags.quick {
		b.WriteString("nothing was written: there is no terminal here to confirm on.\n\n")
	} else {
		fmt.Fprintf(&b, "nothing was written: confidence %.0f%% is below the quick-mode threshold of %.0f%%.\n\n",
			d.Routing.Confidence*100, cfg.QuickModeConfidence*100)
	}

	fmt.Fprintf(&b, "  Suggested: %s\n", d.Destination())
	fmt.Fprintf(&b, "  Format:    %s\n", d.Routing.Type.Label())
	fmt.Fprintf(&b, "  Confidence %.0f%%\n\n", d.Routing.Confidence*100)
	b.WriteString(strings.TrimRight(d.Markdown, "\n"))
	b.WriteString("\n\n")

	if !flags.quick {
		b.WriteString("Run murmur in a terminal to confirm, or add --quick to save this automatically.")
	} else {
		b.WriteString("Run murmur in a terminal to confirm, add an explicit hint such as >Path/To/Note,\nor lower quick_mode_confidence in your config.")
	}
	return errors.New(b.String())
}

// runTUI starts the Bubble Tea program, wiring up a terminal input source even
// when standard input was a pipe.
func runTUI(a *app.App, cfg config.Config, opts tui.Options) error {
	builder := func(c config.Config) (*app.App, error) {
		return openApp(c, false)
	}
	root := tui.New(a, cfg, builder, opts)

	teaOpts := []tea.ProgramOption{tea.WithAltScreen()}
	if !stdinIsTerminal() {
		// The thought arrived on a pipe, so read keystrokes from the terminal.
		if tty, err := os.Open("/dev/tty"); err == nil {
			defer func() { _ = tty.Close() }()
			teaOpts = append(teaOpts, tea.WithInput(tty))
		} else {
			return errors.New("standard input is not a terminal, so Murmur cannot show the confirmation screen.\nUse --quick, or an explicit destination such as >Inbox.md")
		}
	}

	p := tea.NewProgram(root, teaOpts...)
	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("terminal interface failed: %w", err)
	}
	if m, ok := final.(*tui.Root); ok {
		if summary, saved := m.Result(); saved {
			fmt.Println(summary)
		}
	}
	return nil
}
