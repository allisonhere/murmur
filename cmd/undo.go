package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alliebayless/murmur/internal/app"
)

func newUndoCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Reverse the most recent write",
		Long: `Undo shows the last thing Murmur wrote and asks before reversing it.

If the note changed after Murmur wrote it, undo will not overwrite the newer
content: it either removes just the block Murmur inserted, or refuses and
explains what to do instead.`,
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

			plan, err := a.PlanUndo()
			if err != nil {
				if errors.Is(err, app.ErrNothingToUndo) {
					fmt.Println("Nothing to undo.")
					return nil
				}
				return err
			}

			printPlan(plan)

			if !yes {
				if !stdinIsTerminal() {
					return errors.New("undo needs confirmation; re-run with --yes in a non-interactive shell")
				}
				question := "Undo this write?"
				if plan.Conflict {
					question = "The note changed since. Remove only Murmur's block?"
				}
				ok, err := confirm(os.Stdout, os.Stdin, question)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Left as it is.")
					return nil
				}
			}

			if err := a.ApplyUndo(plan); err != nil {
				return err
			}
			switch plan.Strategy {
			case app.UndoDelete:
				fmt.Printf("Deleted %s\n", plan.Tx.Path)
			case app.UndoPatch:
				fmt.Printf("Removed the inserted block from %s\n", plan.Tx.Path)
			default:
				fmt.Printf("Restored %s\n", plan.Tx.Path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func printPlan(plan app.UndoPlan) {
	tx := plan.Tx
	fmt.Printf("Last write: %s\n", tx.Path)
	if tx.Section != "" {
		fmt.Printf("  Section:  ## %s\n", tx.Section)
	}
	fmt.Printf("  When:     %s\n", tx.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Action:   %s\n", plan.Detail)
	fmt.Println()
	for _, line := range strings.Split(strings.TrimRight(tx.Inserted, "\n"), "\n") {
		fmt.Println("  │ " + line)
	}
	fmt.Println()
}
