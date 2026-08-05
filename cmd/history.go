package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/tui"
)

func newHistoryCmd() *cobra.Command {
	var limit int
	var plain bool
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show recent captures",
		Args:  cobra.NoArgs,

		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := mustConfigure()
			if err != nil {
				return err
			}
			a, err := openApp(cfg, true)
			if err != nil {
				return err
			}
			defer func() { _ = a.Close() }()

			if !plain && stdoutIsTerminal() && stdinIsTerminal() {
				return runTUI(a, cfg, tui.Options{StartScreen: tui.ScreenHistory, Verbose: flags.verbose})
			}

			records, err := a.History(limit)
			if err != nil {
				return err
			}
			if len(records) == 0 {
				fmt.Println("No captures yet.")
				return nil
			}
			for _, rec := range records {
				printRecord(rec)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "how many captures to show")
	cmd.Flags().BoolVar(&plain, "plain", false, "print to standard output instead of opening the history screen")
	return cmd
}

func printRecord(rec model.CaptureRecord) {
	dest := rec.NotePath
	if rec.Section != "" {
		dest += " › " + rec.Section
	}
	var marks []string
	if rec.Corrected {
		marks = append(marks, "corrected")
	}
	if rec.Undone {
		marks = append(marks, "undone")
	}
	suffix := ""
	if len(marks) > 0 {
		suffix = "  [" + strings.Join(marks, ", ") + "]"
	}

	fmt.Printf("%s  %s\n", rec.CreatedAt.Format("2006-01-02 15:04"), strings.Join(strings.Fields(rec.Raw), " "))
	fmt.Printf("  → %s  (%s, %.0f%%)%s\n", dest, rec.Type.Label(), rec.Confidence*100, suffix)
}
