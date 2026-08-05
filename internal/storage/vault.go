// Package storage provides safe, atomic access to files inside the vault. Every
// path that reaches the filesystem goes through Resolve first, which is the
// single place where "never write outside the vault" is enforced.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vault is a rooted view of the user's Obsidian vault.
type Vault struct {
	root string
	// realRoot is the symlink-resolved root, used for containment checks.
	realRoot string
}

// ErrOutsideVault is returned for any path that escapes the vault root.
var ErrOutsideVault = errors.New("path is outside the vault")

// NewVault opens a vault rooted at dir.
func NewVault(dir string) (*Vault, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("vault path is empty; run `murmur` once to configure it")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve vault path: %w", err)
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("vault %s does not exist; update vault_path in your config", abs)
	}
	if err != nil {
		return nil, fmt.Errorf("open vault: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault %s is not a directory", abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	return &Vault{root: abs, realRoot: real}, nil
}

// Root returns the absolute vault root.
func (v *Vault) Root() string { return v.root }

// Resolve converts a vault-relative path into an absolute one, rejecting
// absolute paths, parent traversal, and symlinks that point out of the vault.
func (v *Vault) Resolve(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("empty destination path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "~") {
		// Absolute paths are only accepted when they already live in the vault.
		abs := filepath.Clean(rel)
		if !v.contains(abs) {
			return "", fmt.Errorf("%w: %s", ErrOutsideVault, rel)
		}
		return abs, nil
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: path contains a null byte", ErrOutsideVault)
	}

	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrOutsideVault, rel)
	}
	abs := filepath.Join(v.root, clean)
	if !v.contains(abs) {
		return "", fmt.Errorf("%w: %s", ErrOutsideVault, rel)
	}
	// If the file (or its nearest existing parent) resolves through a symlink
	// out of the vault, reject it too.
	if real, err := resolveExisting(abs); err == nil && !v.containsReal(real) {
		return "", fmt.Errorf("%w: %s resolves outside the vault", ErrOutsideVault, rel)
	}
	return abs, nil
}

func resolveExisting(p string) (string, error) {
	cur := p
	var suffix []string
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{real}, suffix...)...), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", errors.New("no existing ancestor")
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		cur = parent
	}
}

func (v *Vault) contains(abs string) bool {
	rel, err := filepath.Rel(v.root, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (v *Vault) containsReal(abs string) bool {
	rel, err := filepath.Rel(v.realRoot, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Rel converts an absolute path into a slash-separated vault-relative path.
func (v *Vault) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(v.root, abs)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%w: %s", ErrOutsideVault, abs)
	}
	return filepath.ToSlash(rel), nil
}

// FileState is a snapshot of a vault file at a point in time.
type FileState struct {
	Rel     string
	Abs     string
	Exists  bool
	Content string
	Hash    string
	Mode    os.FileMode
}

// Read loads a vault file. A missing file is not an error: Exists is false and
// Content is empty, which is what "create the note" needs.
func (v *Vault) Read(rel string) (FileState, error) {
	abs, err := v.Resolve(rel)
	if err != nil {
		return FileState{}, err
	}
	st := FileState{Rel: filepath.ToSlash(filepath.Clean(rel)), Abs: abs, Mode: 0o644}
	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return st, nil
	case err != nil:
		return st, fmt.Errorf("stat %s: %w", rel, err)
	case info.IsDir():
		return st, fmt.Errorf("%s is a directory, not a note", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return st, fmt.Errorf("cannot read %s: permission denied", rel)
		}
		return st, fmt.Errorf("read %s: %w", rel, err)
	}
	st.Exists = true
	st.Content = string(data)
	st.Hash = Hash(st.Content)
	st.Mode = info.Mode().Perm()
	return st, nil
}

// Hash returns the SHA-256 of the content, used for conflict detection.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Write atomically replaces a vault file. The content is written to a temporary
// file in the same directory (so the rename cannot cross filesystems), the
// original permissions are preserved, and the temp file is renamed over the
// target.
func (v *Vault) Write(rel, content string) error {
	abs, err := v.Resolve(rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	perm := os.FileMode(0o644)
	if info, err := os.Stat(abs); err == nil {
		perm = info.Mode().Perm()
		if perm&0o200 == 0 {
			return fmt.Errorf("%s is read-only; change its permissions to save here", rel)
		}
	}

	tmp, err := os.CreateTemp(dir, ".murmur-*.tmp")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot write in %s: permission denied", filepath.Dir(rel))
		}
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("replace %s: %w", rel, err)
	}
	return nil
}

// Exists reports whether a vault-relative file exists.
func (v *Vault) Exists(rel string) bool {
	abs, err := v.Resolve(rel)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && !info.IsDir()
}
