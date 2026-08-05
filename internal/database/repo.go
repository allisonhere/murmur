package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/model"
)

// Repo is the data access layer over the Murmur database.
type Repo struct{ db *DB }

// NewRepo wraps a database handle.
func NewRepo(db *DB) *Repo { return &Repo{db: db} }

// ---------------------------------------------------------------- vault index

// Stamp is the modification signature used to detect stale index entries.
type Stamp struct {
	ModTime time.Time
	Size    int64
}

// Stamps returns the recorded modification time and size for every indexed
// file, keyed by vault-relative path.
func (r *Repo) Stamps() (map[string]Stamp, error) {
	rows, err := r.db.Query(`SELECT rel_path, mod_time, size FROM vault_files`)
	if err != nil {
		return nil, fmt.Errorf("read index stamps: %w", err)
	}
	defer rows.Close()

	out := map[string]Stamp{}
	for rows.Next() {
		var path string
		var mod, size int64
		if err := rows.Scan(&path, &mod, &size); err != nil {
			return nil, err
		}
		out[path] = Stamp{ModTime: time.Unix(mod, 0), Size: size}
	}
	return out, rows.Err()
}

// UpsertNote inserts or replaces a note and all of its child rows.
func (r *Repo) UpsertNote(n model.Note) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO vault_files (rel_path, file_name, title, excerpt, mod_time, size, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(rel_path) DO UPDATE SET
			file_name = excluded.file_name,
			title     = excluded.title,
			excerpt   = excluded.excerpt,
			mod_time  = excluded.mod_time,
			size      = excluded.size,
			indexed_at= excluded.indexed_at`,
		n.RelPath, n.FileName, n.Title, n.Excerpt, n.ModTime.Unix(), n.Size, time.Now().Unix()); err != nil {
		return fmt.Errorf("index %s: %w", n.RelPath, err)
	}
	// LastInsertId is not meaningful on the UPDATE branch of an upsert, so
	// always resolve the row id explicitly.
	var id int64
	if err := tx.QueryRow(`SELECT id FROM vault_files WHERE rel_path = ?`, n.RelPath).Scan(&id); err != nil {
		return err
	}

	for _, table := range []string{"headings", "tags", "aliases", "links"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE file_id = ?`, id); err != nil {
			return err
		}
	}
	for _, h := range n.Headings {
		if _, err := tx.Exec(`INSERT INTO headings (file_id, level, text, line) VALUES (?, ?, ?, ?)`,
			id, h.Level, h.Text, h.Line); err != nil {
			return err
		}
	}
	for _, t := range n.Tags {
		if _, err := tx.Exec(`INSERT INTO tags (file_id, tag) VALUES (?, ?)`, id, t); err != nil {
			return err
		}
	}
	for _, a := range n.Aliases {
		if _, err := tx.Exec(`INSERT INTO aliases (file_id, alias) VALUES (?, ?)`, id, a); err != nil {
			return err
		}
	}
	for _, l := range n.Links {
		if _, err := tx.Exec(`INSERT INTO links (file_id, target) VALUES (?, ?)`, id, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteNote removes a note (and its children) from the index.
func (r *Repo) DeleteNote(relPath string) error {
	_, err := r.db.Exec(`DELETE FROM vault_files WHERE rel_path = ?`, relPath)
	return err
}

// ClearIndex empties the vault index, leaving history intact.
func (r *Repo) ClearIndex() error {
	for _, table := range []string{"headings", "tags", "aliases", "links", "vault_files"} {
		if _, err := r.db.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
	}
	return nil
}

// Notes loads the whole index. Vaults are typically a few thousand notes with
// bounded excerpts, so loading everything once per run keeps ranking simple and
// fast.
func (r *Repo) Notes() ([]model.Note, error) {
	rows, err := r.db.Query(`SELECT id, rel_path, file_name, title, excerpt, mod_time, size, indexed_at
		FROM vault_files ORDER BY rel_path`)
	if err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	defer rows.Close()

	var notes []model.Note
	byID := map[int64]int{}
	for rows.Next() {
		var n model.Note
		var mod, indexed int64
		if err := rows.Scan(&n.ID, &n.RelPath, &n.FileName, &n.Title, &n.Excerpt, &mod, &n.Size, &indexed); err != nil {
			return nil, err
		}
		n.ModTime = time.Unix(mod, 0)
		n.Indexed = time.Unix(indexed, 0)
		byID[n.ID] = len(notes)
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return notes, nil
	}

	if err := r.eachChild(`SELECT file_id, level, text, line FROM headings ORDER BY file_id, line`,
		func(scan func(...any) error) error {
			var fileID int64
			var h model.Heading
			if err := scan(&fileID, &h.Level, &h.Text, &h.Line); err != nil {
				return err
			}
			if i, ok := byID[fileID]; ok {
				notes[i].Headings = append(notes[i].Headings, h)
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := r.eachChild(`SELECT file_id, tag FROM tags`, func(scan func(...any) error) error {
		var fileID int64
		var v string
		if err := scan(&fileID, &v); err != nil {
			return err
		}
		if i, ok := byID[fileID]; ok {
			notes[i].Tags = append(notes[i].Tags, v)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := r.eachChild(`SELECT file_id, alias FROM aliases`, func(scan func(...any) error) error {
		var fileID int64
		var v string
		if err := scan(&fileID, &v); err != nil {
			return err
		}
		if i, ok := byID[fileID]; ok {
			notes[i].Aliases = append(notes[i].Aliases, v)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := r.eachChild(`SELECT file_id, target FROM links`, func(scan func(...any) error) error {
		var fileID int64
		var v string
		if err := scan(&fileID, &v); err != nil {
			return err
		}
		if i, ok := byID[fileID]; ok {
			notes[i].Links = append(notes[i].Links, v)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return notes, nil
}

func (r *Repo) eachChild(query string, fn func(scan func(...any) error) error) error {
	rows, err := r.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ------------------------------------------------------------------ captures

// InsertCapture stores a capture and returns its id.
func (r *Repo) InsertCapture(rec model.CaptureRecord) (int64, error) {
	res, err := r.db.Exec(`INSERT INTO captures
		(created_at, raw, markdown, note_path, section, content_type, tags, confidence, source, corrected, undone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		rec.CreatedAt.Unix(), rec.Raw, rec.Markdown, rec.NotePath, rec.Section,
		string(rec.Type), strings.Join(rec.Tags, ","), rec.Confidence, string(rec.Source), boolInt(rec.Corrected))
	if err != nil {
		return 0, fmt.Errorf("record capture: %w", err)
	}
	return res.LastInsertId()
}

// InsertCandidates records the alternatives that were considered for a capture.
func (r *Repo) InsertCandidates(captureID int64, cands []model.Candidate, chosen string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, c := range cands {
		if _, err := tx.Exec(`INSERT INTO routing_candidates (capture_id, note_path, score, rank, reason, chosen)
			VALUES (?, ?, ?, ?, ?, ?)`,
			captureID, c.Note.RelPath, c.Score, i+1, c.Reason(), boolInt(c.Note.RelPath == chosen)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Captures returns the most recent captures, newest first.
func (r *Repo) Captures(limit int) ([]model.CaptureRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`SELECT c.id, c.created_at, c.raw, c.markdown, c.note_path, c.section,
			c.content_type, c.tags, c.confidence, c.source, c.corrected, c.undone,
			COALESCE(w.id, 0)
		FROM captures c
		LEFT JOIN write_transactions w ON w.capture_id = c.id
		ORDER BY c.created_at DESC, c.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	defer rows.Close()

	var out []model.CaptureRecord
	for rows.Next() {
		var rec model.CaptureRecord
		var created int64
		var tags, ctype, source string
		var corrected, undone int
		if err := rows.Scan(&rec.ID, &created, &rec.Raw, &rec.Markdown, &rec.NotePath, &rec.Section,
			&ctype, &tags, &rec.Confidence, &source, &corrected, &undone, &rec.Transaction); err != nil {
			return nil, err
		}
		rec.CreatedAt = time.Unix(created, 0)
		rec.Type = model.ContentType(ctype)
		rec.Source = model.RoutingSource(source)
		rec.Corrected = corrected == 1
		rec.Undone = undone == 1
		if tags != "" {
			rec.Tags = strings.Split(tags, ",")
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// -------------------------------------------------------------- transactions

// InsertWriteTransaction stores an undo record.
func (r *Repo) InsertWriteTransaction(tx model.WriteTransaction) (int64, error) {
	res, err := r.db.Exec(`INSERT INTO write_transactions
		(capture_id, created_at, path, hash_before, hash_after, inserted, section, mode, created_file, backup, undone, undone_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)`,
		tx.CaptureID, tx.CreatedAt.Unix(), tx.Path, tx.HashBefore, tx.HashAfter,
		tx.Inserted, tx.Section, string(tx.Mode), boolInt(tx.Created), tx.Backup)
	if err != nil {
		return 0, fmt.Errorf("record undo information: %w", err)
	}
	return res.LastInsertId()
}

// ErrNothingToUndo is returned when no reversible write remains.
var ErrNothingToUndo = errors.New("there is nothing to undo")

// LatestTransaction returns the most recent write that has not been undone.
func (r *Repo) LatestTransaction() (model.WriteTransaction, error) {
	row := r.db.QueryRow(`SELECT id, capture_id, created_at, path, hash_before, hash_after,
			inserted, section, mode, created_file, backup, undone, undone_at
		FROM write_transactions WHERE undone = 0 ORDER BY created_at DESC, id DESC LIMIT 1`)
	return scanTransaction(row)
}

func scanTransaction(row *sql.Row) (model.WriteTransaction, error) {
	var tx model.WriteTransaction
	var created, undoneAt int64
	var mode string
	var createdFile, undone int
	err := row.Scan(&tx.ID, &tx.CaptureID, &created, &tx.Path, &tx.HashBefore, &tx.HashAfter,
		&tx.Inserted, &tx.Section, &mode, &createdFile, &tx.Backup, &undone, &undoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		return tx, ErrNothingToUndo
	}
	if err != nil {
		return tx, err
	}
	tx.CreatedAt = time.Unix(created, 0)
	tx.Mode = model.InsertMode(mode)
	tx.Created = createdFile == 1
	tx.Undone = undone == 1
	if undoneAt > 0 {
		tx.UndoneAt = time.Unix(undoneAt, 0)
	}
	return tx, nil
}

// MarkUndone flags a transaction (and its capture) as reversed.
func (r *Repo) MarkUndone(id, captureID int64) error {
	if _, err := r.db.Exec(`UPDATE write_transactions SET undone = 1, undone_at = ? WHERE id = ?`,
		time.Now().Unix(), id); err != nil {
		return err
	}
	if captureID > 0 {
		if _, err := r.db.Exec(`UPDATE captures SET undone = 1 WHERE id = ?`, captureID); err != nil {
			return err
		}
	}
	return nil
}

// --------------------------------------------------------------- learning

// RecordRouting reinforces the association between the tokens of a thought and
// the destination the user actually accepted. Confirmations get a small weight;
// explicit corrections get a larger one.
func (r *Repo) RecordRouting(tokens []string, notePath, section string, ctype model.ContentType, corrected bool) error {
	weight := 0.5
	if corrected {
		weight = 1.5
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	for _, tok := range tokens {
		if _, err := tx.Exec(`INSERT INTO routing_corrections (token, note_path, content_type, section, weight, hits, updated_at)
			VALUES (?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(token, note_path) DO UPDATE SET
				weight = routing_corrections.weight + excluded.weight,
				hits = routing_corrections.hits + 1,
				content_type = excluded.content_type,
				section = excluded.section,
				updated_at = excluded.updated_at`,
			tok, notePath, string(ctype), section, weight, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LearnedRoute is a remembered token/destination association.
type LearnedRoute struct {
	NotePath string
	Section  string
	Type     model.ContentType
	Weight   float64
}

// LearnedRoutes returns the destinations previously chosen for these tokens.
func (r *Repo) LearnedRoutes(tokens []string) ([]LearnedRoute, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tokens)), ",")
	args := make([]any, len(tokens))
	for i, t := range tokens {
		args[i] = t
	}
	rows, err := r.db.Query(`SELECT note_path, section, content_type, SUM(weight) AS w
		FROM routing_corrections WHERE token IN (`+placeholders+`)
		GROUP BY note_path ORDER BY w DESC LIMIT 25`, args...)
	if err != nil {
		return nil, fmt.Errorf("read learned routes: %w", err)
	}
	defer rows.Close()

	var out []LearnedRoute
	for rows.Next() {
		var lr LearnedRoute
		var ctype string
		if err := rows.Scan(&lr.NotePath, &lr.Section, &ctype, &lr.Weight); err != nil {
			return nil, err
		}
		lr.Type = model.ContentType(ctype)
		out = append(out, lr)
	}
	return out, rows.Err()
}

// ResetLearning clears the learned routing table.
func (r *Repo) ResetLearning() (int64, error) {
	res, err := r.db.Exec(`DELETE FROM routing_corrections`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------- metadata

// SetMeta stores a small key/value pair (index timestamps and similar).
func (r *Repo) SetMeta(key, value string) error {
	_, err := r.db.Exec(`INSERT INTO settings_metadata (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

// Meta reads a metadata value; a missing key returns an empty string.
func (r *Repo) Meta(key string) (string, error) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM settings_metadata WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
