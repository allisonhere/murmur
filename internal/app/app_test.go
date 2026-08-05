package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alliebayless/murmur/internal/app"
	"github.com/alliebayless/murmur/internal/config"
	"github.com/alliebayless/murmur/internal/model"
)

var testNow = time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC)

// newTestApp copies the sample vault, points a fresh database and config at it
// and opens a fully wired application.
func newTestApp(t *testing.T) (*app.App, string) {
	t.Helper()

	root := copyVault(t)
	cfgDir := t.TempDir()
	dataDir := t.TempDir()

	// Keep the test off the developer's real XDG directories.
	t.Setenv("MURMUR_CONFIG_DIR", cfgDir)
	t.Setenv("MURMUR_DATA_DIR", dataDir)

	cfg := config.Default()
	cfg.VaultPath = root
	cfg.VaultName = "TestVault"
	cfg.SetPath(filepath.Join(cfgDir, "config.yaml"))
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	a, err := app.Open(cfg, app.Options{DBPath: filepath.Join(dataDir, "murmur.db")})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, root
}

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

func read(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func prepare(t *testing.T, a *app.App, text string, opts app.PrepareOptions) *app.Draft {
	t.Helper()
	if opts.Now.IsZero() {
		opts.Now = testNow
	}
	d, err := a.Prepare(context.Background(), text, opts)
	if err != nil {
		t.Fatalf("Prepare(%q): %v", text, err)
	}
	return d
}

// ---------------------------------------------------------------- preparation

func TestPrepareRoutesToTheObviousNote(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, "investigate why the z13 trackpad is detected as a fallback mouse", app.PrepareOptions{})

	if d.Routing.NotePath != "Projects/Linux/ROG Flow Z13.md" {
		t.Errorf("routed to %s", d.Routing.NotePath)
	}
	if d.Routing.Section != "Trackpad troubleshooting" {
		t.Errorf("section = %q", d.Routing.Section)
	}
	if d.Routing.Mode != model.InsertUnderHeading {
		t.Errorf("mode = %s", d.Routing.Mode)
	}
	if d.Markdown == "" {
		t.Error("no Markdown was rendered")
	}
	if d.FileHashBefore == "" {
		t.Error("no conflict hash was recorded")
	}
	if d.Corrected() {
		t.Error("an untouched draft should not count as corrected")
	}
}

func TestPrepareRejectsEmptyThought(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := a.Prepare(context.Background(), "   \n  ", app.PrepareOptions{}); !errors.Is(err, app.ErrEmptyThought) {
		t.Fatalf("err = %v, want ErrEmptyThought", err)
	}
}

func TestPrepareHonoursExplicitDestination(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, ">Projects/Tidemail add an attachment preview", app.PrepareOptions{})
	if d.Routing.NotePath != "Projects/Tidemail.md" {
		t.Errorf("routed to %s", d.Routing.NotePath)
	}
	if d.Routing.Confidence != 1 {
		t.Errorf("confidence = %v, want 1 for an explicit destination", d.Routing.Confidence)
	}
	if strings.Contains(d.Markdown, ">Projects") {
		t.Errorf("the routing hint leaked into the content: %q", d.Markdown)
	}
}

// --------------------------------------------------------------------- saving

func TestSaveInsertsUnderTheChosenHeading(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, "check whether hid_asus needs patching for the z13 trackpad", app.PrepareOptions{})
	before := read(t, root, "Projects/Linux/ROG Flow Z13.md")

	res, err := d.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Created {
		t.Error("an existing note was reported as created")
	}
	if !strings.Contains(res.Summary(), "Saved to Projects/Linux/ROG Flow Z13.md under ## Trackpad troubleshooting") {
		t.Errorf("summary = %q", res.Summary())
	}

	after := read(t, root, "Projects/Linux/ROG Flow Z13.md")
	if after == before {
		t.Fatal("the file was not changed")
	}
	if !strings.Contains(after, strings.Split(d.Markdown, "\n")[0]) {
		t.Errorf("inserted content is missing:\n%s", after)
	}
	// The insertion must land inside the right section.
	trackpad := section(after, "## Trackpad troubleshooting")
	if !strings.Contains(trackpad, "hid_asus") {
		t.Errorf("content landed outside its section:\n%s", after)
	}
	// Frontmatter and later sections must survive untouched.
	if !strings.HasPrefix(after, "---\ntitle: ROG Flow Z13") {
		t.Error("frontmatter was damaged")
	}
	if !strings.Contains(after, "## Battery") {
		t.Error("a later section was lost")
	}
}

