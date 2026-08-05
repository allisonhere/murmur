package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alliebayless/murmur/internal/config"
)

func TestLoadMissingFileReportsNotConfigured(t *testing.T) {
	t.Parallel()
	_, err := config.LoadFrom(filepath.Join(t.TempDir(), "config.yaml"))
	if !errors.Is(err, config.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestLoadFillsInDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("vault_path: /tmp/vault\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.VaultPath != "/tmp/vault" {
		t.Errorf("VaultPath = %q", cfg.VaultPath)
	}
	if cfg.DateFormat != "2006-01-02" || cfg.DefaultTaskNote != "Inbox/Tasks.md" {
		t.Errorf("defaults were not applied: %+v", cfg)
	}
	if cfg.QuickModeConfidence != 0.90 {
		t.Errorf("QuickModeConfidence = %v", cfg.QuickModeConfidence)
	}
	if cfg.AI.Provider != config.ProviderNone {
		t.Errorf("AI provider should default to none, got %q", cfg.AI.Provider)
	}
	if cfg.Daily.Journal != "Journal" || cfg.Daily.Tasks != "Tasks" || cfg.Daily.Notes != "Notes" {
		t.Errorf("daily sections were not defaulted: %+v", cfg.Daily)
	}
}

func TestLoadReportsBadYAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("vault_path: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadFrom(path)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.Contains(err.Error(), "Fix the YAML") {
		t.Errorf("error should tell the user what to do: %v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")

	cfg := config.Default()
	cfg.VaultPath = "/tmp/vault"
	cfg.VaultName = "Vault"
	cfg.SetPath(path)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %v, want 0600", perm)
	}

	loaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.VaultPath != cfg.VaultPath || loaded.VaultName != cfg.VaultName {
		t.Errorf("round trip lost data: %+v", loaded)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "api_key_env") {
		t.Error("the config should record the API key environment variable name")
	}
	if strings.Contains(string(data), "api_key:") {
		t.Error("the config must never contain an api_key field")
	}
}

func TestAPIKeyComesFromTheEnvironment(t *testing.T) {
	cfg := config.Default()
	cfg.AI.APIKeyEnv = "MURMUR_TEST_KEY"
	t.Setenv("MURMUR_TEST_KEY", "secret-value")

	if got := cfg.APIKey(); got != "secret-value" {
		t.Errorf("APIKey = %q", got)
	}
	cfg.AI.APIKeyEnv = ""
	if got := cfg.APIKey(); got != "" {
		t.Errorf("APIKey should be empty when no variable is named, got %q", got)
	}
}

func TestValidateVault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := config.ValidateVault(root); err != nil {
		t.Fatalf("a plain directory should be accepted: %v", err)
	}
	warning, err := config.ValidateVault(root)
	if err != nil || warning == "" {
		t.Errorf("expected a soft warning about the missing .obsidian directory, got %q / %v", warning, err)
	}

	if err := os.Mkdir(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if warning, err := config.ValidateVault(root); err != nil || warning != "" {
		t.Errorf("a real vault should validate cleanly: %q / %v", warning, err)
	}

	if _, err := config.ValidateVault(filepath.Join(root, "missing")); err == nil {
		t.Error("a missing path should be rejected")
	}
	if _, err := config.ValidateVault(""); err == nil {
		t.Error("an empty path should be rejected")
	}

	file := filepath.Join(root, "file.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ValidateVault(file); err == nil {
		t.Error("a file should be rejected as a vault")
	}
}

func TestDirHonoursXDG(t *testing.T) {
	t.Setenv("MURMUR_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/tmp/xdg", "murmur") {
		t.Errorf("Dir = %q", dir)
	}

	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("MURMUR_DATA_DIR", "")
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if data != filepath.Join("/tmp/xdg-data", "murmur") {
		t.Errorf("DataDir = %q", data)
	}
}

func TestResolvedVaultName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.VaultPath = "/home/someone/Documents/Obsidian Vault"
	if got := cfg.ResolvedVaultName(); got != "Obsidian Vault" {
		t.Errorf("ResolvedVaultName = %q", got)
	}
	cfg.VaultName = "Explicit"
	if got := cfg.ResolvedVaultName(); got != "Explicit" {
		t.Errorf("ResolvedVaultName = %q", got)
	}
}
