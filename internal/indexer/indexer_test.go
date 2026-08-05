package indexer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/database"
	"github.com/alliebayless/murmur/internal/indexer"
	"github.com/alliebayless/murmur/internal/model"
	"github.com/alliebayless/murmur/internal/storage"
)

// copyVault copies the checked-in sample vault into a temporary directory so
// tests can write to it freely.
func copyVault(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "vault")
	dst := t.TempDir()

	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("copy sample vault: %v", err)
	}
	return dst
}

func newIndexer(t *testing.T, root string) (*indexer.Indexer, *database.Repo) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "murmur.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	v, err := storage.NewVault(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	repo := database.NewRepo(db)
	cfg := config.Default()
	cfg.VaultPath = root
	return indexer.New(v, repo, cfg), repo
}

func TestIndexSampleVault(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, repo := newIndexer(t, root)

	stats, err := ix.Run(false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Indexed == 0 {
		t.Fatal("nothing was indexed")
	}

	notes, err := repo.Notes()
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	byPath := map[string]model.Note{}
	for _, n := range notes {
		byPath[n.RelPath] = n
	}

	z13, ok := byPath["Projects/Linux/ROG Flow Z13.md"]
	if !ok {
		t.Fatalf("the Z13 note was not indexed; got %v", keys(byPath))
	}
	if z13.Title != "ROG Flow Z13" {
		t.Errorf("title = %q", z13.Title)
	}
	if !hasString(z13.Aliases, "Z13") {
		t.Errorf("aliases = %v", z13.Aliases)
	}
	if !hasString(z13.Tags, "linux") || !hasString(z13.Tags, "asus") {
		t.Errorf("tags = %v", z13.Tags)
	}
	if !hasHeading(z13.Headings, "Trackpad troubleshooting") {
		t.Errorf("headings = %v", z13.Headings)
	}
	for _, h := range z13.Headings {
		if strings.Contains(h.Text, "fake heading") {
			t.Error("a heading inside a fenced code block was indexed")
		}
	}
	if !hasString(z13.Links, "Fedora Suspend") {
		t.Errorf("wikilinks = %v", z13.Links)
	}
	if len(z13.Excerpt) > indexer.ExcerptLimit {
		t.Errorf("excerpt is %d bytes, over the %d limit", len(z13.Excerpt), indexer.ExcerptLimit)
	}
}

func TestIndexHonoursExclusions(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, repo := newIndexer(t, root)

	if _, err := ix.Run(false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	notes, err := repo.Notes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		switch {
		case strings.HasPrefix(n.RelPath, ".trash/"):
			t.Errorf("a trashed note was indexed: %s", n.RelPath)
		case strings.HasPrefix(n.RelPath, ".obsidian/"):
			t.Errorf("an .obsidian file was indexed: %s", n.RelPath)
		case strings.HasPrefix(n.RelPath, "Templates/"):
			t.Errorf("an excluded Templates note was indexed: %s", n.RelPath)
		}
	}
}

func TestIndexIsIncrementalAndDetectsChanges(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, repo := newIndexer(t, root)

	first, err := ix.Run(false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ix.Run(false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Indexed != 0 {
		t.Errorf("second run re-indexed %d unchanged notes", second.Indexed)
	}
	if second.Skipped != first.Scanned {
		t.Errorf("skipped %d of %d scanned notes", second.Skipped, first.Scanned)
	}

	// Change a note and make sure the change is picked up.
	target := filepath.Join(root, "Inbox.md")
	if err := os.WriteFile(target, []byte("# Inbox\n\n## Notes\n\n## Brand New Section\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	third, err := ix.Run(false)
	if err != nil {
		t.Fatal(err)
	}
	if third.Indexed != 1 {
		t.Errorf("changed note was not re-indexed (indexed=%d)", third.Indexed)
	}

	notes, err := repo.Notes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.RelPath == "Inbox.md" && !hasHeading(n.Headings, "Brand New Section") {
			t.Errorf("the new heading was not indexed: %v", n.Headings)
		}
	}
}

func TestIndexRemovesDeletedNotes(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, repo := newIndexer(t, root)

	if _, err := ix.Run(false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Projects", "Pantry.md")); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.Run(false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("Removed = %d, want 1", stats.Removed)
	}
	notes, err := repo.Notes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.RelPath == "Projects/Pantry.md" {
			t.Error("the deleted note is still in the index")
		}
	}
}

func TestIndexSurvivesBrokenFrontmatter(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, repo := newIndexer(t, root)

	stats, err := ix.Run(false)
	if err != nil {
		t.Fatalf("a broken note must not fail the whole index: %v", err)
	}
	if len(stats.Warnings) == 0 {
		t.Error("expected a warning about the malformed frontmatter")
	}

	notes, err := repo.Notes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range notes {
		if n.RelPath == "Reference/Broken Frontmatter.md" {
			found = true
			if n.Title != "Broken Frontmatter" {
				t.Errorf("title fell back to %q, want the H1 text", n.Title)
			}
		}
	}
	if !found {
		t.Error("the note with broken frontmatter was skipped entirely")
	}
}

func TestRebuildClearsTheIndex(t *testing.T) {
	t.Parallel()
	root := copyVault(t)
	ix, _ := newIndexer(t, root)

	if _, err := ix.Run(false); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.Run(true)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 0 {
		t.Errorf("a rebuild skipped %d notes; it should re-parse everything", stats.Skipped)
	}
	if stats.Indexed != stats.Scanned {
		t.Errorf("rebuild indexed %d of %d scanned", stats.Indexed, stats.Scanned)
	}
}

func keys(m map[string]model.Note) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func hasHeading(hs []model.Heading, want string) bool {
	for _, h := range hs {
		if h.Text == want {
			return true
		}
	}
	return false
}