func TestSaveCreatesMissingNoteAndDirectories(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, ">Projects/New/Deep Note.md remember to write this up", app.PrepareOptions{})
	res, err := d.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !res.Created {
		t.Error("a new note should be reported as created")
	}
	content := read(t, root, "Projects/New/Deep Note.md")
	if !strings.Contains(strings.ToLower(content), "write this up") {
		t.Errorf("content missing:\n%s", content)
	}
}

func TestSaveRefusesToWriteOutsideTheVault(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, "a thought", app.PrepareOptions{})
	if err := d.SetDestination("../escaped.md"); err == nil {
		t.Fatal("SetDestination allowed a path outside the vault")
	}
}

func TestSaveDetectsConcurrentEdit(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, "check the z13 trackpad again", app.PrepareOptions{})

	// Someone edits the note in Obsidian after the preview was built.
	target := filepath.Join(root, filepath.FromSlash(d.Routing.NotePath))
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(original, []byte("\nEdited elsewhere.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = d.Save()
	if err == nil {
		t.Fatal("expected a conflict error")
	}
	var conflict *app.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}

	// The other person's edit must still be there.
	if !strings.Contains(read(t, root, d.Routing.NotePath), "Edited elsewhere.") {
		t.Error("the concurrent edit was overwritten")
	}

	// Rebuilding the preview lets the save go through.
	if err := d.RefreshDestination(); err != nil {
		t.Fatalf("RefreshDestination: %v", err)
	}
	if _, err := d.Save(); err != nil {
		t.Fatalf("save after refresh: %v", err)
	}
}

func TestSaveRecordsHistoryAndLearning(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, "the z13 trackpad needs work", app.PrepareOptions{})
	if err := d.SetDestination("Inbox/Tasks.md"); err != nil { // a correction
		t.Fatal(err)
	}
	if err := d.SetSection("Hardware"); err != nil {
		t.Fatal(err)
	}
	if !d.Corrected() {
		t.Fatal("changing the destination should count as a correction")
	}
	if _, err := d.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	records, err := a.History(10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("history has %d records, want 1", len(records))
	}
	rec := records[0]
	if rec.NotePath != "Inbox/Tasks.md" || rec.Section != "Hardware" {
		t.Errorf("history recorded %s / %s", rec.NotePath, rec.Section)
	}
	if !rec.Corrected {
		t.Error("the correction was not recorded")
	}
	if rec.Undone {
		t.Error("a fresh capture should not be marked undone")
	}

	// The correction should now bias routing for similar words.
	d2 := prepare(t, a, "the z13 trackpad needs work again", app.PrepareOptions{})
	if d2.Routing.NotePath != "Inbox/Tasks.md" {
		t.Logf("learning did not flip the destination (got %s); it is a nudge, not a rule", d2.Routing.NotePath)
	}
	n, err := a.ResetLearning()
	if err != nil {
		t.Fatalf("ResetLearning: %v", err)
	}
	if n == 0 {
		t.Error("nothing was learned to reset")
	}
}

