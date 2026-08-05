// Package config loads and saves Murmur's YAML configuration from the XDG
// configuration directory. Secrets are never stored here: the AI section names
// an environment variable to read the API key from instead.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the user's configuration file.
type Config struct {
	VaultPath string `yaml:"vault_path"`
	// VaultName is used to build obsidian:// URIs. When empty the base name of
	// VaultPath is used.
	VaultName string `yaml:"vault_name,omitempty"`

	DefaultInbox    string `yaml:"default_inbox"`
	DefaultTaskNote string `yaml:"default_task_note"`
	DailyNotePath   string `yaml:"daily_note_path"`
	// DailyTemplatePath is an optional vault-relative path to a template used
	// when a daily note has to be created.
	DailyTemplatePath string `yaml:"daily_template_path,omitempty"`

	DateFormat string `yaml:"date_format"`
	TimeFormat string `yaml:"time_format"`

	QuickModeConfidence float64 `yaml:"quick_mode_confidence"`

	ExcludedPaths []string `yaml:"excluded_paths"`

	AI         AIConfig         `yaml:"ai"`
	Formatting FormattingConfig `yaml:"formatting"`
	Daily      DailySections    `yaml:"daily_sections"`

	// path is where this config was loaded from; not serialised.
	path string `yaml:"-"`
}

// AIConfig configures the optional classification provider.
type AIConfig struct {
	Provider  string `yaml:"provider"` // none | ollama | openai
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	// TimeoutSeconds bounds a single classification request.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`
}

// FormattingConfig controls how thoughts are rendered to Markdown.
type FormattingConfig struct {
	IncludeCaptureDate bool   `yaml:"include_capture_date"`
	UseCalloutsForIdea bool   `yaml:"use_callouts_for_ideas"`
	TaskDateProperty   string `yaml:"task_date_property"`
}

// DailySections maps content types onto headings inside the daily note.
type DailySections struct {
	Journal string `yaml:"journal"`
	Tasks   string `yaml:"tasks"`
	Notes   string `yaml:"notes"`
}

// Provider names.
const (
	ProviderNone   = "none"
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

// Default returns a configuration with sensible values and no vault set.
func Default() Config {
	return Config{
		DefaultInbox:        "Inbox.md",
		DefaultTaskNote:     "Inbox/Tasks.md",
		DailyNotePath:       "Daily/{{date}}.md",
		DateFormat:          "2006-01-02",
		TimeFormat:          "15:04",
		QuickModeConfidence: 0.90,
		ExcludedPaths:       []string{".obsidian", ".git", ".trash", "Templates", "Attachments"},
		AI: AIConfig{
			Provider:       ProviderNone,
			APIKeyEnv:      "MURMUR_API_KEY",
			TimeoutSeconds: 20,
		},
		Formatting: FormattingConfig{
			IncludeCaptureDate: true,
			UseCalloutsForIdea: true,
			TaskDateProperty:   "Added",
		},
		Daily: DailySections{
			Journal: "Journal",
			Tasks:   "Tasks",
			Notes:   "Notes",
		},
	}
}

// Dir returns Murmur's configuration directory, honouring XDG_CONFIG_HOME.
func Dir() (string, error) {
	if v := os.Getenv("MURMUR_CONFIG_DIR"); v != "" {
		return v, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "murmur"), nil
}

// DataDir returns the directory holding the SQLite database, honouring
// XDG_DATA_HOME.
func DataDir() (string, error) {
	if v := os.Getenv("MURMUR_DATA_DIR"); v != "" {
		return v, nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "murmur"), nil
}

// Path returns the full path of the configuration file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// RulesPath returns the full path of the deterministic routing rules file.
func RulesPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "routes.yaml"), nil
}

// ErrNotConfigured is returned by Load when no configuration file exists yet.
var ErrNotConfigured = errors.New("murmur is not configured yet")

