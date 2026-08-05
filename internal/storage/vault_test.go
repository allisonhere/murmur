package storage_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alliebayless/murmur/internal/storage"
)

func newTestVault(t *testing.T) (*storage.Vault, string) {
	t.Helper()
	root := t.TempDir()
	v, err := storage.NewVault(root)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	return v, root
}

func TestResolveRejectsTraversal(t *testing.T) {
	t.Parallel()
	v, root := newTestVault(t)

	bad := []string{
		"../escape.md",
		"../../etc/passwd",
		"Projects/../../outside.md",
		"/etc/passwd",
		"~/secrets.md",
		"..",
	}
	for _, p := range bad {
		if _, err := v.Resolve(p); err == nil {
			t.Errorf("Resolve(%q) allowed a path outside the vault", p)
		} else if !errors.Is(err, storage.ErrOutsideVault) {
			t.Errorf("Resolve(%q) = %v, want ErrOutsideVault", p, err)
		}
	}

	// Traversal that stays inside the vault is fine.
	got, err := v.Resolve("Projects/../Inbox.md")
	if err != nil {
		t.Fatalf("Resolve of an inside-vault path failed: %v", err)
	}
	if got != filepath.Join(root, "Inbox.md") {
		t.Errorf("Resolve = %q, want %q", got, filepath.Join(root, "Inbox.md"))
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()

	v, root := newTestVault(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	if _, err := v.Resolve("linked/secret.md"); err == nil {
		t.Error("a symlink out of the vault was accepted")
	}
}

func TestWriteIsAtomicAndPreservesPermissions(t *testing.T) {
	t.Parallel()
	v, root := newTestVault(t)

	target := filepath.Join(root, "Notes", "note.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := v.Write("Notes/note.md", "updated\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated\n" {
		t.Errorf("content = %q", data)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v, want 0600", info.Mode().Perm())
	}

	// No temporary files may survive a successful write.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".murmur-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestWriteCreatesParentDirectories(t *testing.T) {
	t.Parallel()
	v, root := newTestVault(t)

	if err := v.Write("Deep/Nested/Path/note.md", "hello\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Deep", "Nested", "Path", "note.md")); err != nil {
		t.Fatalf("file was not created: %v", err)
	}
}

func TestWriteRefusesReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("read-only enforcement differs for this platform or user")
	}
	t.Parallel()
	v, root := newTestVault(t)

	target := filepath.Join(root, "locked.md")
	if err := os.WriteFile(target, []byte("x\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	err := v.Write("locked.md", "y\n")
	if err == nil {
		t.Fatal("expected a read-only error")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestReadMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	v, _ := newTestVault(t)

	st, err := v.Read("does/not/exist.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if st.Exists || st.Content != "" || st.Hash != "" {
		t.Errorf("missing file reported as %+v", st)
	}
}

func TestHashDetectsChanges(t *testing.T) {
	t.Parallel()
	if storage.Hash("a") == storage.Hash("b") {
		t.Error("different content hashed the same")
	}
	if storage.Hash("a") != storage.Hash("a") {
		t.Error("hashing is not deterministic")
	}
}

func TestRel(t *testing.T) {
	t.Parallel()
	v, root := newTestVault(t)

	got, err := v.Rel(filepath.Join(root, "Projects", "Tidemail.md"))
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	if got != "Projects/Tidemail.md" {
		t.Errorf("Rel = %q", got)
	}
	if _, err := v.Rel("/somewhere/else.md"); err == nil {
		t.Error("Rel accepted a path outside the vault")
	}
}

func TestNewVaultRejectsMissingDirectory(t *testing.T) {
	t.Parallel()
	if _, err := storage.NewVault(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing vault")
	}
	if _, err := storage.NewVault(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}