func TestSaveIntoDailyNoteUsesTemplate(t *testing.T) {
	a, root := newTestApp(t)
	a.Cfg.DailyTemplatePath = "Templates/Daily.md"

	d := prepare(t, a, "finished the first Murmur prototype", app.PrepareOptions{
		Daily:     true,
		ForceType: model.TypeJournal,
	})
	if d.Routing.NotePath != "Daily/2026-08-05.md" {
		t.Fatalf("daily path = %s", d.Routing.NotePath)
	}
	if d.Routing.Section != "Journal" {
		t.Errorf("section = %q, want Journal", d.Routing.Section)
	}

	if _, err := d.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	content := read(t, root, "Daily/2026-08-05.md")
	if !strings.Contains(content, "date: 2026-08-05") {
		t.Errorf("template variables were not expanded:\n%s", content)
	}
	if !strings.Contains(content, "## Tasks") || !strings.Contains(content, "## Notes") {
		t.Errorf("template structure missing:\n%s", content)
	}
	if !strings.Contains(section(content, "## Journal"), "prototype") {
		t.Errorf("the entry did not land under Journal:\n%s", content)
	}
}

func TestPreviewShowsTheResultingFile(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, "check the z13 trackpad", app.PrepareOptions{})
	preview, err := d.Preview()
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !strings.Contains(preview, "## Battery") {
		t.Error("the preview lost the rest of the note")
	}
	if !strings.Contains(preview, "trackpad") {
		t.Error("the preview does not contain the new content")
	}
}

func TestManualMarkdownSurvivesRerender(t *testing.T) {
	a, _ := newTestApp(t)

	d := prepare(t, a, "buy a ups battery", app.PrepareOptions{})
	d.SetMarkdown("- [ ] Hand written content")
	d.Render()
	if d.Markdown != "- [ ] Hand written content" {
		t.Errorf("manual content was overwritten: %q", d.Markdown)
	}

	// Changing the content type intentionally regenerates it.
	d.SetType(model.TypeIdea)
	if strings.Contains(d.Markdown, "Hand written") {
		t.Errorf("changing the type should regenerate the preview: %q", d.Markdown)
	}
}

// ----------------------------------------------------------------------- undo

func TestUndoRestoresTheFile(t *testing.T) {
	a, root := newTestApp(t)

	before := read(t, root, "Projects/Linux/ROG Flow Z13.md")
	d := prepare(t, a, "check the z13 trackpad wiring", app.PrepareOptions{})
	if _, err := d.Save(); err != nil {
		t.Fatal(err)
	}

	plan, err := a.PlanUndo()
	if err != nil {
		t.Fatalf("PlanUndo: %v", err)
	}
	if plan.Strategy != app.UndoRestore || plan.Conflict {
		t.Errorf("plan = %+v, want a clean restore", plan)
	}
	if err := a.ApplyUndo(plan); err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}
	if got := read(t, root, "Projects/Linux/ROG Flow Z13.md"); got != before {
		t.Errorf("undo did not restore the file exactly:\n%s", got)
	}

	records, _ := a.History(10)
	if len(records) != 1 || !records[0].Undone {
		t.Error("the capture was not marked as undone")
	}
	if _, err := a.PlanUndo(); !errors.Is(err, app.ErrNothingToUndo) {
		t.Errorf("err = %v, want ErrNothingToUndo", err)
	}
}

func TestUndoDeletesACreatedNote(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, ">Inbox/Brand New.md write this down", app.PrepareOptions{})
	if _, err := d.Save(); err != nil {
		t.Fatal(err)
	}

	plan, err := a.PlanUndo()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != app.UndoDelete {
		t.Fatalf("strategy = %s, want delete", plan.Strategy)
	}
	if err := a.ApplyUndo(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Inbox", "Brand New.md")); !os.IsNotExist(err) {
		t.Error("the created note was not deleted")
	}
}

