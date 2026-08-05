package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newLearningCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learning",
		Short: "Inspect or clear what Murmur has learned from your corrections",
		Long: `Murmur keeps a small weighted table of the words you have routed to each
note. It is not a model and never leaves your machine; it simply nudges ranking
towards the destinations you have chosen before.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var yes bool
	reset := &cobra.Command{
		Use:           "reset",
		Short:         "Forget every learned routing association",
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

			if !yes {
				if !stdinIsTerminal() {
					return errors.New("learning reset needs confirmation; re-run with --yes")
				}
				ok, err := confirm(os.Stdout, os.Stdin, "Forget all learned routing history?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Kept.")
					return nil
				}
			}

			n, err := a.ResetLearning()
			if err != nil {
				return err
			}
			fmt.Printf("Cleared %d learned associations.\n", n)
			return nil
		},
	}
	reset.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	cmd.AddCommand(reset)
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Murmur version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("murmur %s\n", Version)
		},
	}
}
