package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/alliebayless/murmur/internal/config"
)

// setupModel is the first-run screen that locates the Obsidian vault.
type setupModel struct {
	st      Styles
	cfg     config.Config
	input   textinput.Model
	warning string
	guesses []string
}

func newSetupModel(st Styles, cfg config.Config) setupModel {
	ti := textinput.New()
	ti.Prompt = st.Accent.Render("› ")
	ti.Placeholder = "/home/you/Documents/Obsidian Vault"
	ti.CharLimit = 500
	ti.Width = 50

	guesses := guessVaults()
	if cfg.VaultPath != "" {
		ti.SetValue(cfg.VaultPath)
	} else if len(guesses) > 0 {
		ti.SetValue(guesses[0])
	}
	ti.CursorEnd()

	return setupModel{st: st, cfg: cfg, input: ti, guesses: guesses}
}

func (m *setupModel) focus() tea.Cmd { return m.input.Focus() }

func (m *setupModel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// commit validates the entered path and writes the configuration file.
func (m *setupModel) commit() (config.Config, error) {
	path := strings.TrimSpace(m.input.Value())
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	warning, err := config.ValidateVault(path)
	if err != nil {
		return m.cfg, err
	}
	m.warning = warning

	cfg := m.cfg
	cfg.VaultPath = path
	if cfg.VaultName == "" {
		cfg.VaultName = filepath.Base(path)
	}
	if err := cfg.Save(); err != nil {
		return cfg, err
	}
	m.cfg = cfg
	return cfg, nil
}

func (m *setupModel) view(width int) string {
	inner := width - 4
	lines := []string{
		"",
		"  " + m.st.Prompt.Render("Welcome to Murmur."),
		"",
	}
	for _, l := range wrapPlain("Murmur captures rough thoughts straight into your Obsidian vault. Point it at your vault to get started — nothing is written until you confirm it.", inner-4) {
		lines = append(lines, "  "+m.st.Muted.Render(l))
	}
	lines = append(lines,
		"",
		"  "+m.st.Label.Render("Vault path"),
		"  "+m.input.View(),
		"",
	)

	if len(m.guesses) > 1 {
		lines = append(lines, "  "+m.st.Faint.Render("Also found:"))
		for _, g := range m.guesses[1:] {
			lines = append(lines, "    "+m.st.Faint.Render(truncPlain(g, inner-6)))
		}
		lines = append(lines, "")
	}
	if m.warning != "" {
		lines = append(lines, "  "+m.st.Warn.Render(truncPlain(m.warning, inner-4)), "")
	}

	cfgPath, _ := config.Path()
	lines = append(lines, "  "+m.st.Faint.Render("Settings will be saved to "+truncPlain(cfgPath, inner-30)), "")

	return m.st.Card(width, "Setup", []Section{{Lines: lines}})
}

// guessVaults looks in the usual places for something that smells like an
// Obsidian vault, so most users can just press Enter.
func guessVaults() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Notes"),
		home,
		filepath.Join(home, "Dropbox"),
		filepath.Join(home, "Sync"),
	}
	var found []string
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			candidate := filepath.Join(root, e.Name())
			if seen[candidate] {
				continue
			}
			if _, err := os.Stat(filepath.Join(candidate, ".obsidian")); err == nil {
				seen[candidate] = true
				found = append(found, candidate)
			}
		}
		if _, err := os.Stat(filepath.Join(root, ".obsidian")); err == nil && !seen[root] {
			seen[root] = true
			found = append(found, root)
		}
		if len(found) >= 5 {
			break
		}
	}
	return found
}