func TestUndoAfterAnEditRemovesOnlyMurmursBlock(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, "check the z13 trackpad wiring", app.PrepareOptions{})
	if _, err := d.Save(); err != nil {
		t.Fatal(err)
	}

	// The user then edits the same note by hand.
	target := filepath.Join(root, "Projects", "Linux", "ROG Flow Z13.md")
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(content, []byte("\n## Something I added later\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := a.PlanUndo()
	if err != nil {
		t.Fatalf("PlanUndo: %v", err)
	}
	if plan.Strategy != app.UndoPatch || !plan.Conflict {
		t.Fatalf("plan = %+v, want a flagged patch", plan)
	}
	if err := a.ApplyUndo(plan); err != nil {
		t.Fatal(err)
	}

	after := read(t, root, "Projects/Linux/ROG Flow Z13.md")
	if !strings.Contains(after, "## Something I added later") {
		t.Error("undo destroyed the user's newer edit")
	}
	if strings.Contains(after, "wiring") {
		t.Errorf("Murmur's block was not removed:\n%s", after)
	}
}

func TestUndoRefusesWhenTheBlockIsGone(t *testing.T) {
	a, root := newTestApp(t)

	d := prepare(t, a, "check the z13 trackpad wiring", app.PrepareOptions{})
	if _, err := d.Save(); err != nil {
		t.Fatal(err)
	}

	// The note is rewritten completely: Murmur must not guess.
	target := filepath.Join(root, "Projects", "Linux", "ROG Flow Z13.md")
	if err := os.WriteFile(target, []byte("# Rewritten from scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := a.PlanUndo()
	if err == nil {
		t.Fatal("expected undo to refuse")
	}
	var conflict *app.UndoConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want UndoConflictError", err)
	}
	if !strings.Contains(err.Error(), "no longer present") {
		t.Errorf("the error should explain the conflict: %v", err)
	}
	if got := read(t, root, "Projects/Linux/ROG Flow Z13.md"); got != "# Rewritten from scratch\n" {
		t.Error("the newer content was modified despite the refusal")
	}
}

func TestUndoWithNoHistory(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := a.PlanUndo(); !errors.Is(err, app.ErrNothingToUndo) {
		t.Fatalf("err = %v, want ErrNothingToUndo", err)
	}
}

// ----------------------------------------------------------------- quick mode

// quickModeAllows mirrors the rule the CLI applies: an explicit destination, or
// confidence at or above the configured threshold.
func quickModeAllows(d *app.Draft, cfg config.Config) bool {
	if d.Hints.Path != "" || d.Hints.Project != "" {
		return true
	}
	return d.Routing.Confidence >= cfg.QuickModeConfidence
}

func TestQuickModeConfidenceBehaviour(t *testing.T) {
	a, _ := newTestApp(t)
	cfg := a.Cfg

	explicit := prepare(t, a, ">Inbox/Tasks.md buy a replacement ups battery", app.PrepareOptions{})
	if !quickModeAllows(explicit, cfg) {
		t.Error("an explicit destination should always be allowed to auto-save")
	}

	vague := prepare(t, a, "hmm", app.PrepareOptions{})
	if vague.Routing.Confidence >= cfg.QuickModeConfidence {
		t.Errorf("a vague thought scored %.2f, at or above the %.2f threshold",
			vague.Routing.Confidence, cfg.QuickModeConfidence)
	}
	if quickModeAllows(vague, cfg) {
		t.Error("a low-confidence thought must not auto-save")
	}

	// Lowering the threshold changes the decision, proving it is honoured.
	cfg.QuickModeConfidence = 0.0
	if !quickModeAllows(vague, cfg) {
		t.Error("a zero threshold should allow anything through")
	}
}

func TestObsidianURI(t *testing.T) {
	a, _ := newTestApp(t)
	uri := a.ObsidianURI("Projects/Linux/ROG Flow Z13.md")
	if !strings.HasPrefix(uri, "obsidian://open?") {
		t.Errorf("uri = %q", uri)
	}
	if !strings.Contains(uri, "vault=TestVault") {
		t.Errorf("vault name missing: %q", uri)
	}
	if strings.Contains(uri, ".md") {
		t.Errorf("the extension should be dropped: %q", uri)
	}
}

// section returns the text of a Markdown section, for assertions.
func section(content, heading string) string {
	idx := strings.Index(content, heading)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		return rest[:next]
	}
	return rest
}
