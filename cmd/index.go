package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	var rebuild bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Scan the vault and update Murmur's index",
		Long: `Murmur keeps a small index of your notes: paths, titles, aliases, tags,
headings and a short excerpt for keyword matching. The index refreshes itself
whenever you capture, so running this by hand is only needed after large
changes to the vault.`,
		Args:          cobra.NoArgs,
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

			stats, err := a.Reindex(rebuild)
			if err != nil {
				return err
			}

			verb := "updated"
			if rebuild {
				verb = "rebuilt"
			}
			fmt.Printf("Index %s in %s\n", verb, stats.Duration.Round(time.Millisecond))
			fmt.Printf("  %d notes scanned, %d indexed, %d unchanged, %d removed\n",
				stats.Scanned, stats.Indexed, stats.Skipped, stats.Removed)
			for _, w := range stats.Warnings {
				fmt.Printf("  warning: %s\n", w)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "discard the existing index and re-parse every note")
	return cmd
}
