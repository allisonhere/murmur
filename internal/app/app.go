// Package app wires Murmur's pieces together and exposes the operations the
// CLI and the TUI both need: prepare a capture, save it, undo it, list history.
package app

import (
	"log"
	"os"
	"time"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/database"
	"github.com/alliebayless/murmur/internal/indexer"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/obsidian"
	"github.com/alliebayless/murmur/internal/provider"
	"github.com/alliebayless/murmur/internal/router"
	"github.com/alliebayless/murmur/internal/storage"
)

// Options control how the application is opened.
type Options struct {
	Verbose bool
	// SkipIndex avoids the startup freshness scan (used by `murmur undo` and
	// `murmur history`, which do not need the index).
	SkipIndex bool
	// ConfigPath overrides the configuration file location.
	ConfigPath string
	// DBPath overrides the database location.
	DBPath string
}

// App is a fully wired Murmur instance.
type App struct {
	Cfg    config.Config
	DB     *database.DB
	Repo   *database.Repo
	Vault  *storage.Vault
	Engine *router.Engine
	Index  *indexer.Indexer

	classifier provider.Classifier
	logger     *log.Logger
	verbose    bool
	indexStats indexer.Stats
}

// Open loads the config, opens the database and vault, refreshes the index and
// builds the routing engine.
func Open(cfg config.Config, opts Options) (*App, error) {
	a := &App{Cfg: cfg, verbose: opts.Verbose}
	a.logger = log.New(os.Stderr, "murmur: ", 0)

	vault, err := storage.NewVault(cfg.VaultPath)
	if err != nil {
		return nil, err
	}
	a.Vault = vault

	dbPath := opts.DBPath
	if dbPath == "" {
		dir, err := config.DataDir()
		if err != nil {
			return nil, err
		}
		dbPath = database.DefaultPath(dir)
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return nil, err
	}
	a.DB = db
	a.Repo = database.NewRepo(db)
	a.Index = indexer.New(vault, a.Repo, cfg)
	a.Debugf("database %s", dbPath)

	if !opts.SkipIndex {
		stats, err := a.Index.EnsureFresh()
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		a.indexStats = stats
		a.Debugf("index: %d scanned, %d updated, %d removed in %s",
			stats.Scanned, stats.Indexed, stats.Removed, stats.Duration.Round(time.Millisecond))
		for _, w := range stats.Warnings {
			a.Debugf("warning: %s", w)
		}
	}

	if err := a.reloadEngine(); err != nil {
		_ = db.Close()
		return nil, err
	}

	cl, err := provider.New(cfg)
	if err != nil {
		// A misconfigured provider must never stop Murmur from capturing.
		a.Debugf("AI provider disabled: %v", err)
		cl = provider.None{}
	}
	a.classifier = cl
	a.Debugf("AI provider: %s", cl.Name())

	return a, nil
}

func (a *App) reloadEngine() error {
	notes, err := a.Repo.Notes()
	if err != nil {
		return err
	}
	rulesPath, err := config.RulesPath()
	if err != nil {
		return err
	}
	rules, err := router.LoadRules(rulesPath)
	if err != nil {
		// Broken rules are a warning, not a fatal error.
		a.Warnf("%v", err)
		rules = router.RuleSet{}
	}
	a.Engine = router.NewEngine(a.Cfg, rules, notes, learnerAdapter{a.Repo})
	a.Debugf("routing over %d indexed notes, %d rules", len(notes), len(rules.Routes))
	return nil
}

// Reindex forces a scan and rebuilds the routing engine.
func (a *App) Reindex(rebuild bool) (indexer.Stats, error) {
	stats, err := a.Index.Run(rebuild)
	if err != nil {
		return stats, err
	}
	a.indexStats = stats
	return stats, a.reloadEngine()
}

// IndexStats returns the statistics of the most recent scan.
func (a *App) IndexStats() indexer.Stats { return a.indexStats }

// ProviderName reports the active AI provider.
func (a *App) ProviderName() string {
	if a.classifier == nil {
		return "none"
	}
	return a.classifier.Name()
}

// Close releases the database handle.
func (a *App) Close() error {
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}

// Debugf logs only in verbose mode.
func (a *App) Debugf(format string, args ...any) {
	if a.verbose {
		a.logger.Printf(format, args...)
	}
}

// Warnf always logs, but stays on stderr so it never corrupts piped output.
func (a *App) Warnf(format string, args ...any) {
	a.logger.Printf("warning: "+format, args...)
}

// learnerAdapter bridges the database's learned-route rows to the router's
// interface, keeping the router free of any database dependency.
type learnerAdapter struct{ repo *database.Repo }

func (l learnerAdapter) LearnedRoutes(tokens []string) ([]router.LearnedRoute, error) {
	rows, err := l.repo.LearnedRoutes(tokens)
	if err != nil {
		return nil, err
	}
	out := make([]router.LearnedRoute, 0, len(rows))
	for _, r := range rows {
		out = append(out, router.LearnedRoute{
			NotePath: r.NotePath,
			Section:  r.Section,
			Type:     r.Type,
			Weight:   r.Weight,
		})
	}
	return out, nil
}

// History returns recent captures.
func (a *App) History(limit int) ([]model.CaptureRecord, error) {
	return a.Repo.Captures(limit)
}

// ResetLearning clears the learned routing table.
func (a *App) ResetLearning() (int64, error) {
	return a.Repo.ResetLearning()
}

// ObsidianURI builds a link to a note in the configured vault.
func (a *App) ObsidianURI(rel string) string {
	return obsidian.URI(a.Cfg.ResolvedVaultName(), rel)
}
