package database_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alliebayless/murmur/internal/database"
	"github.com/alliebayless/murmur/internal/model"
)

func newRepo(t *testing.T) (*database.DB, *database.Repo) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "murmur.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, database.NewRepo(db)
}

func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "murmur.db")

	for i := 0; i < 3; i++ {
		db, err := database.Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatalf("count migrations: %v", err)
		}
		if n != 1 {
			t.Errorf("applied %d migrations after open %d, want 1", n, i)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationsCreateEveryTable(t *testing.T) {
	t.Parallel()
	db, _ := newRepo(t)

	want := []string{
		"vault_files", "headings", "tags", "aliases", "links",
		"captures", "routing_candidates", "routing_corrections",
		"write_transactions", "settings_metadata",
	}
	for _, table := range want {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s is missing: %v", table, err)
		}
	}
}

func TestOpenReportsCorruption(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("this is definitely not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := database.Open(path)
	if err == nil {
		t.Fatal("expected an error for a corrupted database")
	}
	// The message must tell the user how to recover.
	if got := err.Error(); !contains(got, "rebuild") && !contains(got, "corrupt") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestNoteRoundTrip(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)

	note := model.Note{
		RelPath:  "Projects/Tidemail.md",
		FileName: "Tidemail",
		Title:    "Tidemail",
		Aliases:  []string{"Tide Mail"},
		Tags:     []string{"tui", "email"},
		Headings: []model.Heading{{Level: 2, Text: "Roadmap", Line: 4}},
		Links:    []string{"Bubble Tea"},
		Excerpt:  "a terminal email client",
		ModTime:  time.Unix(1700000000, 0),
		Size:     123,
	}
	if err := repo.UpsertNote(note); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	notes, err := repo.Notes()
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(notes))
	}
	got := notes[0]
	if got.Title != note.Title || len(got.Tags) != 2 || len(got.Aliases) != 1 ||
		len(got.Headings) != 1 || len(got.Links) != 1 {
		t.Errorf("round trip lost data: %+v", got)
	}

	// Upserting again must replace children, not duplicate them.
	note.Tags = []string{"tui"}
	if err := repo.UpsertNote(note); err != nil {
		t.Fatal(err)
	}
	notes, _ = repo.Notes()
	if len(notes) != 1 || len(notes[0].Tags) != 1 {
		t.Errorf("upsert duplicated rows: %+v", notes)
	}

	stamps, err := repo.Stamps()
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := stamps["Projects/Tidemail.md"]; !ok || s.Size != 123 {
		t.Errorf("stamps = %+v", stamps)
	}

	if err := repo.DeleteNote("Projects/Tidemail.md"); err != nil {
		t.Fatal(err)
	}
	if notes, _ := repo.Notes(); len(notes) != 0 {
		t.Error("the note was not deleted")
	}
}

func TestCaptureAndTransactionRoundTrip(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)

	id, err := repo.InsertCapture(model.CaptureRecord{
		CreatedAt:  time.Now(),
		Raw:        "buy a ups battery",
		Markdown:   "- [ ] Buy a UPS battery",
		NotePath:   "Inbox/Tasks.md",
		Section:    "Hardware",
		Type:       model.TypeTask,
		Tags:       []string{"hardware", "home"},
		Confidence: 0.91,
		Source:     model.SourceRanking,
		Corrected:  true,
	})
	if err != nil {
		t.Fatalf("InsertCapture: %v", err)
	}

	txID, err := repo.InsertWriteTransaction(model.WriteTransaction{
		CaptureID:  id,
		CreatedAt:  time.Now(),
		Path:       "Inbox/Tasks.md",
		HashBefore: "before",
		HashAfter:  "after",
		Inserted:   "- [ ] Buy a UPS battery",
		Section:    "Hardware",
		Mode:       model.InsertUnderHeading,
		Backup:     "# Tasks\n",
	})
	if err != nil {
		t.Fatalf("InsertWriteTransaction: %v", err)
	}

	recs, err := repo.Captures(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d captures", len(recs))
	}
	if !recs[0].Corrected || len(recs[0].Tags) != 2 || recs[0].Transaction != txID {
		t.Errorf("capture round trip: %+v", recs[0])
	}

	tx, err := repo.LatestTransaction()
	if err != nil {
		t.Fatalf("LatestTransaction: %v", err)
	}
	if tx.ID != txID || tx.Backup != "# Tasks\n" || tx.Mode != model.InsertUnderHeading {
		t.Errorf("transaction round trip: %+v", tx)
	}

	if err := repo.MarkUndone(tx.ID, tx.CaptureID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.LatestTransaction(); !errors.Is(err, database.ErrNothingToUndo) {
		t.Errorf("err = %v, want ErrNothingToUndo", err)
	}
	recs, _ = repo.Captures(10)
	if !recs[0].Undone {
		t.Error("the capture was not marked undone")
	}
}

func TestLearningWeights(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)

	if err := repo.RecordRouting([]string{"trackpad", "z13"}, "Projects/Z13.md", "Trackpad", model.TypeTask, false); err != nil {
		t.Fatal(err)
	}
	// A correction counts for more than a confirmation.
	if err := repo.RecordRouting([]string{"trackpad"}, "Inbox/Tasks.md", "Hardware", model.TypeTask, true); err != nil {
		t.Fatal(err)
	}

	routes, err := repo.LearnedRoutes([]string{"trackpad"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("got %d learned routes, want 2", len(routes))
	}
	if routes[0].NotePath != "Inbox/Tasks.md" {
		t.Errorf("the corrected destination should rank first, got %+v", routes)
	}
	if routes[0].Section != "Hardware" {
		t.Errorf("section was not remembered: %+v", routes[0])
	}

	if routes, _ := repo.LearnedRoutes([]string{"unrelated"}); len(routes) != 0 {
		t.Errorf("unrelated tokens matched: %+v", routes)
	}
	if routes, _ := repo.LearnedRoutes(nil); len(routes) != 0 {
		t.Error("empty token list should return nothing")
	}

	n, err := repo.ResetLearning()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 { // trackpad+z13 for the first route, trackpad for the second
		t.Errorf("reset removed %d rows, want 3", n)
	}
	if routes, _ := repo.LearnedRoutes([]string{"trackpad"}); len(routes) != 0 {
		t.Error("learning survived the reset")
	}
}

func TestMetadata(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)

	if v, err := repo.Meta("missing"); err != nil || v != "" {
		t.Errorf("missing key returned %q / %v", v, err)
	}
	if err := repo.SetMeta("last_index", "2026-08-05"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMeta("last_index", "2026-08-06"); err != nil {
		t.Fatal(err)
	}
	v, err := repo.Meta("last_index")
	if err != nil || v != "2026-08-06" {
		t.Errorf("Meta = %q / %v", v, err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