// Load reads the configuration file, filling in defaults for absent fields.
// It returns ErrNotConfigured when the file does not exist so callers can start
// the first-run setup flow.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(path)
}

// LoadFrom reads a configuration file from an explicit path.
func LoadFrom(path string) (Config, error) {
	cfg := Default()
	cfg.path = path

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, ErrNotConfigured
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w\n\nFix the YAML syntax or delete the file to start over.", path, err)
	}
	cfg.path = path
	cfg.normalise()
	return cfg, nil
}

func (c *Config) normalise() {
	d := Default()
	if c.DateFormat == "" {
		c.DateFormat = d.DateFormat
	}
	if c.TimeFormat == "" {
		c.TimeFormat = d.TimeFormat
	}
	if c.DefaultInbox == "" {
		c.DefaultInbox = d.DefaultInbox
	}
	if c.DefaultTaskNote == "" {
		c.DefaultTaskNote = d.DefaultTaskNote
	}
	if c.DailyNotePath == "" {
		c.DailyNotePath = d.DailyNotePath
	}
	if c.QuickModeConfidence <= 0 {
		c.QuickModeConfidence = d.QuickModeConfidence
	}
	if c.AI.Provider == "" {
		c.AI.Provider = ProviderNone
	}
	if c.AI.TimeoutSeconds <= 0 {
		c.AI.TimeoutSeconds = d.AI.TimeoutSeconds
	}
	if c.Formatting.TaskDateProperty == "" {
		c.Formatting.TaskDateProperty = d.Formatting.TaskDateProperty
	}
	if c.Daily.Journal == "" {
		c.Daily.Journal = d.Daily.Journal
	}
	if c.Daily.Tasks == "" {
		c.Daily.Tasks = d.Daily.Tasks
	}
	if c.Daily.Notes == "" {
		c.Daily.Notes = d.Daily.Notes
	}
	c.VaultPath = expandHome(c.VaultPath)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// Save writes the configuration back to disk, creating the directory when
// needed. The file is written with 0600 permissions because it records the
// user's vault location.
func (c Config) Save() error {
	path := c.path
	if path == "" {
		p, err := Path()
		if err != nil {
			return err
		}
		path = p
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	header := "# Murmur configuration.\n" +
		"# Secrets are never stored here: set the environment variable named by\n" +
		"# ai.api_key_env instead.\n"
	if err := os.WriteFile(path, append([]byte(header), data...), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// SetPath overrides where Save will write.
func (c *Config) SetPath(p string) { c.path = p }

// FilePath reports where this configuration was loaded from.
func (c Config) FilePath() string { return c.path }

// APIKey resolves the provider API key from the environment. It returns an
// empty string when no key is configured, which is not an error: providers that
// do not need one keep working.
func (c Config) APIKey() string {
	if c.AI.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.AI.APIKeyEnv)
}

// ResolvedVaultName returns the vault name used for obsidian:// URIs.
func (c Config) ResolvedVaultName() string {
	if c.VaultName != "" {
		return c.VaultName
	}
	return filepath.Base(strings.TrimRight(c.VaultPath, string(filepath.Separator)))
}

// ValidateVault checks that a path exists, is a directory, and plausibly holds
// an Obsidian vault. A missing .obsidian directory is reported as a soft
// warning rather than an error so that fresh vaults still work.
func ValidateVault(path string) (warning string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("vault path is empty")
	}
	path = expandHome(path)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("vault path %s does not exist", path)
	}
	if err != nil {
		return "", fmt.Errorf("check vault path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("vault path %s is not a directory", path)
	}
	if _, err := os.Stat(filepath.Join(path, ".obsidian")); err != nil {
		return fmt.Sprintf("%s has no .obsidian directory; it may not be an Obsidian vault", path), nil
	}
	return "", nil
}

// Writable reports whether the vault directory can be written to.
func (c Config) Writable() error {
	probe := filepath.Join(c.VaultPath, ".murmur-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("vault %s is not writable: %w", c.VaultPath, err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}
