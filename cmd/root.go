// Package cmd implements Murmur's command line interface.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

// Version is Murmur's release version, overridable at build time with
// -ldflags "-X github.com/alliebayless/murmur/cmd.Version=...".
var Version = "0.1.0"

type globalFlags struct {
	verbose    bool
	quick      bool
	daily      bool
	typeName   string
	noAI       bool
	configPath string
	dbPath     string
}

var flags globalFlags

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "murmur [thought]",
		Short: "Capture a thought straight into your Obsidian vault",
		Long: `Murmur captures a rough thought, works out where it belongs in your
Obsidian vault, formats it as clean Markdown and shows you the result before
anything is written.

  murmur                                 open the capture window
  murmur "Add barcode scanning"          capture from an argument
  echo "Research suspend bug" | murmur   capture from a pipe
  murmur --daily "Fixed the trackpad"    append to today's daily note
  murmur --quick "Buy a UPS battery"     save without confirmation when confident`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapture(cmd, args)
		},
	}

	pf := root.PersistentFlags()
	pf.BoolVarP(&flags.verbose, "verbose", "v", false, "log what Murmur is doing to stderr")
	pf.StringVar(&flags.configPath, "config", "", "path to config.yaml")
	pf.StringVar(&flags.dbPath, "database", "", "path to the Murmur database")

	f := root.Flags()
	f.BoolVarP(&flags.quick, "quick", "q", false, "save without confirmation when routing is confident")
	f.BoolVarP(&flags.daily, "daily", "d", false, "route to today's daily note")
	f.StringVarP(&flags.typeName, "type", "t", "", "force a content type (task, idea, journal, project, reference, question, bookmark, note)")
	f.BoolVar(&flags.noAI, "no-ai", false, "skip the AI provider for this capture")

	root.AddCommand(newIndexCmd(), newUndoCmd(), newHistoryCmd(), newLearningCmd(), newVersionCmd())
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		if errors.Is(err, errQuietExit) {
			return 1
		}
		fmt.Fprintln(os.Stderr, "murmur: "+err.Error())
		return 1
	}
	return 0
}

// errQuietExit signals a failure that has already been reported to the user.
var errQuietExit = errors.New("exit")

// loadConfig reads the configuration, returning ErrNotConfigured when the user
// has not been through setup yet.
func loadConfig() (config.Config, error) {
	if flags.configPath != "" {
		return config.LoadFrom(flags.configPath)
	}
	return config.Load()
}

// openApp opens a fully wired application.
func openApp(cfg config.Config, skipIndex bool) (*app.App, error) {
	return app.Open(cfg, app.Options{
		Verbose:   flags.verbose,
		SkipIndex: skipIndex,
		DBPath:    flags.dbPath,
	})
}

// mustConfigure loads the config or explains how to get set up.
func mustConfigure() (config.Config, error) {
	cfg, err := loadConfig()
	if errors.Is(err, config.ErrNotConfigured) {
		return cfg, errors.New("Murmur is not set up yet. Run `murmur` with no arguments to choose your vault.")
	}
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.VaultPath) == "" {
		return cfg, errors.New("no vault_path in the config. Run `murmur` with no arguments to choose your vault.")
	}
	return cfg, nil
}

// readInput assembles the thought from arguments and standard input.
func readInput(args []string) (string, error) {
	var parts []string
	if piped, err := readPipedStdin(); err != nil {
		return "", err
	} else if piped != "" {
		parts = append(parts, piped)
	}
	if joined := strings.TrimSpace(strings.Join(args, " ")); joined != "" {
		parts = append(parts, joined)
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func readPipedStdin() (string, error) {
	if stdinIsTerminal() {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, os.ErrClosed) {
			return "", nil
		}
		// A broken pipe means the writer went away; treat it as no input
		// rather than a crash.
		if strings.Contains(err.Error(), "broken pipe") {
			return "", nil
		}
		return "", fmt.Errorf("read standard input: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func stdinIsTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

func stdoutIsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// forcedType resolves the --type flag.
func forcedType() (model.ContentType, error) {
	if flags.typeName == "" {
		if flags.daily {
			return "", nil
		}
		return "", nil
	}
	t, ok := model.ParseContentType(flags.typeName)
	if !ok {
		return "", fmt.Errorf("unknown content type %q; use one of task, idea, journal, project, reference, question, bookmark, note", flags.typeName)
	}
	return t, nil
}

// useAI reports whether the AI stage should run for this invocation.
func useAI(cfg config.Config) bool {
	if flags.noAI {
		return false
	}
	return cfg.AI.Provider != "" && cfg.AI.Provider != config.ProviderNone
}

// confirm asks a yes/no question on the terminal.
func confirm(w io.Writer, in io.Reader, question string) (bool, error) {
	fmt.Fprintf(w, "%s [y/N] ", question)
	buf := make([]byte, 8)
	n, err := in.Read(buf)
	if err != nil && n == 0 {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(w)
			return false, nil
		}
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	return answer == "y" || answer == "yes", nil
}
